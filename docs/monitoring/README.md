# new-api Prometheus 监控落地说明

## 1. 目标

这套监控分为两层：

- 应用内：`new-api` 暴露 Prometheus `/metrics`，输出 Go runtime、系统资源、HTTP 请求、渠道级转发指标。
- 应用外：Prometheus 负责抓取与计算，Alertmanager 负责报警，Grafana 负责按时间范围展示。

## 2. 应用侧开关

在 `new-api` 进程环境变量中增加：

```bash
PROMETHEUS_ENABLED=true
PROMETHEUS_PATH=/metrics
PROMETHEUS_BEARER_TOKEN=replace-with-a-long-random-token
```

说明：

- `PROMETHEUS_ENABLED`: 开启 Prometheus 指标暴露。
- `PROMETHEUS_PATH`: 指标路径，默认 `/metrics`。
- `PROMETHEUS_BEARER_TOKEN`: 可选。设置后，Prometheus 抓取必须带 `Authorization: Bearer <token>`。

多站点说明：

- 多站点模式下，`new-api` 本身不需要新增额外环境变量，每个站点仍然只需要上面这 3 个 Prometheus 相关变量。
- 站点维度统一由 Prometheus `scrape_configs[].static_configs[].labels.site` 注入，而不是由 `new-api` 进程自己上报。
- 如果不同站点使用不同的 `PROMETHEUS_BEARER_TOKEN`，建议 Prometheus 按“每个站点一个 job”配置，便于分别带各自的 Bearer Token。
- Grafana 仪表盘里的 `站点`、`渠道`、`模型` 变量已经改成 `query_result + regex` 取值方案，PromQL 引用时统一使用 `$site`、`$channel`、`$model`，交给 Prometheus 数据源做多选值转义，避免 IP:端口这类站点值触发非法转义。

## 2.1 飞书适配层开关

为了避免 Alertmanager 直接把大量渠道告警原样打到飞书，监控目录新增了一个独立的飞书适配服务 `feishu-alert-adapter`。

这个适配层的环境变量写在 `docs/monitoring/docker-compose.monitoring.yml` 里，而不是写进 `new-api` 本体的 Docker Compose：

```bash
ALERTMANAGER_FEISHU_ADAPTER_LISTEN=:9098
ALERTMANAGER_FEISHU_ADAPTER_PATH=/alertmanager/feishu
ALERTMANAGER_FEISHU_ADAPTER_BEARER_TOKEN=replace-with-a-long-random-token
ALERTMANAGER_FEISHU_WEBHOOK_URL=https://open.feishu.cn/open-apis/bot/v2/hook/xxx
ALERTMANAGER_FEISHU_MESSAGE_PREFIX=[new-api 监控告警]
ALERTMANAGER_FEISHU_MIN_INTERVAL_SECONDS=120
ALERTMANAGER_FEISHU_MAX_ALERTS_PER_MESSAGE=10
ALERTMANAGER_FEISHU_REQUEST_TIMEOUT_SECONDS=10
```

说明：

- `ALERTMANAGER_FEISHU_ADAPTER_BEARER_TOKEN`: Alertmanager 调用适配层时使用的 Bearer Token，避免被外部随便 POST。
- `ALERTMANAGER_FEISHU_ADAPTER_BEARER_TOKEN` 需要和 `alertmanager.yml` 里的 `credentials` 保持完全一致。
- `ALERTMANAGER_FEISHU_WEBHOOK_URL`: 飞书机器人 Webhook 地址。
- `ALERTMANAGER_FEISHU_MIN_INTERVAL_SECONDS`: 同一个告警分组的最小发送间隔，默认 120 秒，用来进一步兜底防止飞书被打爆。
- `ALERTMANAGER_FEISHU_MAX_ALERTS_PER_MESSAGE`: 单条飞书消息里最多展开多少条 Alert，超出的只保留汇总。

## 3. 指标清单

### 系统级

- `go_gc_duration_seconds`
- `go_goroutines`
- `go_memstats_*`
- `process_cpu_seconds_total`
- `process_resident_memory_bytes`
- `newapi_system_cpu_usage_percent`
- `newapi_system_memory_usage_percent`
- `newapi_system_disk_usage_percent`
- `newapi_system_monitor_enabled`
- `newapi_system_threshold_percent{resource="cpu|memory|disk"}`
- `newapi_system_performance_rejections_total{reason="cpu|memory|disk"}`

### HTTP 级

- `newapi_http_inflight_requests{route_tag}`
- `newapi_http_requests_total{route_tag,method,status_class}`
- `newapi_http_request_duration_seconds_bucket{route_tag,method}`

### 渠道级

