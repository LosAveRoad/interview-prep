// Package service 实现秒杀核心业务流程。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/lisa/seckill/internal/cache"
	"github.com/lisa/seckill/internal/config"
	"github.com/lisa/seckill/internal/idgen"
	"github.com/lisa/seckill/internal/metrics"
	"github.com/lisa/seckill/internal/model"
	"github.com/lisa/seckill/internal/mq"
	"github.com/lisa/seckill/internal/store"
)

// 业务错误，由 API 层映射为 HTTP 状态码。
var (
	ErrNotFound     = errors.New("activity not found")
	ErrNotStarted   = errors.New("activity not started")
	ErrEnded        = errors.New("activity ended")
	ErrSoldOut      = errors.New("sold out")
	ErrRepeat       = errors.New("already purchased")
	ErrNotPreheated = errors.New("activity not preheated")
	ErrBadToken     = errors.New("invalid seckill token")
	ErrTooMany      = errors.New("too many requests")
)

// Result 是抢购结果。
type Result struct {
	Status  string `json:"status"` // queued / success / failed / none
	OrderID string `json:"order_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Service 串联 Redis、MySQL 与消息队列。
type Service struct {
	cfg   config.Config
	cache *cache.Cache
	store *store.Store
	ids   *idgen.Snowflake
	log   *slog.Logger

	soldOut   sync.Map // 本地售罄标记：activityID -> struct{}，售罄后请求不再访问 Redis
	consumers sync.Map // 已启动消费者的活动：activityID -> struct{}
	bgCtx     context.Context
}

// New 创建 Service。bgCtx 控制后台消费者的生命周期。
func New(bgCtx context.Context, cfg config.Config, c *cache.Cache, s *store.Store,
	ids *idgen.Snowflake, log *slog.Logger) *Service {
	return &Service{cfg: cfg, cache: c, store: s, ids: ids, log: log, bgCtx: bgCtx}
}

func (s *Service) loadActivity(ctx context.Context, id int64) (*model.Activity, error) {
	a, err := s.store.GetActivity(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, cache.ErrActivityNotFound
	}
	return a, err
}

// Activity 查询活动详情（Cache Aside）。
func (s *Service) Activity(ctx context.Context, id int64) (*model.Activity, error) {
	a, err := s.cache.GetActivity(ctx, id, s.loadActivity)
	if errors.Is(err, cache.ErrActivityNotFound) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Service) checkTime(a *model.Activity) error {
	now := time.Now()
	switch {
	case a.Status != model.ActivityPublished:
		return ErrNotFound
	case now.Before(a.StartTime):
		return ErrNotStarted
	case !now.Before(a.EndTime):
		return ErrEnded
	}
	return nil
}

// IssueToken 活动开始后为用户签发一次性令牌（秒杀地址隐藏，防止脚本提前刷接口）。
func (s *Service) IssueToken(ctx context.Context, aid, uid int64) (string, error) {
	a, err := s.Activity(ctx, aid)
	if err != nil {
		return "", err
	}
	if err := s.checkTime(a); err != nil {
		return "", err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	return token, s.cache.SetToken(ctx, aid, uid, token, s.cfg.TokenTTL)
}

// Seckill 执行抢购：限流 → 校验 → 令牌 → 本地售罄标记 → Lua 预扣并投递消息。
// 成功只代表“排队中”，订单由消费者异步落库，返回预生成的订单号。
func (s *Service) Seckill(ctx context.Context, aid, uid int64, token string) (int64, error) {
	metrics.Requests.Add(1)

	if _, done := s.soldOut.Load(aid); done {
		metrics.SoldOut.Add(1)
		return 0, ErrSoldOut
	}
	ok, err := s.cache.AllowUser(ctx, uid, s.cfg.UserReqPerSec)
	if err != nil {
		return 0, err
	}
	if !ok {
		metrics.RateLimited.Add(1)
		return 0, ErrTooMany
	}
	a, err := s.Activity(ctx, aid)
	if err != nil {
		return 0, err
	}
	if err := s.checkTime(a); err != nil {
		return 0, err
	}
	valid, err := s.cache.ConsumeToken(ctx, aid, uid, token)
	if err != nil {
		return 0, err
	}
	if !valid {
		return 0, ErrBadToken
	}

	oid, err := s.ids.Next()
	if err != nil {
		return 0, err
	}
	left, err := s.cache.Deduct(ctx, aid, uid, oid)
	if err != nil {
		return 0, err
	}
	switch left {
	case cache.DeductSoldOut:
		s.soldOut.Store(aid, struct{}{})
		metrics.SoldOut.Add(1)
		return 0, ErrSoldOut
	case cache.DeductRepeat:
		metrics.Repeat.Add(1)
		return 0, ErrRepeat
	case cache.DeductNotPreheated:
		return 0, ErrNotPreheated
	}
	if left == 0 {
		s.soldOut.Store(aid, struct{}{})
	}
	metrics.Queued.Add(1)
	return oid, nil
}

// Result 查询抢购结果：先查 Redis，缺失时回查数据库。
func (s *Service) Result(ctx context.Context, aid, uid int64) (Result, error) {
	v, err := s.cache.GetResult(ctx, aid, uid)
	if err != nil {
		return Result{}, err
	}
	if v != "" {
		status, detail := cache.ParseResult(v)
		if status == "failed" {
			return Result{Status: status, Reason: detail}, nil
		}
		return Result{Status: status, OrderID: detail}, nil
	}
	o, err := s.store.GetOrderByUser(ctx, aid, uid)
	if errors.Is(err, store.ErrNotFound) {
		return Result{Status: "none"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Status: "success", OrderID: strconv.FormatInt(o.ID, 10)}, nil
}

// HandleOrder 是消息消费者：在 MySQL 事务中创建订单并扣减库存，保证幂等。
func (s *Service) HandleOrder(ctx context.Context, m mq.OrderMsg) error {
	a, err := s.Activity(ctx, m.ActivityID)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: activity %d", mq.ErrPermanent, m.ActivityID)
	}
	if err != nil {
		return err
	}
	o := &model.Order{ID: m.OrderID, ActivityID: m.ActivityID, UserID: m.UserID,
		GoodsID: a.GoodsID, Price: a.Price}

	err = s.store.CreateOrder(ctx, o)
	switch {
	case err == nil:
		metrics.OrderCreated.Add(1)
		return s.cache.SetResult(ctx, m.ActivityID, m.UserID, "success:"+strconv.FormatInt(m.OrderID, 10))

	case errors.Is(err, store.ErrDuplicateOrder):
		// 消息重复投递（至少一次语义）或历史订单：订单号相同视为已成功，幂等返回
		exist, qerr := s.store.GetOrderByUser(ctx, m.ActivityID, m.UserID)
		if qerr != nil {
			return qerr
		}
		if exist.ID == m.OrderID {
			return s.cache.SetResult(ctx, m.ActivityID, m.UserID, "success:"+strconv.FormatInt(exist.ID, 10))
		}
		// 用户已有其他订单（例如 Redis 数据丢失后重复抢到），本次预扣作废并归还库存
		metrics.OrderFailed.Add(1)
		_, rerr := s.cache.Rollback(ctx, m.ActivityID, m.UserID, "failed:repeat", true)
		s.soldOut.Delete(m.ActivityID)
		return rerr

	case errors.Is(err, store.ErrSoldOut):
		// 数据库兜底发现已售罄（Redis 与 DB 不一致），失败且不归还 Redis 库存
		metrics.OrderFailed.Add(1)
		_, rerr := s.cache.Rollback(ctx, m.ActivityID, m.UserID, "failed:sold_out", false)
		return rerr
	}
	return err // 数据库暂时不可用等，返回错误交给 MQ 重试
}

// OnDeadLetter 消息多次重试仍失败：回滚预扣，让库存回到池子里。
func (s *Service) OnDeadLetter(ctx context.Context, m mq.OrderMsg) {
	metrics.DeadLetters.Add(1)
	if _, err := s.store.GetOrderByUser(ctx, m.ActivityID, m.UserID); err == nil {
		return // 订单其实已落库，无需回滚
	}
	if _, err := s.cache.Rollback(ctx, m.ActivityID, m.UserID, "failed:system_error", true); err != nil {
		s.log.Error("rollback dead letter", "aid", m.ActivityID, "uid", m.UserID, "err", err)
		return
	}
	s.soldOut.Delete(m.ActivityID)
}

// Preheat 预热即将开始的活动：库存写入 Redis、创建消费者组并启动该活动的消费者。
func (s *Service) Preheat(ctx context.Context, a *model.Activity) error {
	if _, err := s.cache.Preheat(ctx, a); err != nil {
		return err
	}
	stream := cache.StreamKey(a.ID)
	if err := mq.EnsureGroup(ctx, s.cache.Client(), stream, s.cfg.ConsumerGroup); err != nil {
		return err
	}
	if _, loaded := s.consumers.LoadOrStore(a.ID, struct{}{}); !loaded {
		c := &mq.Consumer{
			RDB: s.cache.Client(), Stream: stream, Group: s.cfg.ConsumerGroup,
			Name: s.cfg.ConsumerName, ActivityID: a.ID, MaxRetry: s.cfg.MaxRetry,
			ClaimIdle: s.cfg.ClaimIdle, DLQ: cache.DeadLetterKey,
			Handle: s.HandleOrder, OnDead: s.OnDeadLetter, Log: s.log,
		}
		go c.Run(s.bgCtx)
		s.log.Info("consumer started", "activity", a.ID)
	}
	return nil
}

// PreheatByID 管理接口：手动预热指定活动。
func (s *Service) PreheatByID(ctx context.Context, aid int64) error {
	a, err := s.store.GetActivity(ctx, aid)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.Preheat(ctx, a)
}

// PreheatUpcoming 定时任务：预热未来 PreheatIn 内开始的活动。
func (s *Service) PreheatUpcoming(ctx context.Context) {
	list, err := s.store.ListUpcoming(ctx, time.Now().Add(s.cfg.PreheatIn))
	if err != nil {
		s.log.Error("list upcoming", "err", err)
		return
	}
	for _, a := range list {
		if err := s.Preheat(ctx, a); err != nil {
			s.log.Error("preheat", "activity", a.ID, "err", err)
		}
	}
}

// UpdateActivityInfo 后台修改活动：先更新数据库，再删除缓存。
func (s *Service) UpdateActivityInfo(ctx context.Context, a *model.Activity) error {
	if err := s.store.UpdateActivityInfo(ctx, a); err != nil {
		return err
	}
	var err error
	for i := 0; i < 3; i++ { // 删除失败重试；仍失败则依赖 TTL 兜底
		if err = s.cache.InvalidateActivity(ctx, a.ID); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
	return err
}

// Reconcile 对账：Redis 已购人数应等于 MySQL 订单数 + 在途消息数。
// 这里只做告警，修复由人工或补偿任务完成。
func (s *Service) Reconcile(ctx context.Context) {
	list, err := s.store.ListUpcoming(ctx, time.Now())
	if err != nil {
		s.log.Error("reconcile list", "err", err)
		return
	}
	for _, a := range list {
		stock, buyers, err := s.cache.StockSnapshot(ctx, a.ID)
		if err != nil {
			continue
		}
		orders, err := s.store.CountOrders(ctx, a.ID)
		if err != nil {
			continue
		}
		if stock+buyers != a.TotalStock || orders > buyers {
			metrics.ReconcileDiff.Add(1)
			s.log.Warn("reconcile mismatch", "activity", a.ID, "redis_stock", stock,
				"redis_buyers", buyers, "db_orders", orders, "total", a.TotalStock)
		}
	}
}
