package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type ctxKey int

const loggerCtxKey ctxKey = iota

// requestLogger returns a chi middleware that attaches a request-scoped
// zap logger (with request_id) to the context and emits one structured
// access-log line per request after the handler returns.
func requestLogger(base *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			reqID := middleware.GetReqID(r.Context())
			reqLog := base.With(zap.String("request_id", reqID))
			ctx := context.WithValue(r.Context(), loggerCtxKey, reqLog)

			next.ServeHTTP(ww, r.WithContext(ctx))

			base.Info("request",
				zap.String("request_id", reqID),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.Int("bytes", ww.BytesWritten()),
				zap.Duration("duration", time.Since(start)),
				zap.String("remote_ip", r.RemoteAddr),
			)
		})
	}
}

// loggerFrom returns the request-scoped logger installed by requestLogger,
// or a no-op logger if none is present (keeps tests and direct handler use
// from needing special setup).
func loggerFrom(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerCtxKey).(*zap.Logger); ok {
		return l
	}
	return zap.NewNop()
}
