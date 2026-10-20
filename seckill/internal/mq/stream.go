// Package mq 基于 Redis Stream 实现异步下单消息队列（消费者组 + ACK + 重试 + 死信）。
package mq

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// OrderMsg 是下单消息。
type OrderMsg struct {
	ActivityID int64
	UserID     int64
	OrderID    int64
	TS         int64 // 用户请求时间（毫秒）
}

// ErrPermanent 包装不可重试的错误（如消息格式错误），消费者直接 ACK。
var ErrPermanent = errors.New("mq: permanent error")

// Handler 处理一条消息。返回 nil 或 ErrPermanent 时 ACK；其他错误保留在 PEL 中等待重试。
type Handler func(ctx context.Context, m OrderMsg) error

// DeadLetterFunc 在消息超过最大投递次数后调用，用于补偿（回滚预扣）。
type DeadLetterFunc func(ctx context.Context, m OrderMsg)

// EnsureGroup 创建消费者组（流不存在时一并创建），已存在则忽略。
func EnsureGroup(ctx context.Context, rdb redis.UniversalClient, stream, group string) error {
	err := rdb.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// Consumer 消费一个活动的订单流。
type Consumer struct {
	RDB        redis.UniversalClient
	Stream     string
	Group      string
	Name       string
	ActivityID int64
	MaxRetry   int64
	ClaimIdle  time.Duration
	DLQ        string
	Handle     Handler
	OnDead     DeadLetterFunc
	Log        *slog.Logger
}

// Run 阻塞运行直到 ctx 取消。
func (c *Consumer) Run(ctx context.Context) {
	go c.reclaimLoop(ctx)
	for ctx.Err() == nil {
		streams, err := c.RDB.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.Group,
			Consumer: c.Name,
			Streams:  []string{c.Stream, ">"}, // ">" 只读取从未投递过的新消息
			Count:    32,
			Block:    2 * time.Second,
		}).Result()
		if errors.Is(err, redis.Nil) || ctx.Err() != nil {
			continue
		}
		if err != nil {
			c.Log.Error("xreadgroup", "stream", c.Stream, "err", err)
			time.Sleep(time.Second)
			continue
		}
		for _, s := range streams {
			for _, msg := range s.Messages {
				c.process(ctx, msg)
			}
		}
	}
}

func (c *Consumer) process(ctx context.Context, msg redis.XMessage) {
	m, err := c.decode(msg)
	if err == nil {
		err = c.Handle(ctx, m)
	}
	if err != nil && !errors.Is(err, ErrPermanent) {
		// 不 ACK：消息留在 PEL（Pending Entries List），由 reclaimLoop 重新认领
		c.Log.Warn("handle order failed, will retry", "id", msg.ID, "err", err)
		return
	}
	if err != nil {
		c.Log.Error("drop bad message", "id", msg.ID, "err", err)
	}
	if err := c.RDB.XAck(ctx, c.Stream, c.Group, msg.ID).Err(); err != nil {
		c.Log.Error("xack", "id", msg.ID, "err", err)
	}
}

// reclaimLoop 周期性检查 PEL：空闲过久的消息重新认领处理；超过最大投递次数的进入死信。
func (c *Consumer) reclaimLoop(ctx context.Context) {
	t := time.NewTicker(c.ClaimIdle / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pending, err := c.RDB.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: c.Stream, Group: c.Group, Idle: c.ClaimIdle,
			Start: "-", End: "+", Count: 100,
		}).Result()
		if err != nil {
			c.Log.Error("xpending", "err", err)
			continue
		}
		for _, p := range pending {
			msgs, err := c.RDB.XClaim(ctx, &redis.XClaimArgs{
				Stream: c.Stream, Group: c.Group, Consumer: c.Name,
				MinIdle: c.ClaimIdle, Messages: []string{p.ID},
			}).Result()
			if err != nil || len(msgs) == 0 {
				continue // 已被其他消费者认领
			}
			if p.RetryCount >= c.MaxRetry {
				c.deadLetter(ctx, msgs[0])
				continue
			}
			c.process(ctx, msgs[0])
		}
	}
}

func (c *Consumer) deadLetter(ctx context.Context, msg redis.XMessage) {
	values := map[string]any{"stream": c.Stream, "msg_id": msg.ID}
	for k, v := range msg.Values {
		values[k] = v
	}
	if err := c.RDB.XAdd(ctx, &redis.XAddArgs{Stream: c.DLQ, Values: values}).Err(); err != nil {
		c.Log.Error("write dlq", "id", msg.ID, "err", err)
		return // 写死信失败则不 ACK，下轮再试
	}
	if m, err := c.decode(msg); err == nil && c.OnDead != nil {
		c.OnDead(ctx, m)
	}
	_ = c.RDB.XAck(ctx, c.Stream, c.Group, msg.ID).Err()
	c.Log.Error("message moved to dead letter queue", "id", msg.ID)
}

func (c *Consumer) decode(msg redis.XMessage) (OrderMsg, error) {
	get := func(k string) (int64, error) {
		s, _ := msg.Values[k].(string)
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, errors.Join(ErrPermanent, err)
		}
		return n, nil
	}
	uid, err1 := get("uid")
	oid, err2 := get("oid")
	ts, _ := get("ts")
	if err := errors.Join(err1, err2); err != nil {
		return OrderMsg{}, err
	}
	return OrderMsg{ActivityID: c.ActivityID, UserID: uid, OrderID: oid, TS: ts}, nil
}
