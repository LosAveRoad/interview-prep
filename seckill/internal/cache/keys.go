package cache

import "fmt"

// Redis key 设计：业务前缀 sk + hash tag {activityID}。
// 同一活动的库存、已购集合、消息流、结果放在同一个 slot，Lua 脚本才能在 Cluster 模式下执行。

// StockKey 预扣库存（String）。
func StockKey(aid int64) string { return fmt.Sprintf("sk:{%d}:stock", aid) }

// UsersKey 已抢到的用户集合（Set），实现一人一单。
func UsersKey(aid int64) string { return fmt.Sprintf("sk:{%d}:users", aid) }

// StreamKey 订单消息流（Stream），每个活动一个，避免跨 slot。
func StreamKey(aid int64) string { return fmt.Sprintf("sk:{%d}:orders", aid) }

// InfoKey 活动详情缓存（String/JSON），Cache Aside。
func InfoKey(aid int64) string { return fmt.Sprintf("sk:{%d}:info", aid) }

// ResultKey 用户抢购结果（String），供前端轮询。
func ResultKey(aid, uid int64) string { return fmt.Sprintf("sk:{%d}:result:%d", aid, uid) }

// TokenKey 秒杀令牌（String），隐藏真实秒杀接口、一次性使用。
func TokenKey(aid, uid int64) string { return fmt.Sprintf("sk:{%d}:token:%d", aid, uid) }

// UserRateKey 用户级限流计数（String），按秒分窗口。
func UserRateKey(uid, sec int64) string { return fmt.Sprintf("sk:rl:%d:%d", uid, sec) }

// DeadLetterKey 死信流，记录多次重试仍失败的下单消息。
const DeadLetterKey = "sk:dlq"
