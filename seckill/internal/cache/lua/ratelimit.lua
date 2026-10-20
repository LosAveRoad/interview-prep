-- 固定窗口限流：INCR 与 EXPIRE 在同一脚本内执行，避免产生没有过期时间的计数 key。
-- KEYS[1] 计数 key  ARGV[1] 窗口内最大请求数  ARGV[2] 窗口长度(秒)
-- 返回 1 放行，0 拒绝
local n = redis.call('INCR', KEYS[1])
if n == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[2])
end
if n > tonumber(ARGV[1]) then
    return 0
end
return 1
