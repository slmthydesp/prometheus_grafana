package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestDelays(t *testing.T) {
	if okDelay(0) != 20*time.Millisecond {
		t.Fatalf("okDelay(0)=%s", okDelay(0))
	}
	if okDelay(60) != 80*time.Millisecond {
		t.Fatalf("okDelay(60)=%s", okDelay(60))
	}
	if failDelay() != 5*time.Millisecond {
		t.Fatalf("failDelay=%s", failDelay())
	}
}

func TestOKRecordsMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)
	var slept []time.Duration
	h := buildMux(m, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), func(d time.Duration) {
		if got := testutil.ToFloat64(m.inflight); got != 1 {
			t.Fatalf("inflight during request = %v", got)
		}
		slept = append(slept, d)
	}, func() int { return 0 })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if len(slept) != 1 || slept[0] != 20*time.Millisecond {
		t.Fatalf("slept=%v", slept)
	}
	if got := testutil.ToFloat64(m.inflight); got != 0 {
		t.Fatalf("inflight after = %v", got)
	}
	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/ok", "200")); got != 1 {
		t.Fatalf("requests=%v", got)
	}
	if histogramCount(t, m.duration, "GET", "/ok") != 1 {
		t.Fatal("duration not observed")
	}
}

func TestFail(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)
	var slept []time.Duration
	h := buildMux(m, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), func(d time.Duration) {
		slept = append(slept, d)
	}, func() int { return 0 })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != "fail" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if len(slept) != 1 || slept[0] != 5*time.Millisecond {
		t.Fatalf("slept=%v", slept)
	}
	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/fail", "500")); got != 1 {
		t.Fatalf("requests=%v", got)
	}
}

func TestOtherPath(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)
	h := buildMux(m, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), func(time.Duration) {}, func() int { return 0 })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "not found" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "other", "404")); got != 1 {
		t.Fatalf("requests=%v", got)
	}
}

func TestMetricsEndpointIsNotCounted(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := newMetrics(reg)
	h := buildMux(m, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), func(time.Duration) {}, func() int { return 0 })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if testutil.CollectAndCount(m.requests) != 0 {
		t.Fatal("scraping /metrics created a business counter")
	}
	// promhttp omits HELP/TYPE for a counter that has no samples yet.
	if strings.Contains(rec.Body.String(), `path="/ok"`) {
		t.Fatal(`metrics exposition contained path="/ok" before any business request`)
	}

	okRec := httptest.NewRecorder()
	h.ServeHTTP(okRec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/ok", "200")); got != 1 {
		t.Fatalf("requests after /ok=%v", got)
	}

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d", rec2.Code)
	}
	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/ok", "200")); got != 1 {
		t.Fatalf("scraping /metrics changed counter from 1 to %v", got)
	}
}

func histogramCount(t *testing.T, h *prometheus.HistogramVec, method, path string) uint64 {
	t.Helper()
	metric, ok := h.WithLabelValues(method, path).(prometheus.Metric)
	if !ok {
		t.Fatal("histogram observer is not a metric")
	}
	var out dto.Metric
	if err := metric.Write(&out); err != nil {
		t.Fatal(err)
	}
	return out.GetHistogram().GetSampleCount()
}
