# Prometheus + Grafana Lab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在本机用一个 Go HTTP 服务、Prometheus、Grafana 和 Alertmanager 做成可运行的教学案例，打开就能看到请求量、错误率、耗时和一条会响的错误率告警。

**Architecture:** `loadgen` 按 `FAIL_RATIO` 打 `/ok` 和 `/fail`。`api` 把请求记成 counter、histogram、gauge，并在 `/metrics` 暴露。Prometheus 每 5 秒抓取、每 15 秒算规则，把 `HighHttpErrorRate` 发给只在页面上显示的 Alertmanager。Grafana 用 provisioning 打开就有看板 `HTTP RED`。

**Tech Stack:** Go 1.27.3、`github.com/prometheus/client_golang` v1.25.0、Prometheus v2.55.1、Alertmanager v0.34.1、Grafana 13.2.4、Alpine 3.22、Docker Compose。

**Spec:** `docs/superpowers/specs/2026-10-24-prometheus-grafana-lab-design.md`

## Global Constraints

- 五个服务名：`api`、`loadgen`、`prometheus`、`alertmanager`、`grafana`。同一 Compose 网络，用服务名互访。
- 发布端口：api `8080`，Prometheus `9090`，Grafana `3000`，Alertmanager `9093`。
- Grafana 登录 `admin` / `admin`。匿名访问关闭。
- 告警接收器名 `devnull`，不配置 webhook、邮件或 Slack。
- 不挂数据卷。配置文件用只读 bind mount。`docker compose down` 后这次监控数据清空。
- 镜像标签写死，不用 `latest`：`golang:1.27.3-alpine`、`alpine:3.22`、`prom/prometheus:v2.55.1`、`prom/alertmanager:v0.34.1`、`grafana/grafana:13.2.4`。
- Prometheus 配置保持 2.x 写法。`scrape_interval: 5s`，`evaluation_interval: 15s`。
- 指标名与标签：`http_requests_total{method,path,code}`，`http_request_duration_seconds{method,path}`，`http_requests_in_flight`。直方图桶：`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1`。
- `path` 只允许 `/ok`、`/fail`、`other`。`/metrics` 不计入这三个业务指标。
- `GET /ok` 正文 `ok`、状态 200、睡眠 20–80ms（含两端）。`GET /fail` 正文 `fail`、状态 500、睡眠 5ms。其他路径正文 `not found`、状态 404、path 为 `other`。
- `FAIL_RATIO` 默认 `0.1`，必须是 0 到 1 的数（含两端）。`0.1` 的阈值按整数截断是 100。非法值时 loadgen 退出。
- 告警名 `HighHttpErrorRate`，组名 `api`，`for: 1m`，标签 `severity: warning`。
- 看板 uid `http-red`，标题 `HTTP RED`，最近 15 分钟，每 5 秒刷新。错误率单位 `percentunit`，阈值线 `0.05`。
- `.gitignore` 继续包含 `.superpowers/`。
- 每个任务结束时提交。提交信息用下面给出的原文。仓库若在上级目录，就在 `git rev-parse --show-toplevel` 指向的根目录提交，不要 `git init`。

## File Structure

- `api/main.go` — 路由、睡眠、三个指标、监听 `0.0.0.0:8080`。
- `api/handler_test.go` — 路由和指标的测试。
- `api/go.mod`、`api/go.sum` — 模块 `example.com/prom-lab/api`。
- `api/Dockerfile` — 多阶段构建，运行用户不是 root。
- `loadgen/decide.sh` — 纯函数：比例是否合法、这一轮打 ok 还是 fail。
- `loadgen/decide_test.sh` — 上面两个函数的断言。
- `loadgen/loop.sh` — 死循环打请求，并把周期补到约 100ms。
- `loadgen/Dockerfile` — Alpine + wget，入口是 `loop.sh`。
- `prometheus/prometheus.yml` — 抓取与告警发送。
- `prometheus/rules.yml` — `HighHttpErrorRate`。
- `alertmanager/alertmanager.yml` — `devnull`。
- `grafana/provisioning/datasources/datasource.yml` — 数据源 uid `prometheus`。
- `grafana/provisioning/dashboards/dashboards.yml` — 文件看板 provider。
- `grafana/dashboards/http-red.json` — 八块面板。
- `docker-compose.yml` — 把五个服务接起来。
- `README.md` — 替换现有的一行标题。

这一个计划就覆盖整个案例。五个容器分开不能各自成为一套能看的教学示例，所以不拆成多份计划。

---

### Task 1: 带指标的 Go API

