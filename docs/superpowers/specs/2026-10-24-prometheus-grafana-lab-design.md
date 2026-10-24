# Prometheus + Grafana 教学案例设计

日期：2026-10-24

这是一份能在本机用 `docker compose` 跑起来的教学案例。读者没用过 Prometheus 和 Grafana。案例用一个很小的 Go HTTP 服务产生请求量、错误率、耗时，让读者看见指标原文、抓取、查询、看板和一条告警是怎么串起来的。

## 已定的选择

- 最小可运行教学例，不接入现有区块链服务。
- 故事是通用 HTTP：请求量、错误率、耗时。
- 服务用 Go 写。
- 包含 Alertmanager 和一条错误率告警。告警只在 Alertmanager 自己的页面上显示。
- 中文 README 按操作顺序讲解。
- 不使用数据卷。`docker compose down` 之后这次数据清空，可以重来。

## 架构

五个容器在同一个 Compose 网络里，用服务名互相访问。

1. `loadgen` 大约每 100ms 完成一轮请求。约 90% 打 `GET /ok`，约 10% 打 `GET /fail`。比例来自环境变量 `FAIL_RATIO`，默认 `0.1`。
2. `api` 监听 `8080`。`/ok` 返回 200，`/fail` 返回 500，`/metrics` 返回 Prometheus 文本。
3. `prometheus` 每 5 秒抓取 `http://api:8080/metrics`，每 15 秒计算告警规则，把告警发给 `alertmanager:9093`。
4. `grafana` 用配好的数据源查询 Prometheus，打开就有看板 `HTTP RED`。
5. `alertmanager` 使用名为 `devnull` 的接收器。这个接收器不配置 webhook、邮件或 Slack。告警出现在 `localhost:9093`。

发布到本机的端口：

| 服务 | 地址 | 用来看什么 |
| --- | --- | --- |
| api | http://localhost:8080/metrics | 指标原文 |
| prometheus | http://localhost:9090 | 查询和 Targets |
| grafana | http://localhost:3000 | 看板，登录 `admin` / `admin` |
| alertmanager | http://localhost:9093 | 告警是否 firing |

## api

模块路径：`example.com/prom-lab/api`。

路由：

- `GET /ok`：随机睡眠 20–80ms（含两端），正文 `ok`，状态码 200。
- `GET /fail`：睡眠 5ms，正文 `fail`，状态码 500。
- `GET /metrics`：`prometheus/client_golang` 的默认 handler。
- 其他路径：状态码 404，正文 `not found`。这些请求也计入指标，path 标签写 `other`。loadgen 不打这些路径。

监听 `0.0.0.0:8080`。镜像是多阶段构建：用官方 Go Alpine 镜像编译，运行镜像里只有这个二进制，以非 root 用户运行。

所有业务请求（不含 `/metrics`）经过同一层记录：

- 进入时 `http_requests_in_flight` 加 1，结束时减 1。
- 结束时 `http_requests_total` 加 1。
- 结束时把耗时（秒）记入 `http_request_duration_seconds`。

`/metrics` 使用默认 registerer，所以页面上还会有 `go_*` 和 `process_*`。看板只用下面三个指标。

### 指标

`http_requests_total`，counter。标签：

- `method`：请求方法，loadgen 只会是 `GET`
- `path`：`/ok`、`/fail` 或 `other`
- `code`：十进制状态码字符串，例如 `200`、`500`、`404`

`http_request_duration_seconds`，histogram。标签只有 `method` 和 `path`。桶边界（秒）：`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1`。

`http_requests_in_flight`，gauge，无标签。

约 10 QPS、单次耗时大约 50ms 时，正在处理的请求通常是 0 或 1。看板上的这个数用来认识 gauge，不要求它停在 1 以上。

## loadgen

`loadgen/loop.sh` 在 Alpine 里用 `wget` 打请求。

- `API_URL` 默认 `http://api:8080`。
- `FAIL_RATIO` 必须是 0 到 1 的小数（含两端）。其他值时脚本直接退出，容器会停，避免悄悄打错比例。
- 每一轮：取 `0–999` 的随机数，小于 `FAIL_RATIO * 1000` 的整数截断就打 `/fail`，否则打 `/ok`。`0.1` 因此是 100，大约 10% 的请求打到 `/fail`。
- `wget` 超时 2 秒，把响应丢掉。`/fail` 的 500 会让 `wget` 返回非零，脚本忽略这一次的退出码，循环继续。
- 一轮的目标周期是 100ms。请求花掉的时间从这 100ms 里扣掉；请求超过 100ms 时立刻开始下一轮。这样在 `/ok` 睡几十毫秒时，速率仍大约是每秒 10 个。

Compose 把变量写成 `FAIL_RATIO: ${FAIL_RATIO:-0.1}`，所以宿主环境可以覆盖它。

## Prometheus

`prometheus/prometheus.yml`：

- `scrape_interval: 5s`
- `evaluation_interval: 15s`
- `rule_files` 指向容器内的 `rules.yml`
- `alerting.alertmanagers` 的 target 是 `alertmanager:9093`
- 一个 scrape job，`job_name: api`，target `api:8080`，路径用默认的 `/metrics`

镜像使用官方 `prom/prometheus` 的不可变版本标签，不用 `latest`。实现时把选中的完整标签写进 `docker-compose.yml`。配置保持 Prometheus 2.x 这份写法。

挂载：

- `./prometheus/prometheus.yml` → `/etc/prometheus/prometheus.yml`
- `./prometheus/rules.yml` → `/etc/prometheus/rules.yml`

### 告警规则

组名 `api`，规则名 `HighHttpErrorRate`。`for: 1m`。标签 `severity: warning`。annotation `summary` 写明 HTTP 5xx 比例超过 5%。

