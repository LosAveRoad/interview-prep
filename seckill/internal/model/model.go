// Package model 定义领域模型。
package model

import "time"

// 活动状态。
const (
	ActivityDraft     int8 = 0
	ActivityPublished int8 = 1
	ActivityOffline   int8 = 2
)

// 订单状态。
const (
	OrderUnpaid   int8 = 0
	OrderPaid     int8 = 1
	OrderCanceled int8 = 2
)

// Activity 对应表 seckill_activity。
type Activity struct {
	ID         int64     `json:"id"`
	GoodsID    int64     `json:"goods_id"`
	Title      string    `json:"title"`
	Price      string    `json:"price"` // DECIMAL 以字符串承载，避免浮点误差
	TotalStock int64     `json:"total_stock"`
	Stock      int64     `json:"stock"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Status     int8      `json:"status"`
	Version    int64     `json:"version"`
}

// Order 对应表 seckill_order。
type Order struct {
	ID         int64     `json:"id"`
	ActivityID int64     `json:"activity_id"`
	UserID     int64     `json:"user_id"`
	GoodsID    int64     `json:"goods_id"`
	Price      string    `json:"price"`
	Status     int8      `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}
