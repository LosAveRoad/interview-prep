// Package config 从环境变量加载配置，给出适合本地演示的默认值。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 是服务运行所需的全部配置。
type Config struct {
	HTTPAddr      string
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	AdminToken    string // 管理接口口令，生产环境应接入统一鉴权

	NodeID int64 // 雪花算法机器号，0~1023，多实例部署时必须唯一

	GlobalQPS     float64 // 单实例令牌桶速率
	GlobalBurst   int     // 单实例令牌桶容量
	UserReqPerSec int     // 单用户每秒最多请求数（Redis 固定窗口）

	ConsumerGroup string
	ConsumerName  string
	MaxRetry      int64         // 消息最大投递次数，超过进入死信
	ClaimIdle     time.Duration // pending 消息空闲多久后被重新认领

	TokenTTL  time.Duration // 秒杀令牌有效期
	ResultTTL time.Duration // 抢购结果保留时间
	PreheatIn time.Duration // 提前多久预热活动
}

// Load 读取环境变量。
func Load() Config {
	host, _ := os.Hostname()
	return Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		MySQLDSN:      env("MYSQL_DSN", "root:root@tcp(127.0.0.1:3306)/seckill?parseTime=true&loc=Local&charset=utf8mb4"),
		RedisAddr:     env("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: env("REDIS_PASSWORD", ""),
		RedisDB:       envInt("REDIS_DB", 0),
		AdminToken:    env("ADMIN_TOKEN", "change-me"),
		NodeID:        int64(envInt("NODE_ID", 1)),
		GlobalQPS:     float64(envInt("GLOBAL_QPS", 5000)),
		GlobalBurst:   envInt("GLOBAL_BURST", 1000),
		UserReqPerSec: envInt("USER_REQ_PER_SEC", 5),
		ConsumerGroup: env("CONSUMER_GROUP", "order-creator"),
		ConsumerName:  env("CONSUMER_NAME", host),
		MaxRetry:      int64(envInt("MAX_RETRY", 5)),
		ClaimIdle:     30 * time.Second,
		TokenTTL:      60 * time.Second,
		ResultTTL:     30 * time.Minute,
		PreheatIn:     10 * time.Minute,
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