**Files:**
- Create: `api/go.mod`
- Create: `api/go.sum`
- Create: `api/handler_test.go`
- Create: `api/main.go`
- Create: `api/Dockerfile`
- Test: `api/handler_test.go`

**Interfaces:**
- Consumes: 无。
- Produces: `okDelay(n int) time.Duration`（`n` 为 0..60，返回 `(20+n)ms`）；`failDelay() time.Duration`（`5ms`）；`metrics` 含 `requests *prometheus.CounterVec`、`duration *prometheus.HistogramVec`、`inflight prometheus.Gauge`；`newMetrics(reg prometheus.Registerer) *metrics`；`buildMux(m *metrics, metricsH http.Handler, sleep func(time.Duration), okN func() int) http.Handler`。`main` 用 `prometheus.DefaultRegisterer` 和 `promhttp.Handler()`，监听 `0.0.0.0:8080`。

- [ ] **Step 1: 写会失败的测试**

在 `api/` 下执行：

```bash
go mod init example.com/prom-lab/api
go get github.com/prometheus/client_golang@v1.25.0
go mod tidy
```

把 `api/go.mod` 里的 `go` 行保持为 tidy 写出的版本（工具链是 Go 1.27.3）。创建 `api/handler_test.go`：

```go
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
	if !strings.Contains(rec.Body.String(), "http_requests_total") {
		t.Fatal("metrics exposition missing http_requests_total help or samples setup")
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
```

`TestMetricsEndpointIsNotCounted` 在还没有任何业务请求时，counter 还没有样本，`CollectAndCount` 为 0。响应正文里要有 `http_requests_total` 这个名字：实现时给 counter 注册了 Help，即使没有样本，HELP/TYPE 行也会出现。若 HandlerFor 在没有样本时不输出该名字，把断言改成「正文不含 `path="/ok"`」，并在 `/ok` 之后再请求一次 `/metrics`，确认那一次抓取没有把 counter 从 1 加成 2。优先保持现在的断言；只有 promhttp 的实际输出不带 HELP 行时才改测试。

- [ ] **Step 2: 跑测试，确认失败**

Run: `go test ./...`，工作目录 `api/`。

Expected: FAIL，编译错误 `undefined: okDelay`。

- [ ] **Step 3: 写实现**

创建 `api/main.go`：

```go
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
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	m := newMetrics(prometheus.DefaultRegisterer)
	mux := buildMux(m, promhttp.Handler(), time.Sleep, func() int { return rng.Intn(61) })
	srv := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
```

`GET /ok` 以外的方法打到同一路径时，Go 1.22+ 的 ServeMux 会返回 405，不经过业务计数。loadgen 只用 GET。保持这个行为。

- [ ] **Step 4: 测试通过**

Run: `go test ./...`，工作目录 `api/`。

Expected: PASS。

- [ ] **Step 5: 写 Dockerfile**

创建 `api/Dockerfile`：

```dockerfile
FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api .

FROM alpine:3.22
RUN adduser -D -H -u 65532 app
USER app
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
```

运行镜像里没有 Go 工具链和源码。用户 `app` 的 uid 是 65532。

- [ ] **Step 6: 构建并打一次真实请求**

在仓库的 `prometheus_grafana` 目录：

```bash
docker build -t prom-lab-api ./api
docker rm -f prom-lab-api-smoke >/dev/null 2>&1 || true
docker run -d --name prom-lab-api-smoke -p 18080:8080 prom-lab-api
sleep 1
curl -fsS localhost:18080/ok
curl -fsS localhost:18080/metrics | grep -q 'http_requests_total'
curl -fsS localhost:18080/metrics | grep -q 'go_goroutines'
test "$(curl -s -o /dev/null -w '%{http_code}' localhost:18080/fail)" = "500"
test "$(curl -s localhost:18080/fail)" = "fail"
docker rm -f prom-lab-api-smoke
```

Expected: `/ok` 打印 `ok`，三个检查都成功，容器被删掉。若 `18080` 已被占用，停下来说明，不要改业务端口 `8080`。

- [ ] **Step 7: Commit**

```bash
git add api/go.mod api/go.sum api/handler_test.go api/main.go api/Dockerfile
git commit -m "$(cat <<'EOF'
feat: add an HTTP API that exposes RED metrics

EOF
)"
```

---

### Task 2: 打流量的 loadgen

**Files:**
- Create: `loadgen/decide.sh`
- Create: `loadgen/decide_test.sh`
- Create: `loadgen/loop.sh`
- Create: `loadgen/Dockerfile`
- Test: `loadgen/decide_test.sh`