- `newapi_channel_inflight_requests{channel_id,channel_name,channel_type,request_kind}`
- `newapi_channel_requests_total{channel_id,channel_name,channel_type,request_kind,result}`
- `newapi_channel_requests_last_1m{channel_id,channel_name,channel_type,request_kind}`
- `newapi_channel_success_rpm_last_1m{channel_id,channel_name,channel_type,request_kind}`
- `newapi_channel_errors_total{channel_id,channel_name,channel_type,request_kind,error_code,status_code}`
- `newapi_channel_model_requests_total{channel_id,channel_name,channel_type,request_kind,model_name,result}`
- `newapi_channel_last_error_timestamp_seconds{channel_id,channel_name,channel_type,request_kind,model_name,error_type,error_code,status_code,error_message}`
- `newapi_channel_attempt_duration_seconds_bucket{channel_id,channel_name,channel_type,request_kind,result}`
- `newapi_channel_first_response_duration_seconds_bucket{channel_id,channel_name,channel_type,request_kind}`
- `newapi_channel_upstream_latency_seconds_bucket{channel_id,channel_name,channel_type,request_kind}`

## 4. 与需求的对应关系

### 4.1 系统 GC 与卡顿报警

- 看 `go_gc_duration_seconds`、`go_goroutines`、`newapi_channel_attempt_duration_seconds_bucket`、`newapi_http_inflight_requests`
- 告警规则见 `prometheus-rules.yml` 中：
  - `NewAPIHighGCPauseP99`
  - `NewAPIChannelAvgTotalDurationTooHigh`
  - `NewAPIHTTPInflightTooHigh`
  - `NewAPISystemPerformanceRejected`

### 4.2 每个渠道延迟

- 5 分钟平均总用时：`sum by (site, channel_id, channel_name) (rate(newapi_channel_attempt_duration_seconds_sum{site=~"$site", channel_name=~"$channel", result="success"}[5m])) / sum by (site, channel_id, channel_name) (rate(newapi_channel_attempt_duration_seconds_count{site=~"$site", channel_name=~"$channel", result="success"}[5m]))`
- 5 分钟平均首字用时：`sum by (site, channel_id, channel_name) (rate(newapi_channel_first_response_duration_seconds_sum{site=~"$site", channel_name=~"$channel"}[5m])) / sum by (site, channel_id, channel_name) (rate(newapi_channel_first_response_duration_seconds_count{site=~"$site", channel_name=~"$channel"}[5m]))`
- 看板展示：
  - `Selected Channels Avg Total Duration (5m)`
  - `Selected Channels Avg First Token Duration (5m)`
- 其中首字平均用时只对最近 5 分钟内真正产生过首包的流式请求有值。
- 告警：
  - `NewAPIChannelAvgTotalDurationTooHigh`：5 分钟平均总用时超过 `40s`
  - `NewAPIChannelAvgFirstResponseDurationTooHigh`：5 分钟平均首字用时超过 `10s`

### 4.3 每个渠道调用情况、队列、RPM

- Dashboard 里的 `站点`、`渠道`、`模型` 变量都支持多选，也支持 `All`。其中 `渠道`、`模型` 会跟随当前选择的 `站点` 自动联动筛选。
- 多站点下所有渠道面板都会按 `site + channel` 组合分别展示，不会把不同站点里同名或同 ID 的渠道合并到一起。
- 当前真实流量折线：`sum by (site, channel_id, channel_name) (increase(newapi_channel_requests_total{site=~"$site", channel_name=~"$channel"}[$__rate_interval]))`
- 队列原始指标：`newapi_channel_inflight_requests`
- RPM：`sum by (site, channel_id, channel_name) (newapi_channel_success_rpm_last_1m{site=~"$site", channel_name=~"$channel"})`
- `Selected Channels Request Traffic` 面板基于 `newapi_channel_requests_total` 计数器，按 Grafana 当前选择的时间范围自动分桶统计真实请求数，包含成功和失败请求，不再固定看最近 60 秒。
- `Selected Channels RPM (Last 1m)` 面板按 `站点 / 渠道` 画折线，展示应用内精确维护的“最近 60 秒成功请求数”，不依赖 Prometheus `increase` 外推。

### 4.4 每个渠道报错情况

- 用 `newapi_channel_errors_total`
- `Selected Channels Errors` 面板按 `sum by (site, channel_id, channel_name) (increase(newapi_channel_errors_total{site=~"$site", channel_name=~"$channel"}[$__rate_interval]))` 画折线，选中多个站点或多个渠道时每个 `站点 / 渠道` 各自单独出线。
- 最近错误明细表：`newapi_channel_last_error_timestamp_seconds`
- `Selected Channels Latest Error Details` 会展示最近一次错误对应的站点、模型、渠道、状态码和错误详情，并用 `${__from}` 过滤掉当前时间范围之前的旧错误。
- 错误详情会先做敏感信息脱敏，并把换行压成单行；为了控制标签基数，超长消息会截断到 240 个字符。
- 告警：`NewAPIChannelHasErrors`
- 这一条规则已经从“最近 1 分钟只要有 1 次错误就报警”放宽成“最近 5 分钟至少 3 次错误，并且总请求数至少 10 次才报警”，目的是先压住告警风暴，把飞书通道稳定下来。

