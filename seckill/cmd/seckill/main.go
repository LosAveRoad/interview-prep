// Command seckill 启动秒杀服务。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lisa/seckill/internal/api"
	"github.com/lisa/seckill/internal/cache"
	"github.com/lisa/seckill/internal/config"
	"github.com/lisa/seckill/internal/idgen"
	"github.com/lisa/seckill/internal/service"
	"github.com/lisa/seckill/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(cfg.MySQLDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	// UniversalClient：Addrs 只有一个地址时为单机客户端，多个地址时为 Cluster 客户端
	rdb := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:        []string{cfg.RedisAddr},
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		PoolSize:     200,
		DialTimeout:  time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	})
	defer rdb.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.Ping(pingCtx); err != nil {
		return err
	}
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return err
	}

	ids, err := idgen.New(cfg.NodeID)
	if err != nil {
		return err
	}
	svc := service.New(ctx, cfg, cache.New(rdb, cfg.ResultTTL), db, ids, log)

	// 后台任务：预热即将开始的活动、定期对账
	go every(ctx, 30*time.Second, svc.PreheatUpcoming)
	go every(ctx, time.Minute, svc.Reconcile)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(svc, cfg.GlobalQPS, cfg.GlobalBurst, cfg.AdminToken, log).Routes(),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	// 优雅退出：停止接收新请求，等待处理中的请求完成
	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	return srv.Shutdown(shutdownCtx)
}

// every 立即执行一次 fn，之后每隔 d 执行一次，直到 ctx 取消。
func every(ctx context.Context, d time.Duration, fn func(context.Context)) {
	fn(ctx)
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
}