**Interfaces:**
- Consumes: Task 1 的 `GET /ok` 与 `GET /fail`。
- Produces: `decide ROLL RATIO` 向标准输出打印 `ok` 或 `fail`；`valid_ratio RATIO` 合法时退出码 0。环境变量 `API_URL`（默认 `http://api:8080`）、`FAIL_RATIO`（默认 `0.1`）。镜像入口 `/opt/loop.sh`。

- [ ] **Step 1: 写会失败的测试**

创建 `loadgen/decide_test.sh`：

```sh
#!/bin/sh
set -eu
. "$(dirname "$0")/decide.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

[ "$(decide 0 0.1)" = "fail" ] || fail "roll 0 at 0.1"
[ "$(decide 99 0.1)" = "fail" ] || fail "roll 99 at 0.1"
[ "$(decide 100 0.1)" = "ok" ] || fail "roll 100 at 0.1"
[ "$(decide 999 0)" = "ok" ] || fail "ratio 0"
[ "$(decide 0 1)" = "fail" ] || fail "ratio 1 roll 0"
[ "$(decide 999 1)" = "fail" ] || fail "ratio 1 roll 999"
valid_ratio 0 || fail "0 should be valid"
valid_ratio 1 || fail "1 should be valid"
valid_ratio 0.1 || fail "0.1 should be valid"
valid_ratio 0.0 || fail "0.0 should be valid"
valid_ratio 1.0 || fail "1.0 should be valid"
if valid_ratio 1.1; then fail "1.1 should be invalid"; fi
if valid_ratio -0.1; then fail "negative should be invalid"; fi
if valid_ratio abc; then fail "abc should be invalid"; fi
if valid_ratio ""; then fail "empty should be invalid"; fi
echo OK
```

`chmod 755 loadgen/decide_test.sh`。

- [ ] **Step 2: 跑测试，确认失败**

Run: `sh loadgen/decide_test.sh`。

Expected: FAIL，因为 `decide.sh` 不存在，source 报错。

- [ ] **Step 3: 写 decide.sh 和 loop.sh**

创建 `loadgen/decide.sh`：

```sh
#!/bin/sh
# 整数截断。加上 1e-9，避免 0.1*1000 在浮点里变成 99.999999 后被截成 99。
threshold_for() {
  awk -v r="$1" 'BEGIN { printf "%d", int(r * 1000 + 1e-9) }'
}

decide() {
  roll=$1
  ratio=$2
  threshold=$(threshold_for "$ratio")
  if [ "$roll" -lt "$threshold" ]; then
    printf 'fail\n'
  else
    printf 'ok\n'
  fi
}

valid_ratio() {
  awk -v r="$1" 'BEGIN {
    if (r !~ /^[0-9]+(\.[0-9]+)?$/) exit 1
    n = r + 0
    if (n < 0 || n > 1) exit 1
    exit 0
  }'
}
```

创建 `loadgen/loop.sh`：

```sh
#!/bin/sh
set -eu
cd "$(dirname "$0")"
. ./decide.sh

API_URL="${API_URL:-http://api:8080}"
FAIL_RATIO="${FAIL_RATIO:-0.1}"

if ! valid_ratio "$FAIL_RATIO"; then
  echo "FAIL_RATIO must be a number from 0 to 1" >&2
  exit 1
fi

now_ms() {
  awk '{
    split($1, a, ".")
    frac = a[2] "000"
    printf "%d\n", a[1] * 1000 + substr(frac, 1, 3)
  }' /proc/uptime
}

while true; do
  start=$(now_ms)
  roll=$(od -An -N2 -tu2 /dev/urandom | tr -d ' ')
  roll=$((roll % 1000))
  choice=$(decide "$roll" "$FAIL_RATIO")
  wget -q -T 2 -O /dev/null "$API_URL/$choice" || true
  end=$(now_ms)
  spent=$((end - start))
  if [ "$spent" -ge 0 ] && [ "$spent" -lt 100 ]; then
    rest=$(awk -v s="$spent" 'BEGIN { printf "%.3f", (100 - s) / 1000 }')
    sleep "$rest"
  fi
done
```

`/fail` 让 wget 非零、连接失败也非零。`|| true` 让循环继续。随机数来自 `/dev/urandom`，避免每次新起 awk 时 `srand` 在同一秒内得到同一个数。

- [ ] **Step 4: 测试通过**

Run: `sh loadgen/decide_test.sh`。

Expected: 打印 `OK`，退出码 0。

- [ ] **Step 5: 写 Dockerfile 并确认非法比例会退出**

