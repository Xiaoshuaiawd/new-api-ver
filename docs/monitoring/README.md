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
- Grafana 仪表盘里的 `站点`、`渠道`、`模型` 变量已经改成 `query_result + regex` 方案，避免在部分 Grafana 版本里因为 `label_values(...)` 带过滤条件而触发插件报错。

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

- 看 `go_gc_duration_seconds`、`go_goroutines`、`newapi_http_request_duration_seconds_bucket`
- 告警规则见 `prometheus-rules.yml` 中：
  - `NewAPIHighGCPauseP99`
  - `NewAPIRelayP99TooHigh`
  - `NewAPIHTTPInflightTooHigh`
  - `NewAPISystemPerformanceRejected`

### 4.2 每个渠道延迟

- 5 分钟平均总用时：`sum by (site, channel_id, channel_name) (rate(newapi_channel_attempt_duration_seconds_sum{site=~"${site:regex}", channel_name=~"${channel:regex}", result="success"}[5m])) / sum by (site, channel_id, channel_name) (rate(newapi_channel_attempt_duration_seconds_count{site=~"${site:regex}", channel_name=~"${channel:regex}", result="success"}[5m]))`
- 5 分钟平均首字用时：`sum by (site, channel_id, channel_name) (rate(newapi_channel_first_response_duration_seconds_sum{site=~"${site:regex}", channel_name=~"${channel:regex}"}[5m])) / sum by (site, channel_id, channel_name) (rate(newapi_channel_first_response_duration_seconds_count{site=~"${site:regex}", channel_name=~"${channel:regex}"}[5m]))`
- 看板展示：
  - `Selected Channels Avg Total Duration (5m)`
  - `Selected Channels Avg First Token Duration (5m)`
- 其中首字平均用时只对最近 5 分钟内真正产生过首包的流式请求有值。
- 告警：`NewAPIChannelLatencyP95TooHigh`

### 4.3 每个渠道调用情况、队列、RPM

- Dashboard 里的 `站点`、`渠道`、`模型` 变量都支持多选，也支持 `All`。其中 `渠道`、`模型` 会跟随当前选择的 `站点` 自动联动筛选。
- 多站点下所有渠道面板都会按 `site + channel` 组合分别展示，不会把不同站点里同名或同 ID 的渠道合并到一起。
- 当前真实流量折线：`sum by (site, channel_id, channel_name) (increase(newapi_channel_requests_total{site=~"${site:regex}", channel_name=~"${channel:regex}"}[$__rate_interval]))`
- 队列原始指标：`newapi_channel_inflight_requests`
- RPM：`sum by (site, channel_id, channel_name) (newapi_channel_success_rpm_last_1m{site=~"${site:regex}", channel_name=~"${channel:regex}"})`
- `Selected Channels Request Traffic` 面板基于 `newapi_channel_requests_total` 计数器，按 Grafana 当前选择的时间范围自动分桶统计真实请求数，包含成功和失败请求，不再固定看最近 60 秒。
- `Selected Channels RPM (Last 1m)` 面板按 `站点 / 渠道` 画折线，展示应用内精确维护的“最近 60 秒成功请求数”，不依赖 Prometheus `increase` 外推。

### 4.4 每个渠道报错情况

- 用 `newapi_channel_errors_total`
- `Selected Channels Errors` 面板按 `sum by (site, channel_id, channel_name) (increase(newapi_channel_errors_total{site=~"${site:regex}", channel_name=~"${channel:regex}"}[$__rate_interval]))` 画折线，选中多个站点或多个渠道时每个 `站点 / 渠道` 各自单独出线。
- 最近错误明细表：`newapi_channel_last_error_timestamp_seconds`
- `Selected Channels Latest Error Details` 会展示最近一次错误对应的站点、模型、渠道、状态码和错误详情，并用 `${__from}` 过滤掉当前时间范围之前的旧错误。
- 错误详情会先做敏感信息脱敏，并把换行压成单行；为了控制标签基数，超长消息会截断到 240 个字符。
- 告警：`NewAPIChannelHasErrors`

### 4.5 每个渠道成功率

- 成功率公式：

```promql
100 *
sum by (site, channel_id, channel_name) (
  increase(newapi_channel_requests_total{site=~"${site:regex}", result="success", channel_name=~"${channel:regex}"}[$__rate_interval])
)
/
clamp_min(
  sum by (site, channel_id, channel_name) (
    increase(newapi_channel_requests_total{site=~"${site:regex}", channel_name=~"${channel:regex}"}[$__rate_interval])
  ),
  1
)
```

- `Selected Channels Success Rate` 面板按上面的公式画折线，选中多个站点或多个渠道时每个 `站点 / 渠道` 各自单独出线。

### 4.6 每个渠道调用模型分布与模型成功率

- Dashboard 新增 `Model` 多选变量，并且会跟随当前 `站点 + 渠道` 选择动态列出模型。
- 模型分布折线：`sum by (site, channel_id, channel_name, model_name) (increase(newapi_channel_model_requests_total{site=~"${site:regex}", channel_name=~"${channel:regex}", model_name=~"${model:regex}"}[$__rate_interval]))`
- 模型成功率折线：

```promql
100 *
sum by (site, channel_id, channel_name, model_name) (
  increase(newapi_channel_model_requests_total{site=~"${site:regex}", result="success", channel_name=~"${channel:regex}", model_name=~"${model:regex}"}[$__rate_interval])
)
/
clamp_min(
  sum by (site, channel_id, channel_name, model_name) (
    increase(newapi_channel_model_requests_total{site=~"${site:regex}", channel_name=~"${channel:regex}", model_name=~"${model:regex}"}[$__rate_interval])
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
- `alertmanager.yml`: 告警分组与 webhook 占位示例
- `grafana-dashboard.json`: Grafana 面板，导入完成后先在顶部 `数据源` 变量中选择 Prometheus，再通过 `站点 / 渠道 / 模型` 变量联动查看多站点数据
- 这里没有继续使用导入时的 `__inputs` 数据源替换，因为 Grafana 对 query 变量的 `templating.list[].datasource` 占位符替换存在限制，容易出现 `Datasource named ${...} was not found`。所以这里改成了 Grafana 官方支持的数据源变量方案。
- `docker-compose.monitoring.yml`: 本地联调用 Compose 示例

## 7. 建议的接入顺序

1. 启动 `new-api` 并开启 `PROMETHEUS_ENABLED=true`
2. 用 `curl -H "Authorization: Bearer ..."` 验证 `/metrics`
3. 在 `prometheus.yml` 里为每个站点单独配置 scrape job，并补上唯一的 `labels.site`
4. 启动 `Prometheus + Alertmanager + Grafana`
5. 导入 `grafana-dashboard.json`
6. 根据生产阈值微调 `prometheus-rules.yml`