### 4.5 每个渠道成功率

- 成功率公式：

```promql
100 *
sum by (site, channel_id, channel_name) (
  increase(newapi_channel_requests_total{site=~"$site", result="success", channel_name=~"$channel"}[$__rate_interval])
)
/
clamp_min(
  sum by (site, channel_id, channel_name) (
    increase(newapi_channel_requests_total{site=~"$site", channel_name=~"$channel"}[$__rate_interval])
  ),
  1
)
```

- `Selected Channels Success Rate` 面板按上面的公式画折线，选中多个站点或多个渠道时每个 `站点 / 渠道` 各自单独出线。

### 4.6 每个渠道调用模型分布与模型成功率

- Dashboard 新增 `Model` 多选变量，并且会跟随当前 `站点 + 渠道` 选择动态列出模型。
- 模型分布折线：`sum by (site, channel_id, channel_name, model_name) (increase(newapi_channel_model_requests_total{site=~"$site", channel_name=~"$channel", model_name=~"$model"}[$__rate_interval]))`
- 模型成功率折线：

```promql
100 *
sum by (site, channel_id, channel_name, model_name) (
  increase(newapi_channel_model_requests_total{site=~"$site", result="success", channel_name=~"$channel", model_name=~"$model"}[$__rate_interval])
)
/
clamp_min(
  sum by (site, channel_id, channel_name, model_name) (
    increase(newapi_channel_model_requests_total{site=~"$site", channel_name=~"$channel", model_name=~"$model"}[$__rate_interval])
  ),
  1
)
```

- `Selected Channels Model Traffic Distribution` 和 `Selected Channels Model Success Rate` 都是渠道级折线图，图例格式为 `站点 / 渠道 / 模型`。

## 5. 时区说明

- Prometheus 内部时间戳仍然是 UTC，这属于正常存储方式。
- Grafana Dashboard 已强制设置 `timezone = Asia/Shanghai`。
- 面板查询统一使用 `$__range`、`$__range_s`、`$__rate_interval`，所以用户切换任意时间范围时，图表和统计都会按该范围重新计算。

## 6. 文件说明

- `prometheus.yml`: 抓取与规则加载配置，示例已经改成多站点写法，要求给每个站点补 `labels.site`
- `prometheus-rules.yml`: 告警规则
- `alertmanager.yml`: Alertmanager 到飞书适配层的配置。现在默认按 `alertname + site + severity` 分组，`group_wait=1m`、`group_interval=10m`、`repeat_interval=2h`，并关闭 `send_resolved`，优先压低告警风暴。
- `Dockerfile.feishu-alert-adapter`: 飞书适配层镜像构建文件
- `grafana-dashboard.json`: Grafana 面板，导入完成后先在顶部 `数据源` 变量中选择 Prometheus，再通过 `站点 / 渠道 / 模型` 变量联动查看多站点数据
- 这里没有继续使用导入时的 `__inputs` 数据源替换，因为 Grafana 对 query 变量的 `templating.list[].datasource` 占位符替换存在限制，容易出现 `Datasource named ${...} was not found`。所以这里改成了 Grafana 官方支持的数据源变量方案。
- `docker-compose.monitoring.yml`: 本地联调用 Compose 示例，已经包含 `prometheus + alertmanager + grafana + feishu-alert-adapter`

## 7. 建议的接入顺序

1. 启动 `new-api` 并开启 `PROMETHEUS_ENABLED=true`
2. 用 `curl -H "Authorization: Bearer ..."` 验证 `/metrics`
3. 在 `prometheus.yml` 里为每个站点单独配置 scrape job，并补上唯一的 `labels.site`
4. 填好 `docker-compose.monitoring.yml` 里的飞书 Webhook 和适配层 Bearer Token
5. 启动 `Prometheus + Alertmanager + Grafana + feishu-alert-adapter`
6. 导入 `grafana-dashboard.json`
7. 根据生产阈值微调 `prometheus-rules.yml`

## 8. 这次为“先稳定送达”做了什么

- `NewAPIChannelHasErrors` 从单次错误即告警，放宽为 5 分钟内至少 3 次错误且总请求数至少 10。
- Alertmanager 分组从 `channel_id + channel_name` 级别，收敛成 `alertname + site + severity` 级别。
- Alertmanager 首次等待时间改成 1 分钟，组内更新间隔改成 10 分钟，重复提醒改成 2 小时。
- 默认关闭 `send_resolved`，先不把恢复通知也发到飞书。
- 增加 `feishu-alert-adapter`，在飞书前面再做一层 Bearer 鉴权、文本适配和最小发送间隔抑制。