创建 `loadgen/Dockerfile`：

```dockerfile
FROM alpine:3.22
RUN apk add --no-cache wget
COPY decide.sh loop.sh /opt/
RUN chmod 755 /opt/decide.sh /opt/loop.sh
ENTRYPOINT ["/opt/loop.sh"]
```

```bash
docker build -t prom-lab-loadgen ./loadgen
docker run --rm -e FAIL_RATIO=2 prom-lab-loadgen
```

Expected: 容器退出码为 1，标准错误含 `FAIL_RATIO must be a number from 0 to 1`。不要用 `FAIL_RATIO=0.1` 跑这个检查，那会进入死循环。

- [ ] **Step 6: Commit**

```bash
git add loadgen/decide.sh loadgen/decide_test.sh loadgen/loop.sh loadgen/Dockerfile
git commit -m "$(cat <<'EOF'
feat: add a load generator with a configurable error ratio

EOF
)"
```

---

### Task 3: Prometheus 抓取和告警规则

**Files:**
- Create: `prometheus/prometheus.yml`
- Create: `prometheus/rules.yml`
- Test: `promtool check config` 与 `promtool check rules`

**Interfaces:**
- Consumes: Task 1 的 `api:8080/metrics`。Task 4 的 `alertmanager:9093` 可以后写；这份配置先按这个主机名写。
- Produces: 容器内路径 `/etc/prometheus/prometheus.yml` 与 `/etc/prometheus/rules.yml`。job 名 `api`。规则组 `api`，告警 `HighHttpErrorRate`。

- [ ] **Step 1: 写配置**

创建 `prometheus/prometheus.yml`：

```yaml
global:
  scrape_interval: 5s
  evaluation_interval: 15s

rule_files:
  - /etc/prometheus/rules.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets: ["alertmanager:9093"]

scrape_configs:
  - job_name: api
    static_configs:
      - targets: ["api:8080"]
```

创建 `prometheus/rules.yml`：

```yaml
groups:
  - name: api
    rules:
      - alert: HighHttpErrorRate
        expr: |
          sum(rate(http_requests_total{code=~"5.."}[1m]))
          /
          sum(rate(http_requests_total[1m]))
          > 0.05
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: HTTP 5xx 比例超过 5%
```

- [ ] **Step 2: 用 promtool 检查**

在 `prometheus_grafana` 目录：

```bash
docker run --rm --entrypoint promtool \
  -v "$PWD/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
  -v "$PWD/prometheus/rules.yml:/etc/prometheus/rules.yml:ro" \
  prom/prometheus:v2.55.1 \
  check config /etc/prometheus/prometheus.yml
docker run --rm --entrypoint promtool \
  -v "$PWD/prometheus/rules.yml:/etc/prometheus/rules.yml:ro" \
  prom/prometheus:v2.55.1 \
  check rules /etc/prometheus/rules.yml
```

Expected: 两条命令都退出码 0，输出里有 `SUCCESS`。

- [ ] **Step 3: Commit**

```bash
git add prometheus/prometheus.yml prometheus/rules.yml
git commit -m "$(cat <<'EOF'
feat: scrape the API and alert when the error ratio exceeds 5%

EOF
)"
```

---

### Task 4: Alertmanager

**Files:**
- Create: `alertmanager/alertmanager.yml`
- Test: `amtool check-config`

**Interfaces:**
- Consumes: Prometheus 发到 `alertmanager:9093` 的告警。
- Produces: 接收器名 `devnull`。UI 上 firing 对应 API `status.state == "active"`。

- [ ] **Step 1: 写配置**

创建 `alertmanager/alertmanager.yml`：

```yaml
route:
  receiver: devnull

receivers:
  - name: devnull
```

- [ ] **Step 2: 用 amtool 检查**

```bash
docker run --rm --entrypoint amtool \
  -v "$PWD/alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro" \
  prom/alertmanager:v0.34.1 \
  check-config /etc/alertmanager/alertmanager.yml
```

Expected: 退出码 0。可以有 “no integrations” 之类的提示，退出码必须是 0。

- [ ] **Step 3: Commit**

```bash
git add alertmanager/alertmanager.yml
git commit -m "$(cat <<'EOF'
feat: receive alerts on a page-only Alertmanager receiver

EOF
)"
```

---

### Task 5: Grafana 数据源和看板

**Files:**
- Create: `grafana/provisioning/datasources/datasource.yml`
- Create: `grafana/provisioning/dashboards/dashboards.yml`
- Create: `grafana/dashboards/http-red.json`
- Test: 内联 Python 检查 JSON

