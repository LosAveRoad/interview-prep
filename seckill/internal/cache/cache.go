// Package cache 封装秒杀相关的 Redis 操作。
package cache

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/lisa/seckill/internal/model"
)

var (
	//go:embed lua/deduct.lua
	deductSrc string
	//go:embed lua/rollback.lua
	rollbackSrc string
	//go:embed lua/ratelimit.lua
	rateLimitSrc string

	// redis.Script 先用 EVALSHA 执行，脚本未缓存（NOSCRIPT）时自动回退到 EVAL。
	deductScript    = redis.NewScript(deductSrc)
	rollbackScript  = redis.NewScript(rollbackSrc)
	rateLimitScript = redis.NewScript(rateLimitSrc)
)

// Deduct 的返回码。
const (
	DeductSoldOut      int64 = -1
	DeductRepeat       int64 = -2
	DeductNotPreheated int64 = -3
)

// emptyPlaceholder 缓存空值，防止缓存穿透。
const emptyPlaceholder = "<nil>"

// ErrActivityNotFound 表示活动不存在（含命中空值缓存）。
var ErrActivityNotFound = errors.New("cache: activity not found")

// Cache 是 Redis 访问层。
type Cache struct {
	rdb       redis.UniversalClient
	sf        singleflight.Group
	resultTTL time.Duration
}

// New 创建 Cache。UniversalClient 兼容单机、哨兵与 Cluster 模式。
func New(rdb redis.UniversalClient, resultTTL time.Duration) *Cache {
	return &Cache{rdb: rdb, resultTTL: resultTTL}
}

// Client 返回底层客户端（供消息队列使用）。
func (c *Cache) Client() redis.UniversalClient { return c.rdb }

// Preheat 把活动库存加载到 Redis。使用 SETNX：活动进行中重复预热不会覆盖已扣减的库存。
func (c *Cache) Preheat(ctx context.Context, a *model.Activity) (bool, error) {
	ttl := time.Until(a.EndTime) + 24*time.Hour // 活动结束后再保留一天用于对账
	return c.rdb.SetNX(ctx, StockKey(a.ID), a.Stock, ttl).Result()
}

// Deduct 执行库存预扣 Lua 脚本，返回剩余库存或负数返回码。
func (c *Cache) Deduct(ctx context.Context, aid, uid, oid int64) (int64, error) {
	keys := []string{StockKey(aid), UsersKey(aid), StreamKey(aid), ResultKey(aid, uid)}
	return deductScript.Run(ctx, c.rdb, keys,
		uid, oid, time.Now().UnixMilli(), int64(c.resultTTL/time.Second)).Int64()
}

// Rollback 回滚预扣（幂等）。restoreStock=false 用于数据库已售罄、不应再归还库存的场景。
func (c *Cache) Rollback(ctx context.Context, aid, uid int64, result string, restoreStock bool) (bool, error) {
	restore := "0"
	if restoreStock {
		restore = "1"
	}
	keys := []string{StockKey(aid), UsersKey(aid), ResultKey(aid, uid)}
	n, err := rollbackScript.Run(ctx, c.rdb, keys,
		uid, result, int64(c.resultTTL/time.Second), restore).Int64()
	return n == 1, err
}

// AllowUser 用户级限流：每秒最多 limit 次请求。
func (c *Cache) AllowUser(ctx context.Context, uid int64, limit int) (bool, error) {
	key := UserRateKey(uid, time.Now().Unix())
	n, err := rateLimitScript.Run(ctx, c.rdb, []string{key}, limit, 2).Int64()
	return n == 1, err
}

// SetToken 保存一次性秒杀令牌。
func (c *Cache) SetToken(ctx context.Context, aid, uid int64, token string, ttl time.Duration) error {
	return c.rdb.Set(ctx, TokenKey(aid, uid), token, ttl).Err()
}

// ConsumeToken 原子地取出并删除令牌（GETDEL，Redis 6.2+），保证令牌只能使用一次。
func (c *Cache) ConsumeToken(ctx context.Context, aid, uid int64, token string) (bool, error) {
	v, err := c.rdb.GetDel(ctx, TokenKey(aid, uid)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == token, nil
}

// SetResult 写入抢购结果。
func (c *Cache) SetResult(ctx context.Context, aid, uid int64, result string) error {
	return c.rdb.Set(ctx, ResultKey(aid, uid), result, c.resultTTL).Err()
}

// GetResult 读取抢购结果，不存在返回空串。
func (c *Cache) GetResult(ctx context.Context, aid, uid int64) (string, error) {
	v, err := c.rdb.Get(ctx, ResultKey(aid, uid)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// GetActivity 按 Cache Aside 读取活动详情：
//   - 命中直接返回；命中空值占位符返回 ErrActivityNotFound（防穿透）；
//   - 未命中时用 singleflight 合并并发回源（防击穿）；
//   - 回写缓存时 TTL 加随机抖动（防雪崩）。
func (c *Cache) GetActivity(ctx context.Context, aid int64,
	load func(context.Context, int64) (*model.Activity, error)) (*model.Activity, error) {

	key := InfoKey(aid)
	if raw, err := c.rdb.Get(ctx, key).Result(); err == nil {
		if raw == emptyPlaceholder {
			return nil, ErrActivityNotFound
		}
		var a model.Activity
		if err := json.Unmarshal([]byte(raw), &a); err == nil {
			return &a, nil
		}
	} else if !errors.Is(err, redis.Nil) {
		// Redis 故障时降级直接查库（由上层限流保护数据库）
		return load(ctx, aid)
	}

	v, err, _ := c.sf.Do(key, func() (any, error) {
		a, err := load(ctx, aid)
		if errors.Is(err, ErrActivityNotFound) {
			_ = c.rdb.Set(ctx, key, emptyPlaceholder, time.Minute).Err()
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		if b, err := json.Marshal(a); err == nil {
			ttl := 10*time.Minute + time.Duration(rand.IntN(120))*time.Second
			_ = c.rdb.Set(ctx, key, b, ttl).Err()
		}
		return a, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*model.Activity), nil
}

// InvalidateActivity 删除活动缓存（先更新数据库，再删除缓存）。
func (c *Cache) InvalidateActivity(ctx context.Context, aid int64) error {
	return c.rdb.Del(ctx, InfoKey(aid)).Err()
}

// StockSnapshot 读取 Redis 中的剩余库存与已购人数，用于对账。
func (c *Cache) StockSnapshot(ctx context.Context, aid int64) (stock, buyers int64, err error) {
	pipe := c.rdb.Pipeline()
	stockCmd := pipe.Get(ctx, StockKey(aid))
	buyersCmd := pipe.SCard(ctx, UsersKey(aid))
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, 0, err
	}
	stock, _ = strconv.ParseInt(stockCmd.Val(), 10, 64)
	return stock, buyersCmd.Val(), nil
}

// ParseResult 解析结果字符串 "queued:<oid>" / "success:<oid>" / "failed:<reason>"。
func ParseResult(v string) (status, detail string) {
	status, detail, _ = strings.Cut(v, ":")
	return status, detail
}
