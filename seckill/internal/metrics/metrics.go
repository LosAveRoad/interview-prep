// Package metrics 用标准库 expvar 暴露计数器（GET /debug/vars），生产可替换为 Prometheus。
package metrics

import "expvar"

var (
	Requests      = expvar.NewInt("seckill_requests_total")
	RateLimited   = expvar.NewInt("seckill_rate_limited_total")
	SoldOut       = expvar.NewInt("seckill_sold_out_total")
	Repeat        = expvar.NewInt("seckill_repeat_total")
	Queued        = expvar.NewInt("seckill_queued_total")
	OrderCreated  = expvar.NewInt("seckill_order_created_total")
	OrderFailed   = expvar.NewInt("seckill_order_failed_total")
	DeadLetters   = expvar.NewInt("seckill_dead_letter_total")
	ReconcileDiff = expvar.NewInt("seckill_reconcile_mismatch_total")
)