**Interfaces:**
- Consumes: Prometheus 查询，数据源 uid `prometheus`，URL `http://prometheus:9090`。
- Produces: 看板 uid `http-red`，标题 `HTTP RED`。八块面板的标题和表达式如下。

- [ ] **Step 1: 写 provisioning**

创建 `grafana/provisioning/datasources/datasource.yml`：

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    uid: prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
```

创建 `grafana/provisioning/dashboards/dashboards.yml`：

```yaml
apiVersion: 1

providers:
  - name: lab
    orgId: 1
    folder: ""
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
```

- [ ] **Step 2: 写看板 JSON**

创建 `grafana/dashboards/http-red.json`。八块面板，`refresh` 为 `5s`，`time.from` 为 `now-15m`，`time.to` 为 `now`。数据源都是 `{"type":"prometheus","uid":"prometheus"}`。

完整文件：

```json
{
  "uid": "http-red",
  "title": "HTTP RED",
  "timezone": "browser",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "5s",
  "time": { "from": "now-15m", "to": "now" },
  "tags": ["lab"],
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "links": [],
  "annotations": { "list": [] },
  "templating": { "list": [] },
  "panels": [
    {
      "id": 1,
      "type": "stat",
      "title": "QPS",
      "gridPos": { "x": 0, "y": 0, "w": 6, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "sum(rate(http_requests_total[1m]))", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "reqps" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false }, "colorMode": "value", "graphMode": "none", "justifyMode": "auto", "orientation": "auto", "textMode": "auto" }
    },
    {
      "id": 2,
      "type": "stat",
      "title": "错误率",
      "gridPos": { "x": 6, "y": 0, "w": 6, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "sum(rate(http_requests_total{code=~\"5..\"}[1m])) / sum(rate(http_requests_total[1m]))", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "percentunit" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false }, "colorMode": "value", "graphMode": "none", "justifyMode": "auto", "orientation": "auto", "textMode": "auto" }
    },
    {
      "id": 3,
      "type": "stat",
      "title": "p95 耗时",
      "gridPos": { "x": 12, "y": 0, "w": 6, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "s" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false }, "colorMode": "value", "graphMode": "none", "justifyMode": "auto", "orientation": "auto", "textMode": "auto" }
    },
    {
      "id": 4,
      "type": "stat",
      "title": "正在处理",
      "gridPos": { "x": 18, "y": 0, "w": 6, "h": 4 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "http_requests_in_flight", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "reduceOptions": { "calcs": ["lastNotNull"], "fields": "", "values": false }, "colorMode": "value", "graphMode": "none", "justifyMode": "auto", "orientation": "auto", "textMode": "auto" }
    },
    {
      "id": 5,
      "type": "timeseries",
      "title": "QPS 随时间",
      "gridPos": { "x": 0, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "sum(rate(http_requests_total[1m]))", "legendFormat": "QPS", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "reqps", "custom": { "drawStyle": "line", "lineWidth": 1, "fillOpacity": 10, "showPoints": "never", "spanNulls": false, "thresholdsStyle": { "mode": "off" } } }, "overrides": [] }
    },
    {
      "id": 6,
      "type": "timeseries",
      "title": "错误率",
      "gridPos": { "x": 12, "y": 4, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "sum(rate(http_requests_total{code=~\"5..\"}[1m])) / sum(rate(http_requests_total[1m]))", "legendFormat": "错误率", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": {
        "defaults": {
          "unit": "percentunit",
          "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": null }, { "color": "red", "value": 0.05 }] },
          "custom": { "drawStyle": "line", "lineWidth": 1, "fillOpacity": 10, "showPoints": "never", "spanNulls": false, "thresholdsStyle": { "mode": "line" } }
        },
        "overrides": []
      }
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "耗时 p50 与 p95",
      "gridPos": { "x": 0, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [
        { "refId": "A", "expr": "histogram_quantile(0.50, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))", "legendFormat": "p50", "datasource": { "type": "prometheus", "uid": "prometheus" } },
        { "refId": "B", "expr": "histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))", "legendFormat": "p95", "datasource": { "type": "prometheus", "uid": "prometheus" } }
      ],
      "fieldConfig": { "defaults": { "unit": "s", "custom": { "drawStyle": "line", "lineWidth": 1, "fillOpacity": 10, "showPoints": "never", "spanNulls": false, "thresholdsStyle": { "mode": "off" } } }, "overrides": [] }
    },
    {
      "id": 8,
      "type": "timeseries",
      "title": "按状态码的请求速率",
      "gridPos": { "x": 12, "y": 12, "w": 12, "h": 8 },
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "targets": [{ "refId": "A", "expr": "sum by (code) (rate(http_requests_total[1m]))", "legendFormat": "{{code}}", "datasource": { "type": "prometheus", "uid": "prometheus" } }],
      "fieldConfig": { "defaults": { "unit": "reqps", "custom": { "drawStyle": "line", "lineWidth": 1, "fillOpacity": 10, "showPoints": "never", "spanNulls": false, "thresholdsStyle": { "mode": "off" } } }, "overrides": [] }
    }
  ]
}
```

- [ ] **Step 3: 检查 JSON**

```bash
python3 - <<'PY'
import json
p = "grafana/dashboards/http-red.json"
d = json.load(open(p))
assert d["uid"] == "http-red"
assert d["title"] == "HTTP RED"
assert d["refresh"] == "5s"
assert d["time"] == {"from": "now-15m", "to": "now"}
assert len(d["panels"]) == 8
titles = [panel["title"] for panel in d["panels"]]
assert titles == ["QPS", "错误率", "p95 耗时", "正在处理", "QPS 随时间", "错误率", "耗时 p50 与 p95", "按状态码的请求速率"]
exprs = []
for panel in d["panels"]:
    for t in panel["targets"]:
        exprs.append(t["expr"])
