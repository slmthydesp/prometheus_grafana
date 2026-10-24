package main

import (
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func okDelay(n int) time.Duration {
	return time.Duration(20+n) * time.Millisecond
}

func failDelay() time.Duration {
	return 5 * time.Millisecond
}

type metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests processed, partitioned by method, path, and status code.",
		}, []string{"method", "path", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		}, []string{"method", "path"}),
		inflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}
	reg.MustRegister(m.requests, m.duration, m.inflight)
	return m
}

type statusRecorder struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wrote {
		return
	}
	s.wrote = true
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}

func instrument(m *metrics, path string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inflight.Inc()
		defer m.inflight.Dec()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next(rec, r)
		m.requests.WithLabelValues(r.Method, path, strconv.Itoa(rec.code)).Inc()
		m.duration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}

func buildMux(m *metrics, metricsH http.Handler, sleep func(time.Duration), okN func() int) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metricsH)
	mux.Handle("GET /ok", instrument(m, "/ok", func(w http.ResponseWriter, r *http.Request) {
		sleep(okDelay(okN()))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	mux.Handle("GET /fail", instrument(m, "/fail", func(w http.ResponseWriter, r *http.Request) {
		sleep(failDelay())
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "fail")
	}))
	mux.Handle("/", instrument(m, "other", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found")
	}))
	return mux
}

func main() {
	m := newMetrics(prometheus.DefaultRegisterer)
	mux := buildMux(m, promhttp.Handler(), time.Sleep, func() int { return rand.Intn(61) })
	srv := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
