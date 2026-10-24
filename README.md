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
