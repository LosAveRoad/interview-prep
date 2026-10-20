// Package api 提供 HTTP 接口（Go 1.22+ ServeMux 路由模式）。
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"expvar"
	"log/slog"
	"net/http"
	"strconv"

	"golang.org/x/time/rate"

	"github.com/lisa/seckill/internal/metrics"
	"github.com/lisa/seckill/internal/service"
)

// Handler 持有业务服务与限流器。
type Handler struct {
	svc        *service.Service
	limiter    *rate.Limiter // 单实例令牌桶，保护本实例与下游
	adminToken string
	log        *slog.Logger
}

// New 创建 Handler。
func New(svc *service.Service, qps float64, burst int, adminToken string, log *slog.Logger) *Handler {
	return &Handler{svc: svc, limiter: rate.NewLimiter(rate.Limit(qps), burst),
		adminToken: adminToken, log: log}
}

// Routes 注册路由。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/activities/{id}", h.getActivity)
	mux.Handle("POST /api/v1/activities/{id}/token", h.rateLimit(http.HandlerFunc(h.issueToken)))
	mux.Handle("POST /api/v1/activities/{id}/seckill", h.rateLimit(http.HandlerFunc(h.seckill)))
	mux.HandleFunc("GET /api/v1/activities/{id}/result", h.result)
	mux.HandleFunc("POST /admin/activities/{id}/preheat", h.preheat)
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// rateLimit 令牌桶限流中间件，拿不到令牌立即返回 429（不排队等待）。
func (h *Handler) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.limiter.Allow() {
			metrics.RateLimited.Add(1)
			writeErr(w, http.StatusTooManyRequests, "server busy, please retry")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) getActivity(w http.ResponseWriter, r *http.Request) {
	aid, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Activity(r.Context(), aid)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) issueToken(w http.ResponseWriter, r *http.Request) {
	aid, ok := pathID(w, r)
	if !ok {
		return
	}
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	token, err := h.svc.IssueToken(r.Context(), aid, uid)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *Handler) seckill(w http.ResponseWriter, r *http.Request) {
	aid, ok := pathID(w, r)
	if !ok {
		return
	}
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.Token == "" {
		writeErr(w, http.StatusBadRequest, "token required")
		return
	}
	oid, err := h.svc.Seckill(r.Context(), aid, uid, req.Token)
	if err != nil {
		h.fail(w, err)
		return
	}
	// 202 Accepted：请求已受理，订单异步创建，客户端轮询 /result
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "queued", "order_id": strconv.FormatInt(oid, 10)})
}

func (h *Handler) result(w http.ResponseWriter, r *http.Request) {
	aid, ok := pathID(w, r)
	if !ok {
		return
	}
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	res, err := h.svc.Result(r.Context(), aid, uid)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) preheat(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Admin-Token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.adminToken)) != 1 {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	aid, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.PreheatByID(r.Context(), aid); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "preheated"})
}

// fail 把业务错误映射为 HTTP 状态码；未知错误只记录日志，不向客户端暴露细节。
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSoldOut), errors.Is(err, service.ErrRepeat):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrTooMany):
		writeErr(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, service.ErrBadToken):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNotStarted), errors.Is(err, service.ErrEnded),
		errors.Is(err, service.ErrNotPreheated):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		h.log.Error("internal error", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid activity id")
		return 0, false
	}
	return id, true
}

// userID 演示用：从网关注入的 X-User-ID 头读取用户。生产环境应由网关校验 JWT 后注入。
func userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	uid, err := strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	if err != nil || uid <= 0 {
		writeErr(w, http.StatusUnauthorized, "login required")
		return 0, false
	}
	return uid, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
