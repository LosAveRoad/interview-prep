-- 下单失败时回滚预扣：幂等，只有用户仍在已购集合中才回滚。
-- KEYS[1] 库存  KEYS[2] 已购用户  KEYS[3] 抢购结果
-- ARGV[1] user_id  ARGV[2] 结果值  ARGV[3] 结果 TTL(秒)  ARGV[4] 是否归还库存(1/0)
if redis.call('SREM', KEYS[2], ARGV[1]) == 1 then
    if ARGV[4] == '1' then
        redis.call('INCR', KEYS[1])
    end
    redis.call('SET', KEYS[3], ARGV[2], 'EX', ARGV[3])
    return 1
end
return 0
