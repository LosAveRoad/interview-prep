-- 库存预扣 + 一人一单 + 投递订单消息，整段脚本在 Redis 中原子执行。
-- 所有 KEY 使用同一个 hash tag {activityID}，保证 Redis Cluster 下落在同一个 slot。
-- KEYS[1] 库存        sk:{aid}:stock   (string, 整数)
-- KEYS[2] 已购用户    sk:{aid}:users   (set)
-- KEYS[3] 订单消息流  sk:{aid}:orders  (stream)
-- KEYS[4] 抢购结果    sk:{aid}:result:{uid} (string)
-- ARGV[1] user_id  ARGV[2] order_id  ARGV[3] 请求时间戳(ms)  ARGV[4] 结果 TTL(秒)
-- 返回：>=0 扣减后剩余库存；-1 已售罄；-2 重复购买；-3 活动未预热
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 1 then
    return -2
end
local stock = redis.call('GET', KEYS[1])
if not stock then
    return -3
end
stock = tonumber(stock)
if stock <= 0 then
    return -1
end
redis.call('DECR', KEYS[1])
redis.call('SADD', KEYS[2], ARGV[1])
redis.call('XADD', KEYS[3], 'MAXLEN', '~', '1000000', '*',
    'uid', ARGV[1], 'oid', ARGV[2], 'ts', ARGV[3])
redis.call('SET', KEYS[4], 'queued:' .. ARGV[2], 'EX', ARGV[4])
return stock - 1