assert "sum(rate(http_requests_total[1m]))" in exprs
assert 'sum(rate(http_requests_total{code=~"5.."}[1m])) / sum(rate(http_requests_total[1m]))' in exprs
assert "histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))" in exprs
assert "histogram_quantile(0.50, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))" in exprs
assert "http_requests_in_flight" in exprs
assert "sum by (code) (rate(http_requests_total[1m]))" in exprs
assert d["panels"][1]["fieldConfig"]["defaults"]["unit"] == "percentunit"
err = d["panels"][5]
assert err["fieldConfig"]["defaults"]["unit"] == "percentunit"
assert err["fieldConfig"]["defaults"]["custom"]["thresholdsStyle"]["mode"] == "line"
assert err["fieldConfig"]["defaults"]["thresholds"]["steps"][1]["value"] == 0.05
assert [t["legendFormat"] for t in d["panels"][6]["targets"]] == ["p50", "p95"]
print("OK")
PY
```

Expected: 打印 `OK`。

- [ ] **Step 4: Commit**

```bash
git add grafana/provisioning/datasources/datasource.yml grafana/provisioning/dashboards/dashboards.yml grafana/dashboards/http-red.json
git commit -m "$(cat <<'EOF'
feat: provision the HTTP RED Grafana dashboard

EOF
)"
```

---

### Task 6: Compose 把五个服务接起来

**Files:**
- Create: `docker-compose.yml`
- Test: `docker compose config`

**Interfaces:**
- Consumes: Task 1–5 的镜像构建上下文和配置路径。
- Produces: 服务名 `api`、`loadgen`、`prometheus`、`alertmanager`、`grafana`。`FAIL_RATIO: ${FAIL_RATIO:-0.1}`。只读挂载与 spec 一致。没有命名 volume。

- [ ] **Step 1: 写 compose 文件**

创建 `docker-compose.yml`：

```yaml
services:
  api:
    build: ./api
    ports:
      - "8080:8080"

  loadgen:
    build: ./loadgen
    environment:
      API_URL: http://api:8080
      FAIL_RATIO: ${FAIL_RATIO:-0.1}
    depends_on:
      - api

  prometheus:
    image: prom/prometheus:v2.55.1
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./prometheus/rules.yml:/etc/prometheus/rules.yml:ro
    depends_on:
      - api
      - alertmanager

  alertmanager:
    image: prom/alertmanager:v0.34.1
    ports:
      - "9093:9093"
    volumes:
      - ./alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro

  grafana:
    image: grafana/grafana:13.2.4
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: admin
      GF_AUTH_ANONYMOUS_ENABLED: "false"
      GF_USERS_ALLOW_SIGN_UP: "false"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
    depends_on:
      - prometheus
```

这些 volume 是配置挂载，不是数据卷。不要加 `prometheus-data` 或 Grafana sqlite 的命名卷。

- [ ] **Step 2: 检查 compose 能解析**

```bash
docker compose config >/dev/null
```

Expected: 退出码 0。

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "$(cat <<'EOF'
feat: run the lab with Docker Compose

EOF
)"
```

---

### Task 7: 中文 README

**Files:**
- Modify: `README.md`
- Test: `grep` 六段标题和恢复命令

