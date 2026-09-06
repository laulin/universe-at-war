package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
	"universeatwar/internal/observability"
)

type contextKey int

const requestIDKey contextKey = iota

// requestID tags every request with a correlation identifier and echoes it so
// an operator can match a browser error with a log line.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			next.ServeHTTP(response, request)
			return
		}
		identifier := hex.EncodeToString(raw[:])
		response.Header().Set("X-Request-Id", identifier)
		next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), requestIDKey, identifier)))
	})
}

// RequestIDFrom returns the correlation identifier of the current request.
func RequestIDFrom(ctx context.Context) string {
	identifier, _ := ctx.Value(requestIDKey).(string)
	return identifier
}

// statusRecorder remembers the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

// requestLogger records one line per request. Only the path is logged: the
// query string and the cookies may carry secrets.
func requestLogger(logger *slog.Logger, metrics *observability.Metrics, next http.Handler) http.Handler {
	if logger == nil && metrics == nil {
		return next
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: response}
		next.ServeHTTP(recorder, request)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.Request(status, time.Since(started))
		if logger == nil {
			return
		}
		logger.Info("http request",
			"method", request.Method,
			"path", request.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", RequestIDFrom(request.Context()),
		)
	})
}