表达式：

```promql
sum(rate(http_requests_total{code=~"5.."}[1m]))
/
sum(rate(http_requests_total[1m]))
> 0.05
```

没有请求时除数是 0，结果为空，比较不成立，告警保持安静。loadgen 在跑时不会碰到这种情况。

默认 `FAIL_RATIO=0.1` 时，抓取攒满约 1 分钟后条件成立，再持续 1 分钟变为 firing。从 `docker compose up` 到 Alertmanager 上看到 firing，大约 2 分钟。

把 `FAIL_RATIO` 改成 0 并只重建 loadgen 之后，1 分钟窗口里的 5xx 会掉下去。条件变为假之后，下一次计算（15 秒内）告警离开 firing。这一步大约 1 分钟，验收放宽到 2 分钟内。

## Alertmanager

`alertmanager/alertmanager.yml` 只有一条 route，receiver 名为 `devnull`。receivers 里这个名字存在，下面不写 webhook、email 或 slack。镜像使用官方 `prom/alertmanager` 的不可变版本标签，不用 `latest`，完整标签写进 `docker-compose.yml`。挂载 `./alertmanager/alertmanager.yml` → `/etc/alertmanager/alertmanager.yml`。

## Grafana

登录用户 `admin`，密码 `admin`。匿名访问关闭。

Provisioning：

- 数据源名 `Prometheus`，类型 `prometheus`，access `proxy`，url `http://prometheus:9090`，默认数据源，不可在界面里改。
- dashboard provider 读取 `/var/lib/grafana/dashboards`。
- 文件 `grafana/dashboards/http-red.json`，uid `http-red`，标题 `HTTP RED`。

看板时间范围默认最近 15 分钟，刷新 5 秒。八块面板，查询窗口都是 `[1m]`：

1. Stat，QPS：`sum(rate(http_requests_total[1m]))`
2. Stat，错误率：`sum(rate(http_requests_total{code=~"5.."}[1m])) / sum(rate(http_requests_total[1m]))`。面板单位用 Grafana 的 `percentunit`（数值 0.1 显示成 10%）
3. Stat，p95：`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))`
4. Stat，正在处理：`http_requests_in_flight`
5. 时间序列，QPS，查询同 1
6. 时间序列，错误率，查询同 2，单位同样是 `percentunit`，阈值线画在 `0.05`（显示为 5%）
7. 时间序列，p50 与 p95。p50 是同一条 `histogram_quantile`，分位数换成 `0.50`
8. 时间序列，按状态码：`sum by (code) (rate(http_requests_total[1m]))`

镜像使用官方 `grafana/grafana` 的不可变版本标签，不用 `latest`，完整标签写进 `docker-compose.yml`。Provisioning 用 Grafana 10 及以上的 `apiVersion: 1` 写法。

挂载：

- `./grafana/provisioning` → `/etc/grafana/provisioning`
- `./grafana/dashboards` → `/var/lib/grafana/dashboards`

## 文件

```
docker-compose.yml
README.md
.gitignore

api/main.go
api/go.mod
api/Dockerfile

loadgen/loop.sh
loadgen/Dockerfile

prometheus/prometheus.yml
prometheus/rules.yml

alertmanager/alertmanager.yml

grafana/provisioning/datasources/datasource.yml
grafana/provisioning/dashboards/dashboards.yml
grafana/dashboards/http-red.json
```

现有的 `README.md` 只有一行标题，实现时整篇换成下面的中文说明。`.gitignore` 继续忽略 `.superpowers/`。

README 六段，按这个顺序：

1. 五个容器各管什么，以及 counter、histogram、gauge 的差别。
2. `docker compose up --build`。
3. 四个地址分别看什么。
4. 看板上每条查询在说什么，以及为什么用 `rate` 而不是累计值。
5. 为什么默认会 firing，以及这条命令让它恢复：`FAIL_RATIO=0 docker compose up -d --force-recreate --no-deps loadgen`。
6. `docker compose down` 清掉这次数据。

## 失败时的表现

- `/fail` 的 500 计入 `http_requests_total`。这是案例要演示的错误率。
- api 进程退出后，Prometheus Targets 里该 target 为 DOWN。看板不再有新点。Prometheus 默认大约 5 分钟把过期样本标成陈旧，之后 `HighHttpErrorRate` 离开 firing。
- 启动后的第一分钟，`rate(...[1m])` 还没有完整窗口，看板可能暂时没有线。README 写明等大约 2 分钟再看告警。

## 验收

实现完成后，在本机执行并通过这些检查，才算这版案例完成：

1. `docker compose up --build` 后，`api`、`loadgen`、`prometheus`、`alertmanager`、`grafana` 都是 Up。
2. `curl -fsS localhost:8080/metrics` 的正文包含 `http_requests_total`。
3. `http://localhost:9090/api/v1/targets` 里 job `api` 的 health 是 `up`。
4. 启动 15 秒后，用 `admin` / `admin` 访问 Grafana，uid `http-red` 的看板存在。对 Prometheus 查询第 1 条和第 8 条表达式，都能返回序列。
5. 启动约 2 分钟后，Alertmanager 的告警列表里 `HighHttpErrorRate` 为 firing。
6. 执行 README 里的恢复命令后，2 分钟内该告警不再是 firing。
7. 在浏览器打开 Grafana 看板，确认八块面板都有数据，错误率面板能看见 5% 阈值线。

## 不做的事

- 不把告警送到邮件、Slack 或其他外部系统。
- 不加 TLS、反向代理、API 认证，也不给 Prometheus 和 Alertmanager 加登录。
- 不挂数据卷，不写 recording rules。
- 不监控宿主机、现有扫链服务或链节点。
- 不在 Grafana 里再配一套告警。告警只有 Prometheus 规则这一条。