**Interfaces:**
- Consumes: 前面各任务的端口、查询、告警名、恢复命令。
- Produces: 读者按文件顺序能启动、看四个地址、看懂查询、让告警恢复、再关掉。

- [ ] **Step 1: 替换 README**

用下面的全文替换 `README.md`：

```markdown
# Prometheus + Grafana 教学案例

一个 Go 服务产生 HTTP 指标。Prometheus 抓取并计算告警，Grafana 画图，Alertmanager 在自己的页面上显示告警。

## 五个容器各管什么

| 容器 | 作用 |
| --- | --- |
| api | Go 服务。`/ok` 返回 200，`/fail` 返回 500，`/metrics` 给出指标原文 |
| loadgen | 大约每秒 10 个请求。默认约 10% 打到 `/fail` |
| prometheus | 每 5 秒抓取，每 15 秒计算告警 |
| grafana | 打开就有看板 HTTP RED |
| alertmanager | 告警出现在页面上，不发邮件或 Slack |

指标有三种：

- **counter** `http_requests_total` 只增不减。标签是 `method`、`path`、`code`。看「每秒几个」要用 `rate(...[1m])`，这是最近 1 分钟的平均每秒增量。累计值会一直涨，看不出现在快还是慢。
- **histogram** `http_request_duration_seconds` 记录每次请求的耗时。`histogram_quantile(0.95, ...)` 得到 p95。
- **gauge** `http_requests_in_flight` 可升可降，表示这一刻还没处理完的请求。按这个案例的速率和耗时，这个数通常是 0 或 1。

## 启动

需要本机已安装 Docker Compose。

```bash
docker compose up --build
```

前台运行，方便看日志。要放后台就加 `-d`。

启动后的第一分钟，`rate(...[1m])` 还没有完整窗口，看板可能暂时没有线。告警大约 2 分钟后才变为 firing。

## 四个地址

- http://localhost:8080/metrics — 指标原文，Prometheus 抓的就是这一页
- http://localhost:9090 — 自己跑查询，Targets 页里 job `api` 应为 UP
- http://localhost:3000 — 看板，登录 `admin` / `admin`，打开 HTTP RED
- http://localhost:9093 — 告警列表

## 看板上的查询

- QPS：`sum(rate(http_requests_total[1m]))`
- 错误率：`sum(rate(http_requests_total{code=~"5.."}[1m])) / sum(rate(http_requests_total[1m]))`。数值 0.1 在看板上显示为 10%
- p95：`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))`
- 正在处理：`http_requests_in_flight`
- 按状态码：`sum by (code) (rate(http_requests_total[1m]))`

错误率图上有一条 5% 的线。默认流量在这条线上面。

## 告警为什么会响，以及怎么让它恢复

规则名 `HighHttpErrorRate`：最近 1 分钟的 5xx 比例大于 5%，并且连续成立 1 分钟。默认 `FAIL_RATIO=0.1`，所以大约 2 分钟后 Alertmanager 上是 firing。

把失败比例降到 0，只重建 loadgen：

```bash
FAIL_RATIO=0 docker compose up -d --force-recreate --no-deps loadgen
```

1 分钟窗口里的旧错误会逐渐出去。大约 1 分钟后告警离开 firing，验收放宽到 2 分钟。

api 停掉之后，Prometheus Targets 变成 DOWN。大约 5 分钟后样本被标成陈旧，告警也会离开 firing。

## 关掉并清掉这次数据

```bash
docker compose down
```

没有数据卷。下次 `up` 从空的 Prometheus 和 Grafana 重新开始。
```

- [ ] **Step 2: 检查 README 含指定的六段和命令**

```bash
grep -q '五个容器各管什么' README.md
grep -q 'docker compose up --build' README.md
grep -q '四个地址' README.md
grep -q 'rate(...\[1m\])' README.md
grep -q 'FAIL_RATIO=0 docker compose up -d --force-recreate --no-deps loadgen' README.md
grep -q 'docker compose down' README.md
grep -qx '.superpowers/' .gitignore
```

Expected: 全部退出码 0。

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: explain how to run the Prometheus and Grafana lab

