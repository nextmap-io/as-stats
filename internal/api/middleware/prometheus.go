package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/metrics"
)

// Prometheus returns a chi middleware that records per-request latency and
// status code metrics. Route patterns are normalised via chi's
// RouteContext().RoutePattern() so dynamic segments (e.g. /ip/1.2.3.4)
// don't explode the label cardinality.
func Prometheus() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)

			// chi populates the route pattern after the router matches,
			// which happens inside next.ServeHTTP. Reading it here (after
			// the handler returns) gives us the normalised path.
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}

			code := strconv.Itoa(ww.status)
			metrics.HTTPRequestsTotal.WithLabelValues(r.Method, route, code).Inc()
			metrics.HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}