EOF
)"
```

---

### Task 8: 按 spec 做端到端验收

**Files:**
- Test: 本机 Compose 栈。没有新的源码文件。不要做空提交。

**Interfaces:**
- Consumes: Task 1–7 的全部产物。
- Produces: spec「验收」一节的 7 条都有记录。栈留着运行，把四个地址告诉用户。

- [ ] **Step 1: 启动**

在 `prometheus_grafana` 目录。若 8080、9090、3000、9093 已被占用，停下来说明，不要改 spec 里的端口。

```bash
docker compose up --build -d
docker compose ps
```

Expected: `api`、`loadgen`、`prometheus`、`alertmanager`、`grafana` 都是 Up。

- [ ] **Step 2: 指标原文**

```bash
curl -fsS localhost:8080/metrics | grep -q http_requests_total
```

Expected: 退出码 0。

- [ ] **Step 3: 等 15 秒后检查 target、看板和查询**

若 15 秒时 `rate` 还没有点，再等 15 秒重跑同一次检查，最多等到启动后 60 秒。60 秒仍没有序列就停下来看 Prometheus targets 和 api 日志。

```bash
sleep 15
python3 - <<'PY'
import base64, json, urllib.parse, urllib.request
targets = json.load(urllib.request.urlopen("http://localhost:9090/api/v1/targets"))
health = {t["labels"].get("job"): t["health"] for t in targets["data"]["activeTargets"]}
assert health.get("api") == "up", health

def query(expr):
    url = "http://localhost:9090/api/v1/query?" + urllib.parse.urlencode({"query": expr})
    body = json.load(urllib.request.urlopen(url))
    result = body["data"]["result"]
    assert result, expr

query("sum(rate(http_requests_total[1m]))")
query("sum by (code) (rate(http_requests_total[1m]))")
token = base64.b64encode(b"admin:admin").decode()
req = urllib.request.Request(
    "http://localhost:3000/api/dashboards/uid/http-red",
    headers={"Authorization": "Basic " + token},
)
dash = json.load(urllib.request.urlopen(req))
assert dash["dashboard"]["uid"] == "http-red"
print("OK")
PY
```

Expected: 打印 `OK`。

- [ ] **Step 4: 等到告警 firing**

Alertmanager 的 `/api/v2/alerts` 里，页面上的 Firing 对应 `status.state == "active"`。同时看 Prometheus 的 `state == "firing"`。从 `docker compose up` 起最多等 150 秒：

```bash
python3 - <<'PY'
import json, time, urllib.request
deadline = time.time() + 150
while time.time() < deadline:
    prom = json.load(urllib.request.urlopen("http://localhost:9090/api/v1/alerts"))
    firing = [a for a in prom["data"]["alerts"] if a["labels"].get("alertname") == "HighHttpErrorRate" and a["state"] == "firing"]
    am = json.load(urllib.request.urlopen("http://localhost:9093/api/v2/alerts"))
    active = [a for a in am if a["labels"].get("alertname") == "HighHttpErrorRate" and a["status"]["state"] == "active"]
    if firing and active:
        print("firing")
        break
    time.sleep(5)
else:
    raise SystemExit("alert did not fire within 150s")
PY
```

Expected: 打印 `firing`。

- [ ] **Step 5: 把 FAIL_RATIO 设为 0，确认告警离开 firing**

```bash
FAIL_RATIO=0 docker compose up -d --force-recreate --no-deps loadgen
python3 - <<'PY'
import json, time, urllib.request
deadline = time.time() + 120
while time.time() < deadline:
    prom = json.load(urllib.request.urlopen("http://localhost:9090/api/v1/alerts"))
    firing = [a for a in prom["data"]["alerts"] if a["labels"].get("alertname") == "HighHttpErrorRate" and a["state"] == "firing"]
    am = json.load(urllib.request.urlopen("http://localhost:9093/api/v2/alerts"))
    active = [a for a in am if a["labels"].get("alertname") == "HighHttpErrorRate" and a["status"]["state"] == "active"]
    if not firing and not active:
        print("resolved")
        break
    time.sleep(5)
else:
    raise SystemExit("alert still firing after 120s")
PY
```

Expected: 打印 `resolved`。

- [ ] **Step 6: 在浏览器打开看板**

打开 http://localhost:3000 。用 `admin` / `admin` 登录。进入 http://localhost:3000/d/http-red/http-red 。

确认八块面板都有数据：QPS、错误率、p95 耗时、正在处理、QPS 随时间、错误率、耗时 p50 与 p95、按状态码的请求速率。错误率时间序列上能看见 5% 阈值线。

这一步在 `FAIL_RATIO=0` 之后做也可以。错误率会下降，阈值线仍然要在。

- [ ] **Step 7: 留下运行中的栈**

不要 `docker compose down`。告诉用户四个地址，以及恢复命令已经执行过，当前 loadgen 的失败比例是 0。用户若要再看 firing，把 `FAIL_RATIO` 设回 `0.1` 并重建 loadgen。

没有源码变更。不要创建空提交。
