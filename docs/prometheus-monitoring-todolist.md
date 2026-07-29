# Prometheus 监控建设实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **执行要求：** 后续实现应按本文档逐项推进；每个功能先补行为测试，再写最小实现。复选框只表示已经具备可复现证据的完成项。

**目标：** 为 new-api 建立默认安全、低基数、支持多实例聚合的 Prometheus 指标、Grafana 面板和 Alertmanager 告警，同时保持现有渠道页与性能看板的行为不变。

**架构：** 应用实例各自导出请求和进程指标，Prometheus 通过 Counter 求和、Gauge 按指标语义聚合；共享数据库状态只由 Master 导出。Prometheus 用于趋势与告警，现有数据库日志继续承担精确用量、账务和审计查询。

**技术栈：** Go 1.22+、Gin、GORM v2、`prometheus/client_golang v1.22.0`、Prometheus、Grafana、Alertmanager、Docker Compose。

## 全局约束

- 指标代码不得阻断 Relay、计费、退款或异步任务主流程。
- 所有标签必须来自本文档中的固定枚举或受控 ID；禁止用户、Token、IP、Request ID、订单号和错误文本进入标签。
- 数据库 collector 必须兼容 SQLite、MySQL >= 5.7.8 和 PostgreSQL >= 9.6。
- 计费指标只能挂在最终持久化事件旁，遵守现有 quota saturation 审计链路，不能重新计算或重复扣费。
- `/metrics` 默认关闭；启用后必须通过 Bearer、IP/CIDR 白名单或显式 Public 模式之一完成安全决策。
- 已有 `service/channel_runtime_metrics.go` 与 `pkg/perf_metrics` 保留原职责，Prometheus 不从两者读取历史数据。

---

> 状态（2026-07-29）：P0-A、P0-B 已完成；P0-C 的规则、Alertmanager 示例、Grafana provisioning/dashboard、Prometheus 配置、独立 Compose 和部署文档已完成静态实现与本地联调，发布验收仍保留生产基数校准、活动 Relay inflight 多实例下钻等环境项。P1 D1-D10 能力保留；D11 已将默认展示重构为主机、程序、中间件、渠道四层中文监控，新增 Node/PostgreSQL/MySQL/Redis Exporter、渠道 TTFT/Token 指标和 1 分钟/5 分钟实时聚合规则。D12 新增实时流式首字等待、主机/进程/Exporter/中间件告警以及渠道阈值基础设施。当前共 47 条基础 Recording Rules、72 条告警、0 条默认 Relay/渠道延迟与并发阈值规则和 108 条 Dashboard PromQL。DB 等待、Goroutine/heap 增长、任务积压、Relay/渠道延迟与并发阈值都需要生产校准，生产证据完成前不视为最终值。
>
> 勾选规则：只有代码、测试、配置或文档已经落地并通过对应验收时，才允许把条目标记为 `[x]`。仅完成设计、创建空文件或本地手工观察不算完成。

## 当前进度

| 阶段 | 状态 | 已落地 | 完成前还缺 |
| --- | --- | --- | --- |
| P0-A 基础接入 | 代码与自动测试已完成 | 独立 Registry、安全 `/metrics`、Go/Process/Build Info、DB 连接池 | 部署环境手工抓取纳入 P0-C 联调 |
| P0-B 流量与渠道 | 代码与自动测试已完成 | HTTP、Relay、流式、渠道 attempt/retry/inflight/duration、固定错误分类及 `ErrorType` 兜底、Master-only `channel_enabled`/collector 健康状态、渠道 Histogram 关闭开关、客户端取消与 deadline 传播、异常流失败、clean EOF 成功回归、Prometheus/性能看板统一最终成功判定、Midjourney controller 成功边界 | 生产 `R/N` 基数验收归 P0-C 发布联调 |
| P0-C 可视化与告警 | 进行中 | Recording/告警规则、Prometheus/Alertmanager 配置、Grafana 双文件夹 provisioning 与 6 个中文 dashboard、独立 Compose、静态验证脚本、部署文档和本地运行联调 | 生产基数校准、活动 Relay inflight 的多实例下钻、四层 dashboard 的发布环境容器加载和完整发布验收 |
| P1 业务监控 | 进行中 | D1-D10 限流、Redis/缓存、计费、异步任务、transport 首字节、固定错误、DB 等待、Runtime 增长和 Relay 阈值基础设施；D11 渠道 TTFT、Token 吞吐、上游缓存率、1 分钟实时渠道口径和四层中文看板 | 发布环境加载验收与生产阈值校准 |

注：本文中“口径已确定”与“代码已实现”是两种状态。规范性条目可因文档决策已落地而勾选；指标、collector 和验收条目必须有对应代码与测试依据。

### 完成定义与证据

| 条目类型 | 允许勾选的最低证据 | 不足以勾选的情况 |
| --- | --- | --- |
| 口径/架构决策 | 本文已有明确枚举、公式、来源和聚合方式，且没有待定项 | 只有一句目标描述 |
| 应用指标 | 指标已注册、真实业务生命周期已接入、对应行为测试通过 | 只注册 collector 或只测试 Counter `+1` |
| 共享 collector | Master/Slave、空数据、查询失败和自监控测试通过 | 只在单实例手工看到样本 |
| Recording/告警规则 | `promtool check rules` 和 `promtool test rules` 通过 | YAML 能被编辑器打开 |
| Grafana 静态面板 | dashboard JSON 已纳入 provisioning，变量、面板 ID、数据源、PromQL 和 No data 语义通过静态校验 | 只提交 dashboard JSON 或只能被 JSON 解析 |
| Grafana 运行验收 | 发布环境 provisioning 成功，核心查询有数据，变量、No data 和 Master-only 面板语义可用 | 仅静态校验通过或本机单次导入 |
| 部署验收 | 测试环境完成抓取、鉴权、告警触发与恢复，并保存基数结果 | 本机单次 `curl` 成功 |

### 已完成证据索引

| 能力 | 主要代码 | 行为测试 |
| --- | --- | --- |
| 安全 `/metrics`、独立 Registry | `pkg/prometheus_metrics/config.go`、`registry.go`、`router/metrics-router.go` | `config_test.go`、`registry_test.go`、`router/metrics_router_test.go` |
| HTTP 指标与自统计排除 | `middleware/prometheus_http.go` | `middleware/prometheus_http_test.go` |
| Relay、渠道 attempt、取消传播与真实重试 | `controller/relay.go`、`relay/channel/api_request.go`、`service/channel_runtime_metrics.go` | `controller/relay_metrics_test.go`、`relay/helper/stream_scanner_test.go`、`service/channel_runtime_metrics_test.go` |
| 渠道 transport 首字节 | `pkg/prometheus_metrics/channel.go`、`service/channel_runtime_metrics.go`、`relay/channel/api_request.go`、`relay/channel/aws/relay-aws.go`、`relay/channel/xunfei/relay-xunfei.go`、`relay/channel/volcengine/tts.go` | `pkg/prometheus_metrics/channel_test.go`、`relay/channel/api_request_test.go`、`relay/channel/aws/relay_aws_test.go`、`relay/channel/advancedcustom/adaptor_test.go`、`relay/channel/xunfei/relay_xunfei_metrics_test.go`、`relay/channel/volcengine/tts_metrics_test.go` |
| 流式最终成功判定统一 | `relay/common/relay_info.go`、`controller/relay.go`、`pkg/perf_metrics/metrics.go` | `relay/common/relay_info_test.go`、`controller/relay_metrics_test.go`、`pkg/perf_metrics/metrics_test.go` |
| Midjourney 渠道 attempt 与最终 Relay 结果 | `relay/mjproxy_handler.go`、`controller/relay.go` | `relay/mjproxy_handler_test.go`、`controller/relay_metrics_test.go` |
| DB 与共享渠道 collector | `pkg/prometheus_metrics/database_collector.go`、`pkg/prometheus_metrics/channel_state_collector.go` | `pkg/prometheus_metrics/database_collector_test.go`、`pkg/prometheus_metrics/channel_state_collector_test.go` |
| 限流拒绝 | `pkg/prometheus_metrics/rate_limit.go`、`middleware/rate-limit.go`、`middleware/model-rate-limit.go`、`middleware/email-verification-rate-limit.go` | `pkg/prometheus_metrics/rate_limit_test.go`、`middleware/rate_limit_test.go`、`middleware/model_rate_limit_test.go` |
| Redis 与应用缓存 | `pkg/prometheus_metrics/redis.go`、`common/cache_metrics.go`、`pkg/cachex/metrics.go`、`common/redis.go`、`pkg/cachex/hybrid_cache.go` | `pkg/prometheus_metrics/redis_test.go`、`common/redis_metrics_test.go`、`pkg/cachex/hybrid_cache_metrics_test.go`、`middleware/rate_limit_test.go` |
| 计费、Token 与实际额度 | `pkg/prometheus_metrics/billing.go`、`model/log.go`、`service/billing.go`、`service/billing_session.go`、`service/task_billing.go` | `pkg/prometheus_metrics/billing_test.go`、`service/billing_metrics_test.go`、`service/task_billing_test.go` 及对应计费会话测试 |
| 异步任务提交、poll、首次终态与队列 | `pkg/prometheus_metrics/task.go`、`task_queue_collector.go`、`controller/relay.go`、`service/task_polling.go`、`controller/midjourney.go`、`relay/relay_task.go`、`relay/mjproxy_handler.go` | `task_test.go`、`task_queue_collector_test.go`、`controller/relay_metrics_test.go`、`service/task_polling_test.go`、`controller/midjourney_metrics_test.go`、`relay/relay_task_metrics_test.go`、`relay/mjproxy_handler_test.go`、`model/task_cas_test.go` |
| Recording/Alert Rules | `deploy/monitoring/recording-rules.yml`、`alert-rules.yml` | `recording-rules.test.yml`、`alert-rules.test.yml` |
| P0-C/P1 静态部署产物 | `docker-compose.monitoring.yml`、`deploy/monitoring/prometheus.yml`、Exporter、Alertmanager/Grafana 配置、`core/` 四层 dashboard、`extended/` 计费/任务 dashboard、`docs/prometheus-monitoring.md` | `deploy/monitoring/validate.sh`：Prometheus/rules、PostgreSQL/MySQL profile、Compose、YAML/JSON、47 条基础 Recording Rules、72 条告警、0 条默认 Relay/渠道延迟与并发阈值规则和 108 条 dashboard PromQL 静态校验 |

证据索引只说明“在哪里验证”，不替代对应批次的验收命令。测试名称或文件调整时必须同步更新本表，避免文档中的完成状态失去来源。

### 当前关键路径

1. 在发布环境使用受信任的镜像仓库或离线导入相同固定版本镜像，复现本地已通过的启动、抓取和告警联调。
2. 在测试环境代入实际路由数 `R`、渠道数 `N`，完成基数、安全抓取和多实例聚合验收。
3. 在发布环境验证 Node、PostgreSQL、Redis Exporter 与 new-api target 全部 `UP`，MySQL profile 未启用时不产生假 `DOWN` target。
4. 验证 Grafana 两个中文文件夹和 6 个 dashboard 自动加载，并用真实多渠道流量确认 RPM、P90/P95、TTFT、上游首字节、缓存率和 Token 吞吐按 `channel_id` 分开。
5. 上线后使用真实积压、完成率、poll 错误率以及按 `relay_format` 的 P95/P99 耗时和 inflight 分布校准候选阈值；生产积压、Relay 延迟与并发阈值保持未校准状态，不能仅凭本地样本宣称完成。

### 运行验收状态记录（2026-07-28）

本次验收使用独立的本地 SQLite 应用实例，配置 `PROMETHEUS_ENABLED=true`、Bearer 保护、`NODE_TYPE=master` 和固定节点名。Docker Hub 不可达时，使用固定版本的 Quay/Grafana 镜像镜像源完成本地拉取，并仅在本机 retag 为 Compose 默认名称；生产部署仍必须验证镜像来源和 digest 后再使用。

- [x] 已确认 Docker daemon 可用（Docker 29.6.1）。
- [x] 已确认应用测试实例可访问（`http://localhost:3000` 返回 200）。
- [x] Prometheus `v3.5.0`、Alertmanager `v0.28.1`、Grafana `12.1.0` 容器均 healthy。
- [x] Prometheus `new-api` target 为 `UP`，`cluster=default`、`instance=new-api-1` 标签可查询。
- [x] Grafana 已验证自动加载 `newapi-system-overview` 和 `newapi-channel-overview`；`newapi-billing-overview` 与 `newapi-task-overview` 已纳入同一 provisioning 路径和静态验证，发布环境容器加载待复现。
- [x] Alertmanager 已验证 firing → resolved 通知、critical 抑制 warning，以及静默规则；Webhook 收到 firing/resolved 请求且失败计数为 0。
- [x] 本地已验证 Master collector absent 持续 5 分钟后 firing，Master 恢复后规则 resolved 并进入 warning 接收器。
- [ ] 尚未验证生产环境的镜像来源、网络和真实 R/N 基数。

发布环境必须从仓库根目录重新执行 C2/C3 验收，不得仅凭应用 `/metrics` 的单次 `curl` 勾选运行条目。若部署环境不能访问 Docker Hub，应先将相同版本镜像通过受信任的镜像仓库或离线导入方式准备好，再执行同一套命令；不得改用 `latest` 绕过版本固定。

### 运行验收证据模板

每次 C2/C3 验收在发布记录中保存以下信息，至少保留命令、时间、版本和关键输出：

| 证据 | 必须记录 |
| --- | --- |
| 应用 | `git rev-parse HEAD`、`NODE_NAME`、`NODE_TYPE`、`PROMETHEUS_*` 安全模式；无 Token 返回 403，正确 Bearer 返回 200 |
| Compose | `docker compose ... ps` 中 Prometheus/Grafana/Alertmanager 均 healthy；镜像完整版本号 |
| Prometheus | `/-/ready`、`up{job="new-api"}`、`/api/v1/rules` 中规则组状态、`newapi_build_info` 和 DB/collector 指标 |
| Grafana | `/api/health`、6 个 dashboard UID：`newapi-host-overview`、`newapi-application-overview`、`newapi-middleware-overview`、`newapi-channel-overview`、`newapi-billing-overview`、`newapi-task-overview`；两个中文文件夹、变量可查询且核心面板不是意外 No data |
| Alertmanager | `/-/ready`、`/api/v2/status`；告警 firing → resolved，warning/critical 抑制结果和接收端响应 |
| 基数 | `prometheus_tsdb_head_series`、按 `__name__` 聚合的 series 数、活跃路由 `R`、渠道数 `N`、是否启用渠道 Histogram |
| 多实例 | 每个 target 的 `job/cluster/instance`、Counter `sum(rate())`、inflight 按 instance 下钻、Master-only collector 只有一份 |

推荐的最小检查命令：

```bash
docker compose -f docker-compose.monitoring.yml ps
curl -fsS http://localhost:9090/-/ready
curl -fsS http://localhost:3001/api/health
curl -fsS http://localhost:9093/-/ready
curl -fsS http://localhost:9090/api/v1/query --get \
  --data-urlencode 'query=up{job="new-api"}'
curl -fsS http://localhost:9090/api/v1/query --get \
  --data-urlencode 'query=newapi_build_info{job="new-api"}'
curl -fsS http://localhost:3001/api/search | jq '.[].uid'
curl -fsS http://localhost:9093/api/v2/status | jq '{version,cluster}'
```

`alertmanager.yml.example` 的默认 webhook 是占位地址，不能证明真实通知送达。要验证通知，必须在测试环境替换为临时、可审计的接收端；如果只验证 Alertmanager 状态 API、firing/resolved 生命周期和抑制计算，应在记录中明确标注“未验证外部通知送达”。

## 目标

使用 Prometheus 采集服务、Relay、渠道、计费、异步任务和基础设施指标，使用 Grafana 展示，使用 Alertmanager 告警。

Prometheus 只负责运行趋势、聚合和告警；用户、Token、IP、Request ID、订单等明细继续以结构化日志和数据库为准，精确账务也始终以数据库记录为准。

## 范围边界

- [x] Prometheus 不替代消费日志、账单、审计日志和渠道列表页的实时内存统计。
- [x] P0 不建设长期历史明细查询、用户级用量查询、Token 级用量查询或财务对账接口。
- [x] P0 不新增 Kubernetes/Helm、OpenTelemetry Collector、远程存储或高可用 Prometheus；这些能力按部署规模另行规划。
- [x] 指标记录失败不得影响 Relay、计费和任务结算主流程；collector 查询失败应导出自监控错误并记录日志，不能让 `/metrics` 整体 panic。
- [x] 所有指标名称、标签和值都必须先进入本文档的指标字典，禁止在 adaptor 或业务调用点临时发明指标。

## 当前代码锚点

| 能力 | 当前入口 | 规划中的定位 |
| --- | --- | --- |
| 渠道列表实时并发/成功 RPM | `service/channel_runtime_metrics.go` | 保留单实例实时展示，不作为 Prometheus 数据源 |
| 模型/分组性能看板 | `pkg/perf_metrics` | 保留 Redis/DB 聚合，不作为 Prometheus 数据源 |
| 普通 Relay 重试循环 | `controller/relay.go` | 记录一次最终请求和多次渠道 attempt |
| 任务 Relay 重试循环 | `controller/relay.go` | 记录提交结果和渠道 attempt |
| Midjourney 子路径 | `relay/mjproxy_handler.go` | 复用各子路径现有成功判定 |
| 流式终止状态 | `relay/common/stream_status.go`、`relay/helper/stream_scanner.go` | clean EOF 与业务 Done 属于正常结束；取消、超时、scanner error 等属于异常结束 |
| 性能看板成功样本 | `pkg/perf_metrics/metrics.go` | 与 Prometheus 复用最终成功判定，但继续独立存储 |
| HTTP 路由挂载 | `router/main.go`、`router/relay-router.go` | `/metrics` 必须在根 Engine 的 Relay 全局中间件之前注册 |
| 数据库连接池 | `model.DB`、`model.LOG_DB` | 每次 scrape 读取 `DB.DB().Stats()` |
| 计费日志与倍率快照 | `service/log_info_generate.go`、`service/task_billing.go` | 生成最终 quota、冻结倍率、计费来源和饱和审计元数据；Counter 由日志持久化结果驱动 |
| 额度饱和审计 | `attachQuotaSaturation` | 复用同一个 clamp 事件，避免双重判定 |
| 计费会话 | `service/billing.go`、`service/billing_session.go` | 记录预扣、结算和异步退款的最终操作结果，幂等早退不重复计数 |
| 最终计费事件 | `model/log.go` 的 `RecordConsumeLog`/`RecordTaskBillingLog` | `createLog` 成功后按 consume/refund 分流额度、Token 和饱和事件 |
| 订阅拒绝决策 | `service/billing_session.go`、`model/subscription.go` | 在最终返回客户端的决策分支记录，不通过错误 message 文本反向分类 |
| 异步任务生命周期 | `controller/relay.go`、`service/task_polling.go`、`model/task.go`，以及 `relay/mjproxy_handler.go`、`controller/midjourney.go`、`model/midjourney.go` | 同时覆盖 `tasks` 与 `midjourneys` 两套存储；提交在最终响应边界记录，完成只在状态 CAS 成功的实例记录，队列快照仅 Master 导出 |

## 当前基础能力

- [x] `service/channel_runtime_metrics.go` 保留为渠道列表页的单实例实时数据源，不作为 Prometheus 的历史数据源；多实例部署时，页面值只代表处理该页面请求的实例，不能当作集群总值。
- [x] 渠道页面 RPM 已明确为最近 60 秒成功 RPM，失败请求不会增加 RPM。
- [x] 进程内渠道状态支持滚动窗口、空闲状态回收和 panic 时并发归还。
- [x] 普通 Relay、任务 Relay、Midjourney Submit、SwapFace 和 ImageSeed 已统一进入渠道 attempt 生命周期。
- [x] `TrackChannelAttempt` 已成为渠道 attempt 的统一入口，同时驱动渠道页内存统计和 Prometheus recorder，两套数据互不读取。
- [x] `pkg/perf_metrics` 继续服务模型/分组性能看板及 Redis/数据库持久化，Prometheus 不读取其 hot bucket、Redis 或数据库结果。
- [x] `prometheus/client_golang v1.22.0` 已提升为 direct 依赖。
- [x] P0-A 已实现独立 Registry、安全 `/metrics`、Go/Process/Build Info 和数据库连接池指标。
- [x] HTTP、Relay、流式和渠道业务指标已实现并接入业务生命周期。
- [x] 渠道 attempt 总耗时和 transport 首字节 Histogram 可通过 `PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true` 一起关闭，Counter/Gauge 继续保留。
- [x] Master-only 渠道启用状态 collector 已实现，Slave 不注册也不查询。
- [x] Recording Rules 与首批 Prometheus 告警规则已实现，并有固定输入规则测试。
- [x] Prometheus/Alertmanager 配置、Grafana provisioning/dashboard、独立 Compose、secret 示例与部署文档已完成静态实现。
- [x] 本地监控栈已完成实际抓取、dashboard 自动加载、通知恢复、抑制和静默联调；发布环境仍需使用受信任镜像来源复现。

## 一、指标口径与命名规范（P0）

### 1.1 请求层级

- [x] 最终请求：进入 Relay controller 的一次客户端调用只记录一次 `newapi_relay_requests_total`，包括解析、计价、选渠道或上游调用阶段失败；内部重试不增加最终请求数。
- [x] 渠道 attempt：渠道选定且请求体恢复完成后，在进入 provider handler 前开始；即使后续在请求转换阶段失败、尚未真正发出网络请求，也属于该渠道的一次执行 attempt。
- [x] 如未来需要区分“provider handler attempt”和“真实网络发送次数”，另增 transport 层指标；不得把两种口径混入 `newapi_channel_attempts_total`。
- [x] 重试：只有 `retry_index > 0` 且新的渠道 attempt 真正开始时，才增加 `newapi_channel_retries_total`；选渠道失败且没有进入 provider handler 不算重试 attempt。
- [x] 当前并发：Relay/attempt 开始 Gauge `+1`，结束 Gauge `-1`；成功、失败、取消、超时和 panic 都必须归还。
- [x] 指标完成函数已使用 `sync.Once` 保护，防止成功路径与 defer 兜底重复归还、重复计数。

### 1.2 统一 attempt 接口演进

已从仅有 `channelID + success` 的内存统计入口演进为 `TrackChannelAttempt(meta, operation)` 统一生命周期。渠道页内存统计和 Prometheus recorder 在该入口内同时完成，但不互相读取数据。

当前接口表达的领域信息：

```go
type ChannelAttemptMeta struct {
	ChannelID   int
	ChannelType int
	Stream      bool
	RetryIndex  int
	RetryReason string
}

type ChannelAttemptOutcome struct {
	Success    bool
	Err        error
	ErrorType  types.ErrorType
	ErrorCode  types.ErrorCode
	StatusCode int
}
```

- [x] 统一入口内部同时驱动现有 `channel_runtime_metrics` 和 Prometheus recorder，但两套数据各自存储、互不读取。
- [x] attempt 开始时记录 inflight 和重试，结束时记录结果与耗时。
- [x] panic 兜底按 `result="failure", error_type="internal"` 完成指标后重新抛出，由现有 Gin recovery 处理。
- [x] 普通 Relay、任务 Relay 和 Midjourney 各子路径只负责返回统一 outcome，不直接操作 Prometheus collector。
- [x] 渠道页 60 秒窗口保留可注入时钟，窗口测试不依赖 `time.Sleep`。
- [ ] Prometheus 耗时 recorder 如需精确边界测试，再注入计时函数；当前测试只验证样本数和生命周期，不断言具体耗时。

### 1.3 成功口径

- [x] 普通 Relay 最终成功：`controller/relay.go` 返回时 `newAPIError == nil`。
- [x] 任务 Relay 提交成功：`controller/relay.go` 中 `taskErr == nil`。
- [x] Midjourney Submit 成功：业务码为 `1`、`21` 或 `22`。
- [x] Midjourney SwapFace 成功：HTTP 200 且业务码为 `1`。
- [x] Midjourney ImageSeed 成功：HTTP 状态码为 2xx。
- [x] 流式请求最终成功口径已确定：存在 `StreamStatus` 时必须属于正常结束、`EndError == nil` 且没有 soft error；尚未接入 `StreamStatus` 的流式格式沿用 handler 最终 `newAPIError == nil`。TTFT/是否已发送响应头只用于耗时，不单独决定成功。
- [x] 最终 Relay 指标由 controller 的统一 defer/完成点记录，不依赖消费日志写入成功。
- [x] `RelayInfo.FinalSuccess(handlerSuccess)` 是流式最终成功判定的唯一实现；`relayMetricsOutcome` 与 `pkg/perf_metrics.RecordRelaySample` 都复用它，但两者独立记录，不能互相读取数据。

#### 流式最终判定矩阵

| handler 结果 | `StreamStatus` | `EndReason` | `EndError` / soft error | 最终结果 |
| --- | --- | --- | --- | --- |
| 失败 | 任意 | 任意 | 任意 | 失败，以 handler 的 `NewAPIError` 分类 |
| 成功 | `nil` | - | - | 成功，作为尚未接入状态跟踪格式的兼容分支 |
| 成功 | 非空 | `done`、`eof`、`handler_stop` | 均无错误 | 成功 |
| 成功 | 非空 | `done`、`eof`、`handler_stop` | 任一有错误 | 失败；禁止因结束原因“正常”而忽略错误 |
| 成功 | 非空 | `timeout` | 任意 | 失败，`error_type="timeout"` |
| 成功 | 非空 | `client_gone` | 任意 | 失败，`error_type="client_cancelled"` |
| 成功 | 非空 | `scanner_error`、`panic`、`ping_fail`、空值或未知值 | 任意 | 失败，按底层 error 分类，无法细分时归 `internal` |

`eof` 指上游 reader 的 clean EOF，按项目现有协议语义属于正常完成；不能用“首包后连接关闭”直接推断失败。B2 已分别构造客户端取消、deadline 和首包后取消，并增加 clean EOF 成功回归，防止误伤正常 SSE 结束路径。

### 1.4 Counter 设计

- [x] 不单独创建 `channel_success_total` 和 `channel_failures_total`，避免与 attempts 重复计数。
- [x] 使用 `newapi_channel_attempts_total{result="success|failure"}` 统一表达成功和失败。
- [x] 使用 `newapi_relay_requests_total{result="success|failure"}` 统一表达最终请求结果。
- [x] 成功 RPM、失败 RPM 和成功率全部通过 PromQL 从 Counter 派生，应用端不再维护 Prometheus RPM Gauge。

```promql
# 示例以 job 作为部署边界；多集群共用 Prometheus 时还需保留 cluster。
# 渠道成功 RPM
sum by (job, channel_id) (
  rate(newapi_channel_attempts_total{result="success"}[1m])
) * 60

# 渠道所有尝试 RPM
sum by (job, channel_id) (
  rate(newapi_channel_attempts_total[1m])
) * 60

# 渠道重试 RPM
sum by (job, channel_id) (
  rate(newapi_channel_retries_total[1m])
) * 60

# 渠道尝试成功率
sum by (job, channel_id) (rate(newapi_channel_attempts_total{result="success"}[5m]))
/
clamp_min(sum by (job, channel_id) (rate(newapi_channel_attempts_total[5m])), 0.000001)
```

## 二、标签和时间序列预算（P0）

- [x] `route` 使用 Gin 模板路由 `c.FullPath()`；无法取得模板时统一使用 `unmatched`，不使用包含真实 ID 的 URL Path。
- [x] `status_class` 固定为 `2xx`、`3xx`、`4xx`、`5xx`、`other`。
- [x] `result` 固定为 `success`、`failure`。
- [x] 成功请求的 `error_type` 固定为 `none`。
- [x] `stream` 固定为 `true`、`false`。
- [x] `billing_source` 固定为 `wallet`、`subscription`、`unknown`。
- [x] 兼容现有语义：`relayInfo.BillingSource == ""` 归一为 `wallet`；只有确实无法判断来源时才使用 `unknown`。
- [x] `channel_id` 使用十进制字符串，`channel_type` 使用稳定的渠道类型编号。
- [x] `relay_format` 由 `types.RelayFormat` 集中归一化为固定值；未知值统一归入 `other`。
- [x] `relay_format` 当前允许值固定为 `openai`、`claude`、`gemini`、`openai_responses`、`openai_responses_compaction`、`openai_alpha_search`、`openai_audio`、`openai_image`、`openai_realtime`、`rerank`、`embedding`、`task`、`mj_proxy`、`other`。
- [x] `newapi_channel_retries_total.reason` 复用固定 `error_type` 分类，表示触发下一次 attempt 的上一次失败原因，不使用完整错误码或文本。
- [x] P0 的 Relay/渠道指标不包含 `model` 标签。
- [ ] P1 如需模型维度，只允许配置中已存在且通过规范化的模型名称；未知、超长或用户动态模型统一归入 `other`。
- [x] 已实现的 P0 指标不使用用户 ID、Token ID、IP、Request ID、渠道名称、渠道 Key、完整错误文本、任务 ID、订单号作为标签。
- [x] 应用代码不主动添加 `instance` 标签，由 Prometheus target/relabel 配置注入。
- [x] 已建立 P0/P1 指标字典，记类型、单位、标签、记录点和集群聚合函数。
- [x] 已补充 P0 高基数指标的时间序列预算公式，Histogram 计入 `_bucket`、`_sum` 和 `_count`。
- [ ] 上线前代入生产环境的活跃路由数 `R` 和渠道数 `N`，验证实际预算并将结果留在发布记录中。
- [x] P0 预算门槛已确定：单实例应用自定义时间序列不超过 `50,000`，单个指标不超过总预算的 `40%`；超过时优先移除 `channel_type` 等可由配置表补充的冗余标签，而不是提高预算。
- [x] 已实现渠道 Histogram 可配置关闭能力；D5 首字节 Histogram 加入后，当预算试算超过 `45,000` 条或渠道数 `N >= 300` 时必须设置 `PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true`，为 Go/Process 和未来增量保留余量。
- [ ] 上线后使用 `prometheus_tsdb_head_series` 和按 metric name 的 series count 验证实际基数。

### 2.1 P0 时间序列预算

定义：

- `R`：实际产生样本的业务路由模板与 HTTP method 组合数。
- `N`：被请求或被共享 collector 导出的渠道数。`channel_type` 由每个 `channel_id` 唯一决定，不按 `N × 渠道类型数` 重复放大。
- Counter 每个已观测标签组合产生 1 条序列；Histogram 每个标签组合产生 `bucket 数 + 2` 条序列。

| 指标组 | 最大标签组合 | 每组序列 | 预算公式 |
| --- | ---: | ---: | ---: |
| HTTP Counter | `5R` | 1 | `5R` |
| HTTP duration Histogram | `5R` | 15 | `75R` |
| Relay Counter/Gauge/Histogram | 固定枚举 | - | 约 `1,764` |
| Channel attempts | `24N` | 1 | `24N` |
| Channel retries | `11N` | 1 | `11N` |
| Channel inflight | `N` | 1 | `N` |
| Channel duration Histogram | `4N` | 15 | `60N` |
| Channel first-byte Histogram | `N` | 14 | `14N` |
| Channel enabled | `N` | 1 | `N` |

当前自定义指标的保守上界约为 `80R + 111N + 1,800`，尚未计入少量 Build/DB/collector 序列。例如 `R=100, N=400` 时已约为 `54,200`；`R=100, N=300` 时约为 `43,100`，因此 D5 后将强制关闭渠道 Histogram 的保守阈值下调为 `N >= 300`。

### 2.2 P1 增量时间序列预算

D3 计费标签全部为固定枚举，保守上界约 `170` 条。D4 在所有固定标签组合都产生样本时约为 `117` 条：submission `8`、completion `8`、poll `8`、duration Histogram `4 × 2 × (8+2) = 80`、queue Gauge `12`、共享 collector 健康状态 `1`。两批合计保守预算约 `287` 条，不改变 P0 的 `50,000` 总门槛。该数字是设计预算，不是生产实测值；Slave 不注册 Master-only queue collector，因此实际每实例序列数会不同。

- [ ] D4 发布环境上线后用 `count by (__name__) ({__name__=~"newapi_(tokens|quota|actual_quota|billing|subscription|task)_.*"})` 同时核对 D3/D4 实际序列，不得因新增动态 platform、model 或错误文本超出预算；发布记录必须保存查询时间、实例数和结果。

## 三、多实例来源与聚合规则（P0）

指标必须先区分“每实例运行状态”和“共享数据库状态”，不能看到 Gauge 就统一 `sum`。

| 指标类别 | 来源范围 | 导出方式 | 集群聚合 |
| --- | --- | --- | --- |
| HTTP/Relay/渠道 Counter | 每实例事件 | 所有实例导出 | `sum(rate(...))` |
| Relay/渠道/流式 inflight | 每实例状态 | 所有实例导出 | `sum`，同时保留 `by(instance)` |
| Go Runtime、进程 CPU/内存 | 每实例状态 | 所有实例导出 | 默认按实例展示 |
| 数据库连接池 | 每实例连接池 | 所有实例导出 | 连接数 `sum`，利用率按实例计算后 `max` |
| Redis 操作 Counter | 每实例事件 | 所有实例导出 | `sum(rate(...))` |
| 渠道启用状态 | 共享数据库状态 | 仅 Master collector 导出 | `max` 作为防御性查询 |
| 异步任务等待/运行数量 | 共享数据库状态 | 仅 Master collector 导出 | 不跨实例求和 |
| Build/版本信息 | 每实例信息 | 所有实例导出 | 按实例展示或 `max` |

- [x] 共享渠道状态 collector 检查 `common.IsMasterNode`，避免每个实例查询同一数据库并重复导出。
- [x] Prometheus target 必须提供稳定的 `job`、唯一的 `instance` 和显式的 `cluster` target 标签；`external_labels` 不会自动写入本地查询序列，不能单独依赖它。Recording Rules 不得跨 `job/cluster` 混算。
- [x] Master 不存在时通过固定 `job="new-api", cluster="default"` 的 `absent()` 规则告警，不让 Slave 重复导出共享状态；多集群部署时必须为每个集群生成或覆写对应规则。
- [x] 示例 target 统一使用全局 `scrape_interval: 15s`、`scrape_timeout: 10s`，并在每个 `static_configs.labels` 中显式设置与 `external_labels` 一致的 `cluster`；四个 dashboard 均提供集群视图与 `instance` 下钻变量。
- [x] 本地测试环境已从 Prometheus 运行时配置确认所有 target 使用统一的 15 秒 scrape interval 和 10 秒 timeout。
- [x] 当前项目没有渠道熔断器，P0 不新增 `channel_circuit_state` 指标；将来真正实现熔断功能时再单独设计。

### 3.1 Gauge 查询约束

Gauge 的单次 scrape 是采样值，不是事件总数。多实例抓取存在秒级错峰，集群 `sum` 适合观察趋势和容量，不应被解释为严格同时刻的精确快照。

```promql
# 集群当前 Relay 并发；保留 relay_format/stream 维度
sum by (job, relay_format, stream) (newapi_relay_inflight)

# 集群当前渠道并发
sum by (job, channel_id) (newapi_channel_inflight)

# 共享渠道启用状态；正常情况下只有 Master 导出一份
max by (job, channel_id, channel_type) (newapi_channel_enabled)

# 单实例 DB 连接池利用率；MaxOpenConnections=0 的 target 必须排除
max by (job, instance, database) (
  (
    newapi_db_connections{state="in_use"}
    /
    newapi_db_max_open_connections
  )
  and on (job, instance, database)
  (newapi_db_max_open_connections > 0)
)
```

- [x] Relay、渠道 inflight 使用 `sum`，渠道启用状态与共享 collector 健康状态使用 `max`，进程状态默认不跨实例相加。
- [x] Recording Rules 已保留 `cluster/job` 部署边界；DB 利用率保留 `instance/database`，先按实例计算再用于告警。
- [x] Grafana dashboard 已同时提供集群 inflight 与按 `instance` 下钻；首批告警持续时间均长于两个 15 秒 scrape interval。
- [ ] 多实例 target、Counter 聚合和 Master-only collector 已验证；仍需用一个真实活动 Relay 请求验证 inflight Gauge 的 `instance` 下钻没有重复。

## 四、Prometheus 接入与安全（P0）

- [x] 将 `prometheus/client_golang v1.22.0` 提升为直接依赖，不引入第二个版本。
- [x] 新建 `pkg/prometheus_metrics` 统一指标注册模块，集中管理 Counter、Gauge、Histogram 和固定标签枚举。
- [x] 在 `InitResources()` 完成数据库和 Redis 初始化后创建监控配置与 registry，再把只读依赖注入 `router.SetRouter`；禁止由业务包反向读取 router 全局变量。
- [x] 增加独立 `/metrics` 路由。
- [x] 在 `router.SetRouter` 中按 `SetApiRouter` → `SetDashboardRouter` → `SetMetricsRouter` → `SetRelayRouter` 的顺序注册；Gin 已注册路由不会被后续根 `Use(...)` 追溯附加，因此 `/metrics` 可绕过 Relay 全局中间件。
- [x] 增加路由顺序回归测试，防止未来把 `SetMetricsRouter` 移到 `SetRelayRouter` 之后。
- [x] 确保 `/metrics` 不进入 `StatsMiddleware`、TokenAuth、Distribute、限流和业务 Relay 统计链。
- [x] HTTP 指标中间件只记录执行后带有 `route_tag=api|old_api|relay` 的请求；`/metrics`、静态资源、前端回退和 `NoRoute` 不进入应用 HTTP 指标。
- [x] 默认关闭 `/metrics`；关闭时路由不存在并返回现有 `404/NoRoute` 行为，而不是返回一个可探测的“disabled”响应。
- [x] 支持 Bearer Token 和 IP 白名单保护；生产环境启用时至少配置一种保护方式。
- [x] Bearer Token 只从环境变量或安全配置读取，不能通过普通系统配置 API 返回明文。
- [x] Bearer Token 使用常量时间比较；认证失败统一返回 `403` 和固定正文 `forbidden\n`，响应中不得透露另一种认证方式的配置状态。
- [x] IP 白名单支持单 IP 和 CIDR，启动时完成解析校验；请求侧只使用 Gin `ClientIP()`，复用现有可信代理配置，不能无条件信任 `X-Forwarded-For`。
- [x] Token 与 IP 白名单同时配置时采用“任一方式通过即可”的 OR 语义；该语义必须写入部署文档和测试。
- [x] 如确需公开 `/metrics`，必须显式配置 `PROMETHEUS_ALLOW_PUBLIC=true`；该开关优先级最高并写启动警告日志。
- [x] `PROMETHEUS_ENABLED=true` 且 Token、IP 白名单、Public 三者均未配置时，应用必须启动失败，不能静默公开或静默禁用。
- [x] 配置包含空 Token、非法 IP/CIDR 或无法识别的布尔值时启动失败，并输出不含敏感值的错误信息。
- [x] 接入默认 Go collector 和 process collector：Goroutine、GC、内存、CPU、文件描述符、启动时间。
- [x] 使用独立 `prometheus.Registry`，禁止注册到全局 DefaultRegisterer，保证测试隔离并避免第三方库意外暴露指标。
- [x] 暴露 `newapi_build_info{version,commit,build_time}`，值恒为 `1`；`version` 使用 `common.Version`，commit/build time 优先读取显式构建变量，其次读取 `debug.ReadBuildInfo()` 的 `vcs.revision`/`vcs.time`，最后使用 `unknown`。
- [x] `/metrics` 使用 `promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorLog: ...})`，collector/gather 错误写入现有日志系统且不能 panic。

建议配置项：

```text
PROMETHEUS_ENABLED
PROMETHEUS_BEARER_TOKEN
PROMETHEUS_ALLOWED_IPS
PROMETHEUS_ALLOW_PUBLIC
PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM
```

安全决策表：

| Enabled | Token | Allowed IPs | Allow Public | 结果 |
| --- | --- | --- | --- | --- |
| false | 任意 | 任意 | 任意 | 不注册 `/metrics` |
| true | 空 | 空 | false | 启动失败 |
| true | 有效 | 空 | false | Bearer 通过后可抓取 |
| true | 空 | 有效 | false | 来源 IP 命中后可抓取 |
| true | 有效 | 有效 | false | Token 或 IP 任一通过即可抓取 |
| true | 任意 | 任意 | true | 公开抓取，并输出高风险警告 |

## 五、核心指标字典

### P0 流量与渠道指标

| 指标 | 类型/单位 | 主要标签 | 记录点 | 集群聚合 |
| --- | --- | --- | --- | --- |
| `newapi_http_requests_total` | Counter/请求 | `route,method,status_class` | 带业务 `route_tag` 的 HTTP 中间件完成点 | `sum(rate())` |
| `newapi_http_request_duration_seconds` | Histogram/秒 | `route,method,status_class` | 带业务 `route_tag` 的 HTTP 中间件在 `c.Next()` 返回后记录 | `histogram_quantile` |
| `newapi_relay_requests_total` | Counter/请求 | `relay_format,stream,result,error_type` | 最终 Relay 统一完成点 | `sum(rate())` |
| `newapi_relay_duration_seconds` | Histogram/秒 | `relay_format,stream,result` | 最终 Relay 统一完成点 | `histogram_quantile` |
| `newapi_relay_inflight` | Gauge/请求 | `relay_format,stream` | Relay controller 入口/完成点 | `sum` |
| `newapi_stream_ttft_seconds` | Histogram/秒 | `relay_format` | 最终成功流的首次响应时间 | `histogram_quantile` |
| `newapi_stream_duration_seconds` | Histogram/秒 | `relay_format,result` | 流式 Relay 完成点 | `histogram_quantile` |
| `newapi_channel_attempts_total` | Counter/attempt | `channel_id,channel_type,stream,result,error_type` | 统一渠道 attempt 完成点 | `sum(rate())` |
| `newapi_channel_retries_total` | Counter/attempt | `channel_id,channel_type,reason` | `retry_index > 0` 的 attempt 开始点 | `sum(rate())` |
| `newapi_channel_inflight` | Gauge/attempt | `channel_id,channel_type` | 统一渠道 attempt 开始/完成点 | `sum` |
| `newapi_channel_duration_seconds` | Histogram/秒 | `channel_id,channel_type,stream,result` | 统一渠道 attempt 完成点 | `histogram_quantile` |
| `newapi_channel_first_byte_seconds` | Histogram/秒 | `channel_id,channel_type` | 共享 HTTP、AWS SDK 与 Gorilla WebSocket Upgrade 请求上下文中的 `httptrace.GetConn` → `GotFirstResponseByte` | bucket 先按 `le,channel_id,channel_type` 聚合再算分位数 |
| `newapi_channel_enabled` | Gauge/布尔 | `channel_id,channel_type` | Master DB collector | `max`，正常仅一份 |

- [x] HTTP、Relay、流式与渠道 attempt/retry/inflight/duration 指标已注册并接入记录点。
- [x] `newapi_channel_duration_seconds` 和 `newapi_channel_first_byte_seconds` 在 `PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true` 时不注册；渠道 attempts/retries/inflight 不受影响。
- [x] `newapi_channel_enabled` 与 `newapi_shared_collector_up{collector="channel_state"}` 已作为 Master-only collector 落地。
- [x] 渠道状态 collector 只查询 `id,type,status`；空渠道表时 `shared_collector_up=1`，查询失败时置 `0`、增加 `collector_errors_total{collector="channel_state"}` 并记录限频日志。

### P0 Runtime、Build 和数据库指标

| 指标 | 类型/单位 | 主要标签 | 记录点 | 集群聚合 |
| --- | --- | --- | --- | --- |
| `go_*` | Go collector | Prometheus 标准标签 | 每实例 registry | 按实例 |
| `process_*` | Process collector | Prometheus 标准标签 | 每实例 registry | 按实例 |
| `newapi_build_info` | Gauge/信息 | `version,commit,build_time` | 启动时注册 | 按实例或 `max` |
| `newapi_collector_errors_total` | Counter/错误 | `collector` | collector 失败路径 | `sum(rate())` |
| `newapi_shared_collector_up` | Gauge/布尔 | `collector` | 仅 Master 导出共享 collector 健康状态 | `max`；用 `absent()` 检测 Master 缺失 |
| `newapi_db_connections` | Gauge/连接 | `database,state` | `sql.DB.Stats()` | 连接数 `sum` |
| `newapi_db_max_open_connections` | Gauge/连接 | `database` | `sql.DB.Stats()` | `sum` |
| `newapi_db_wait_total` | Counter/等待次数 | `database` | `sql.DB.Stats()` | 先按 `cluster,job,instance,database` 计算 5 分钟 `increase` |
| `newapi_db_wait_duration_seconds_total` | Counter/秒 | `database` | `sql.DB.Stats()` | 先按 `cluster,job,instance,database` 计算 5 分钟 `increase`，再除以同实例等待次数 |
| `newapi_db_max_idle_closed_total` | Counter/连接 | `database` | `sql.DB.Stats()` | `sum(rate())` |
| `newapi_db_max_idle_time_closed_total` | Counter/连接 | `database` | `sql.DB.Stats()` | `sum(rate())` |
| `newapi_db_max_lifetime_closed_total` | Counter/连接 | `database` | `sql.DB.Stats()` | `sum(rate())` |

- [x] Go、Process、Build Info、gather 错误和 DB 连接池指标已实现。
- [x] `newapi_shared_collector_up{collector="channel_state"}` 已实现，查询成功（含空表）为 `1`，查询失败为 `0`。
- [x] DB 等待规则保留 `instance/database`，先对单实例计算 5 分钟等待次数、累计时长和平均时长，禁止先跨实例汇总再求平均。

### P1 渠道首字节

- [x] `newapi_channel_first_byte_seconds{channel_id,channel_type}` 已使用 Go 官方 `httptrace` 接入共享 `relay/channel.doRequest` 路径，以 `GetConn` 第一次回调作为 transport 起点，以 `GotFirstResponseByte` 作为响应头首字节可用终点；连接复用时仍会触发 `GetConn`，每次请求使用 `sync.Once` 防御重复首字节回调。
- [x] AWS Bedrock 原生 SDK 的普通 `InvokeModel`、流式 `InvokeModelWithResponseStream` 和 Nova `InvokeModel` 已把相同 trace 注入 SDK 调用 context；AWS Smithy 使用该 context 构建最终 HTTP 请求，因此与共享 HTTP 路径保持同一指标口径。
- [x] Gorilla WebSocket v1.5.0 在 Upgrade 请求中原生调用 `httptrace.GetConn`，并在解析 HTTP 101 响应前调用 `GotFirstResponseByte`；共享 Realtime Dial（OpenAI/AdvancedCustom）、讯飞和火山 TTS 均已改用带请求 context 的 `DialContext`，继续写入同一 Histogram，而不是记录完整握手或第一条应用消息。
- [x] trace 创建时按值冻结 channel ID/type；该指标不读取 `relayInfo.FirstResponseTime`，不把最终 Relay TTFT 冒充为逐渠道 transport 首字节，且上游在首字节前失败时不产生伪造样本。
- [x] 已盘点绕过共享 `doRequest` 的自建 HTTP client：鉴权、文件上传、模型管理、任务轮询和渠道内部后续查询均属于辅助/控制面或非首个主传输请求，不写入本指标，避免一次渠道 attempt 产生含义不同的多个首字节样本。
- [x] 真实本地 WebSocket Upgrade 行为测试已覆盖共享 Realtime、讯飞和火山 TTS；测试锁定成功握手产生一次带冻结 channel ID/type 的样本，且不依赖最终 Relay TTFT 或第一条应用消息。
- [x] TTFT 只在流式最终成功且 `TTFT > 0` 时 Observe，零值 `FirstResponseTime` 不写入。
- [x] P1 异步耗时指标已确保零值、负持续时间和未知时间单位不 Observe，但同一终态的 completion Counter 仍保留。

### Histogram 初始 buckets（单位：秒）

- [x] HTTP、Relay、渠道总耗时：`0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300, 600`。
- [x] TTFT/首字节：`0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60, 120`。
- [x] 流式持续时间：`1, 5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600`。
- [x] 异步任务端到端耗时：`30, 60, 300, 900, 1800, 3600, 7200, 14400`。当前没有可靠的“进入队列时间 → 开始执行时间”统一字段，因此不创建虚假的队列等待 Histogram；积压只使用 `newapi_task_queue_size` 展示当前数量。
- [ ] 上线两周后根据真实分布调整 buckets，调整前评估时间序列变化。

## 六、错误分类（P0）

固定 `error_type` 枚举：

```text
none
client_cancelled
timeout
connection
rate_limit
authentication
insufficient_quota
invalid_request
channel_unavailable
upstream_4xx
upstream_5xx
internal
```

当前实现的映射优先级按表格从上到下匹配，先命中者覆盖后续兜底：

| 优先级 | 现有信号 | Prometheus `error_type` |
| ---: | --- | --- |
| 1 | `errors.Is(err, context.Canceled)` | `client_cancelled` |
| 2 | `context.DeadlineExceeded`、`channel:response_time_exceeded`、HTTP 408/504/524、`net.Error.Timeout()` | `timeout` |
| 3 | `insufficient_user_quota`、`pre_consume_token_quota_failed` | `insufficient_quota` |
| 4 | HTTP 429 | `rate_limit` |
| 5 | `channel:invalid_key`、`access_denied`、HTTP 401/403 | `authentication` |
| 6 | `get_channel_failed`、`channel:no_available_key` | `channel_unavailable` |
| 7 | `invalid_request`、`read_request_body_failed`、`convert_request_failed`、`bad_request_body`、渠道参数/请求头覆盖错误、HTTP 400/422 | `invalid_request` |
| 8 | `do_request_failed`、`read_response_body_failed`、AWS client/invoke 错误、其他 `net.Error` | `connection` |
| 9 | Token 计数、模型计价、JSON 序列化、RelayInfo 生成、数据查询/更新错误 | `internal` |
| 10 | 其他 HTTP 4xx | `upstream_4xx` |
| 11 | 其他 HTTP 5xx | `upstream_5xx` |
| 12 | 仅有 OpenAI/Claude/Gemini/Midjourney/Rerank/通用上游协议类型 | `upstream_5xx` |
| 13 | 仅有 `new_api_error`、未知错误来源或无法分类 | `internal` |

映射只消费稳定的错误码、HTTP 状态、协议类型和 Go error 类型，不读取 `message` 文本。新增错误码时必须先在此表选择现有分类，并在 `pkg/prometheus_metrics/error_type_test.go` 增加表驱动用例；只有出现新的运维处置类别时才能扩展枚举。

- [x] 已新增集中式 `ClassifyError`/`ClassifyNewAPIError`，使用 `GetErrorType()`、`GetErrorCode()`、HTTP 状态码和底层 error 完成固定分类。
- [x] `types.ErrorType` 只作为最后兜底，不覆盖更精确的错误码、HTTP 状态和底层 error；未知类型归 `internal`，不直接透传任意字符串。
- [x] adaptor 不自行生成新的 Prometheus `error_type`。
- [x] 未知错误统一归入 `internal`，完整错误继续写结构化日志。
- [x] 已使用表驱动测试覆盖映射优先级和未知错误降级。

## 七、限流与容量（P1）

| 指标 | 类型/单位 | 标签 | 记录点 | 集群聚合 |
| --- | --- | --- | --- | --- |
| `newapi_rate_limit_rejections_total` | Counter/拒绝 | `scope,reason` | 限流器确定拒绝、写入 429 前 | `sum(rate())` |

- [x] `newapi_rate_limit_rejections_total{scope,reason}` 已接入全局/IP、用户搜索、邮箱验证和模型请求总量/成功量限流拒绝点；只记录真正返回 429 的拒绝，不把 Redis 检查错误记成限流。
- [x] 当前没有独立并发拒绝器，不实现 `newapi_concurrency_rejections_total`；真正增加并发上限与拒绝行为时再设计。
- [x] 当前没有通用请求队列，不实现 `newapi_request_queue_size`/`newapi_request_queue_duration_seconds`。
- [x] 不重复创建 `newapi_active_streams`；每实例活跃流可用 `sum by (instance,relay_format) (newapi_relay_inflight{stream="true"})` 直接得到。
- [x] `scope` 固定为 `global`、`user`、`token`、`channel`；未知值降级为 `global`，不能把具体用户或 Token ID 放入标签。
- [x] `reason` 固定为 `request_count`、`total_request_count`、`successful_request_count`、`concurrency`、`other`；未知文本归 `other`，不能把中间件 mark 或错误文本直接写入标签。

## 八、计费、Token 和实际额度（P1）

Prometheus Counter 不能减少，因此收费和退款必须拆开记录。

现有“使用日志 → 实际额度消耗”与 P1 Prometheus 指标用途不同：

| 能力 | 数据源 | 精度/生命周期 | 权限与用途 |
| --- | --- | --- | --- |
| 使用日志实际额度 | `logs` 中已冻结的 `quota`、`other.group_ratio` | 数据库精确值，可按日志筛选和长期查询 | 仅超级管理员返回，用于业务核对 |
| Prometheus 实际额度 | 最终消费/退款日志落库事件旁的 Counter | 进程重启会重置，由 Prometheus `increase()` 跨实例聚合 | `/metrics` 运维权限，用于趋势和告警，不作为财务对账 |

示例口径保持不变：倍率为 `1` 时消费显示 10 美元，实际额度为 10 美元；倍率为 `0.23` 时消费显示 10 美元，实际额度为 `10 / 0.23 = 43.478...` 美元。应用内部指标仍记录 quota，Grafana 展示时再按现有 `QuotaPerUnit`/货币规则换算。

- [x] `newapi_tokens_total{direction}`：Token 用量，`direction` 固定为 `input`、`output`、`cache`；`input` 表示非缓存输入，精确计算见 8.3 节。
- [x] `newapi_quota_charged_total{billing_source}`：正向扣除的内部 quota。
- [x] `newapi_quota_refunded_total{billing_source}`：退款 quota 的绝对值。
- [x] `newapi_actual_quota_charged_total{billing_source}`：按消费日志 `quota / group_ratio` 计算的正向实际额度。
- [x] `newapi_actual_quota_refunded_total{billing_source}`：实际额度退款的绝对值。
- [x] `newapi_billing_operations_total{operation,billing_source,result}`：预扣、结算和退款操作结果。
- [x] `newapi_billing_failures_total{operation,billing_source,reason}`：固定原因枚举的计费失败。
- [x] `newapi_quota_saturation_total{kind,operation}`：复用现有 `common.QuotaClamp` 事件。
- [x] `newapi_subscription_rejections_total{reason}`：`insufficient_quota`、`no_available_subscription`、`expired`。
- [x] `operation` 固定为 `pre_consume`、`settle`、`refund`、`task_recalculate`；`reason` 复用集中错误枚举或单独定义的固定计费原因，禁止直接使用数据库/上游错误文本。
- [x] 所有额度指标单位统一为内部 quota，Grafana 使用项目现有货币换算规则展示，不在指标标签中加入货币。
- [x] 净额度通过 `charged - refunded` 查询，不维护可减少的 Gauge 或 Counter。
- [x] 实际额度必须复用消费日志现有冻结倍率口径：`actual_quota = quota / group_ratio`；`group_ratio <= 0`、NaN、Inf 或缺失时按 `1` 处理，不读取后来被管理员修改的新倍率。
- [x] 实际额度 Counter 在最终结算 delta 对应的消费/退款日志成功持久化后记录；禁止通过定时扫描历史日志累加。
- [x] 收费和退款按最终持久化事件类型分流，不能仅按 `quota` 正负判断：`LogTypeConsume` 记录 charged，`LogTypeRefund` 记录 refunded；项目退款日志通常保存正数绝对值。写入 Counter 前统一得到非负值，`quota == 0` 不记录，实际额度沿用同一事件类型。
- [x] `billing_source` 在计费会话确定后冻结；空值按现有兼容语义归入 `wallet`，不得因订阅回退过程对同一 delta 同时记录 wallet 和 subscription。
- [x] `quota_saturation_total` 复用 `attachQuotaSaturation` 已有审计事件；预扣前直接拒绝且无日志的 clamp 按 8.4 节单独定义唯一记录点，不能另起饱和判定。
- [x] 预扣只记录 `billing_operations_total`，额度 charged/refunded 以最终持久化结算 delta 为准，避免预扣和结算重复统计实际消耗。
- [x] 实时面板使用 `sum(increase(...[$__range]))` 或 `sum(rate(...))`，不直接把各实例 Counter 当前值当作累计账务；应用重启、扩缩容和 Prometheus 数据缺口必须在面板说明中可见。
- [x] Prometheus 不增加 `user_id`、用户名、Token、订阅 ID、订单号或 `group_ratio` 标签；需要用户级精确查询时继续走现有日志接口。
- [x] 验证普通 Relay、订阅、钱包回退、退款和异步任务二次结算不会重复记录同一个额度事件。

### 8.1 指标记录边界

| 指标 | 类型/单位 | 固定标签 | 唯一记录边界 | 集群聚合 |
| --- | --- | --- | --- | --- |
| `newapi_tokens_total` | Counter/Token | `direction=input|output|cache` | 成功持久化的 consume 日志 | `sum(rate())` 或 `sum(increase())` |
| `newapi_quota_charged_total` | Counter/内部 quota | `billing_source=wallet|subscription|unknown` | 成功持久化的 consume 日志 | `sum(increase())` |
| `newapi_quota_refunded_total` | Counter/内部 quota | `billing_source` | 成功持久化的 refund 日志 | `sum(increase())` |
| `newapi_actual_quota_charged_total` | Counter/内部 quota | `billing_source` | consume 日志的 `quota / frozen_group_ratio` | `sum(increase())` |
| `newapi_actual_quota_refunded_total` | Counter/内部 quota | `billing_source` | refund 日志的 `quota / frozen_group_ratio` | `sum(increase())` |
| `newapi_billing_operations_total` | Counter/操作 | `operation,result,billing_source` | 操作真正完成或最终失败的分支 | `sum(rate())` |
| `newapi_billing_failures_total` | Counter/失败 | `operation,billing_source,reason` | 与 operation `result="error"` 同一分支 | `sum(rate())` |
| `newapi_quota_saturation_total` | Counter/事件 | `kind,operation` | 已有 `QuotaClamp` 审计事件的唯一消费点 | `sum(increase())` |
| `newapi_subscription_rejections_total` | Counter/拒绝 | `reason` | 订阅决策最终返回客户端的分支 | `sum(rate())` |

### 8.2 固定标签与错误映射

| 标签 | 允许值 | 归一化规则 |
| --- | --- | --- |
| `billing_source` | `wallet`、`subscription`、`unknown` | 空值按现有兼容语义归 `wallet`；仅真正无法判断时归 `unknown` |
| `operation` | `pre_consume`、`settle`、`refund`、`task_recalculate`、`other` | 业务入口必须使用前四项，未知值防御性归 `other` |
| `result` | `success`、`error` | 只表示该操作最终是否完成 |
| 计费 `reason` | `invalid_quota`、`quota_saturation`、`token_quota`、`user_quota`、`subscription_quota`、`database`、`other` | 按稳定 error/code/type 映射，不匹配 message 文本 |
| `kind` | `overflow`、`underflow`、`nan` | 直接来自 `common.QuotaClamp.Kind`，未知值不记录 |
| 订阅 `reason` | `insufficient_quota`、`no_available_subscription`、`expired`、`other` | 在订阅最终决策分支选择，不从客户端错误文本反解析 |

订阅拒绝口径：用户从未持有订阅、或当前分组没有可用订阅时为 `no_available_subscription`；存在已到期订阅但没有活动订阅时为 `expired`；存在适用当前分组的活动订阅但额度不足时为 `insufficient_quota`。客户端现有文案可保持不变，指标分类不得依赖文案。

### 8.3 计算和防溢出规则

- [x] `cache = max(cache_tokens, 0) + max(cache_write_tokens, 0)`，`input = max(max(prompt_tokens, 0) - cache, 0)`，`output = max(completion_tokens, 0)`；先分别归一化，再用无符号或饱和加法，禁止用 `prompt-cache` 的裸 `int` 减法引入溢出。
- [x] Token 只在 consume 日志记录；refund 不反向减 Token Counter，没有 Token 的任务差额日志不伪造 Token 值。
- [x] 实际额度只对正的有限 quota 计算；计算结果为 NaN、Inf 或非正数时回退到原 quota，不向 Prometheus Counter 传入非法值。

### 8.4 事件去重与持久化边界

| 场景 | 额度 Counter | 生命周期 Counter | 去重要求 |
| --- | --- | --- | --- |
| 普通 Relay/初始任务消费日志成功 | consume 记 charged/actual charged，有 Token 则记 Token | `settle` 只在实际 Settle 结果处记录 | 日志与结算指标各自只记一次 |
| 任务二次结算 delta > 0 | task consume 日志记 charged/actual charged | `task_recalculate{result="success"}` | 只在额度更新和日志持久化完成的路径记录 |
| 任务二次结算 delta < 0 | task refund 日志记 refunded/actual refunded | `task_recalculate{result="success"}` | refund Counter 使用 delta 绝对值 |
| 请求失败后退还预扣 | 不记 charged/refunded，因为预扣未进入最终额度 Counter | 异步退款真正完成后记 `refund` | 幂等保护败出的路径不记录 |
| 消费日志开关关闭 | consume 额度/Token 不记；refund 日志仍按现有行为记录 | 已完成的计费操作仍按操作边界记录 | Dashboard/部署文档必须说明 charged/Token Counter 会不完整 |
| `createLog` 失败 | 本次对应的额度、Token 和日志型饱和 Counter 不记 | 计费 operation 按其自身结果记录 | 不得在日志失败分支先行增加用量 Counter |
| 幂等 Settle/Refund/Task CAS 未获胜 | 不记 | 不记 | 不把 no-op 当成第二次 success |

- [x] `model.RecordConsumeLog` 和 `model.RecordTaskBillingLog` 只在 `createLog(log) == nil` 后记录日志型 Counter；创建失败、消费日志关闭或 Prometheus 禁用时不得产生伪造用量。
- [x] 异步 `BillingSession.Refund` 只在所有必需的资金源/额外预留/Token 退还都成功后记 `success`；任一必需步骤失败则记一次 `error` 及固定 reason。
- [x] `quota_saturation_total` 不重新计算饱和：有计费日志的路径从 `other.admin_info.quota_saturation` 记录；预扣前因同一 clamp 拒绝的路径仅在 `PreConsumeBilling` 拒绝点记录，两条路径不得双记。
- [x] 行为测试覆盖钱包、订阅、订阅回退钱包、预扣失败、普通失败退款、任务正/负差额、日志失败/关闭、幂等重试和 quota saturation 恰好一次。

## 九、异步任务（P1）

### 9.1 指标和固定枚举

| 指标 | 标签 | 记录点 | 集群聚合 |
| --- | --- | --- | --- |
| `newapi_task_submissions_total` | `platform,result` | controller 最终提交边界；上游成功但本地任务插入失败必须为 failure | `sum(rate())` |
| `newapi_task_completions_total` | `platform,result` | 任务首次 CAS 进入最终 success/failure 状态 | `sum(rate())` |
| `newapi_task_duration_seconds` | `platform,result` | 与 completion 同一个 CAS 获胜分支 | bucket 先按 `le,platform,result` 求和再算分位数 |
| `newapi_task_poll_total` | `platform,result` | 每次实际上游轮询返回后 | `sum(rate())` |
| `newapi_task_queue_size` | `platform,state=waiting|running|unknown` | Master DB collector 合并 `tasks` 与 `midjourneys` 的当前状态 | 不求和，防御性查询用 `max` |

- [x] `platform` 口径已冻结为 `midjourney`、`suno`、`video`、`other`：`constant.TaskPlatformMidjourney` 归 `midjourney`，`TaskPlatformSuno` 归 `suno`，已注册的通用视频 Task adaptor 归 `video`，未知/动态值归 `other`。未来真正增加图片或音频异步任务后才扩展枚举。
- [x] 结果枚举已冻结：提交/完成 `result` 固定为 `success|failure`；poll `result` 固定为 `success|error`。这两项只表示口径已确定，不代表真实业务接入已经完成。
- [x] 当前没有向客户投递异步任务回调的业务链路，Midjourney notify 是入站通知，不是回调投递失败。因此 D4 不实现 `newapi_task_callback_failures_total`；未来增加真实回调投递后再定义固定 reason。

### 9.2 生命周期与去重

- [x] 提交成功必须同时满足上游提交成功和本地 `task.Insert()`/`Midjourney.Insert()` 成功；上游失败、本地插入失败或 controller panic 均只记一次 failure。通用 Task 内部重试不重复计数，Midjourney Notify/Fetch/ImageSeed 不计 submission。
- [x] completion 同时覆盖 `Task.UpdateWithStatus(fromStatus)` 和 `Midjourney.UpdateWithStatus(fromStatus)`，只在 CAS 获胜且首次进入 `SUCCESS|FAILURE` 时记录；重复轮询、超时 sweep 与正常轮询竞争中 CAS 失败的实例不记录。
- [x] duration 按数据源换算单位：`tasks.submit_time/finish_time` 使用秒，`midjourneys.submit_time/finish_time` 使用毫秒并除以 `1000`。`FinishTime <= 0` 时在 CAS 前按同单位补当前时间并一起持久化；时间缺失、为负或数据源无法归属时不 Observe，但 completion Counter 仍记录。
- [x] poll 表示实际上游查询结果，不表示本地状态持久化结果：已覆盖 Suno、Video、Midjourney 批量轮询和 Gemini/Vertex 实时查询；空队列、未支持 platform、取本地任务失败或其他未发出请求的早退不记录，Fetch/HTTP/read/parse/business error 记录 `error`，上游响应成功解析后记录 `success`。后续本地 CAS/结算失败由 completion、计费指标和日志体现，不能回写或重复修改本次 poll 样本。
- [x] queue collector 只在 `common.IsMasterNode` 注册，分别对 `tasks` 和 `midjourneys` 执行一次按 platform/status 或 status 分组的 GORM 查询后合并，查询使用通用 GORM 语法兼容 SQLite/MySQL/PostgreSQL。`tasks` 的 `NOT_START|SUBMITTED|QUEUED` 归 `waiting`、`IN_PROGRESS` 归 `running`；`midjourneys` 的空状态/`SUBMITTED|QUEUED` 归 `waiting`、`IN_PROGRESS` 归 `running`；其他未完成状态归 `unknown`。空表导出已知 platform/state 的 `0`，任一查询失败导出 `newapi_shared_collector_up{collector="task_queue"} 0`、增加 collector error 并限频记日志。
- [x] queue collector 任一分组查询失败时不导出另一张表的部分队列 Gauge，也不沿用上次 scrape 的旧值；本次只导出 `shared_collector_up=0`。这样 Dashboard 的 No data/collector down 不会被误解释为完整队列快照。
- [x] Task Dashboard 已展示提交量、完成率、轮询错误、P50/P95 任务耗时、Master-only 积压和 collector 健康状态；积压告警已使用 `for: 15m`，但 `newapi:task_queue_total:platform > 100` 仅为可回归测试的保守候选阈值，生产基线校准仍未完成。
- [x] 行为测试覆盖提交成功/失败/本地插入失败、轮询成功/错误、成功/失败终态、超时 sweep、重复轮询/通知、Notify 与轮询竞争、负时长保护、Master/Slave、空表和查询失败。

completion 与计费生命周期必须保持解耦：终态 CAS 获胜就记录一次 completion；之后退款或二次结算失败不能把 completion 改成未完成，也不能重复记录 completion。计费结果继续由第八节的 operation、额度 Counter 和结构化日志表达。

### 9.3 D4 精确接入点

| 生命周期 | 唯一接入点 | 成功条件 | 明确不记录的情况 |
| --- | --- | --- | --- |
| 通用 Task 提交 | `controller/relay.go` 的 `RelayTask` 最终退出点 | 上游提交成功且 `task.Insert()` 成功 | 内部重试过程、只拿到上游成功但本地插入失败 |
| Midjourney 提交 | `controller/relay.go` 的 `RelayMidjourney`，仅 Submit 与 SwapFace 模式 | 复用 `metricsOutcome.Success`，其中已经包含业务码和本地持久化结果 | Notify、Fetch、ImageSeed 等非提交路径 |
| Suno 轮询 | `service/task_polling.go` 的 `updateSunoTasks`，每次 `adaptor.FetchTask` | HTTP 200、响应体读取/解析成功且业务响应成功 | 空任务列表、adaptor 缺失、取渠道失败等未发出上游请求的早退 |
| Video 轮询 | `service/task_polling.go` 的 `updateVideoSingleTask`，每次 `adaptor.FetchTask` | 响应体读取成功且能解析成项目响应或 adaptor 任务结果 | 找不到本地任务、未知 platform 等未发出上游请求的早退 |
| Midjourney 轮询 | `controller/midjourney.go` 的 `runMidjourneyTaskUpdateOnce`，每次 `GetHttpClient().Do` | HTTP 200、body 读取成功且 JSON 解析成功 | 空队列、请求构造失败或取渠道失败 |
| 实时任务查询 | `relay/relay_task.go` 的 `tryRealtimeFetch`，仅真正发出上游状态查询时 | 上游响应被完整读取并成功解析 | 不支持实时查询或复用本地状态直接返回 |
| Task 终态 | `sweepTimedOutTasks`、`updateSunoTasks`、`updateVideoSingleTask` 的 `Task.UpdateWithStatus` 获胜分支 | 首次由非终态进入 `SUCCESS` 或 `FAILURE` | CAS 失败、重复终态、仅更新进度 |
| Midjourney 终态 | 轮询和 Notify 的 `Midjourney.UpdateWithStatus` 获胜分支 | 首次由非终态进入 `SUCCESS` 或 `FAILURE` | 普通 `Update()`、CAS 失败、重复终态 |

- [x] `RecordTaskSubmission` 已由 controller 级 defer 保证 panic 记一次 failure；正常路径使用最终持久化结果，未在每次渠道重试中调用。
- [x] `RecordTaskPoll` 已紧贴实际上游调用，并通过单一 defer/结果变量覆盖 Fetch、HTTP、读 body、解析和业务失败，避免错误分支漏记或重复记。
- [x] `RecordTaskCompletion` 已在 CAS 获胜后调用；终态时间为零时先按对应数据表的时间单位补当前时间并由 CAS 一起持久化，不只在内存里补时间。
- [x] Midjourney Notify 已从普通 `Update()` 改为基于旧状态的 `UpdateWithStatus(oldStatus)`；重复通知保持幂等，Notify 与已经取到旧快照的轮询竞争时只有一个 CAS 获胜者记录 completion/duration。
- [x] 队列数据源已注入 `main.go`，每次采集恰好执行两次 GORM 分组查询：一次查询未终态 `tasks` 的 `platform,status,count`，一次查询未完成 `midjourneys` 的 `status,count`；没有按每个平台或每种状态循环查库。

#### 9.3.1 首次终态写入矩阵

| 数据源 | 旧状态要求 | 新状态 | 缺失终态时间 | 指标时间单位 | CAS 失败行为 |
| --- | --- | --- | --- | --- | --- |
| Task 超时 sweep | 任意非终态 | `FAILURE` | CAS 前填当前 Unix 秒并一起持久化 | `seconds` | 不记录 completion/duration，不执行第二次退款 |
| Suno 正常轮询 | 任意非终态 | `SUCCESS` 或 `FAILURE` | CAS 前填当前 Unix 秒并一起持久化 | `seconds` | 不记录 completion/duration，不重复结算/退款 |
| Video 正常轮询 | 任意非终态 | `SUCCESS` 或 `FAILURE` | CAS 前填当前 Unix 秒并一起持久化 | `seconds` | 不记录 completion/duration，不重复结算/退款 |
| Gemini/Vertex 实时查询 | 任意非终态 | `SUCCESS` 或 `FAILURE` | CAS 前填当前 Unix 秒并一起持久化 | `seconds` | 返回现有查询结果，不记录 completion/duration |
| Midjourney 批量轮询 | 任意非终态 | `SUCCESS` 或 `FAILURE` | CAS 前填当前 Unix 毫秒并一起持久化 | `milliseconds` | 不记录 completion/duration，不重复结算/退款 |
| Midjourney Notify | 任意非终态 | `SUCCESS` 或 `FAILURE` | CAS 前填当前 Unix 毫秒并一起持久化 | `milliseconds` | 视为另一实例已处理的幂等成功，不记录 completion/duration；数据库错误沿用现有更新失败响应 |

只有从非终态进入 `SUCCESS|FAILURE` 才属于 completion。`NOT_START → SUBMITTED/QUEUED/IN_PROGRESS`、进度更新、重复写入相同终态和仅补充图片/视频 URL 都不能记录 completion。

### 9.4 D4 验收矩阵

| 场景 | 必须断言 |
| --- | --- |
| 提交成功、上游失败、本地插入失败、panic | submission 每个 controller 调用恰好增加一次；失败路径不残留 success 样本 |
| poll 成功、HTTP 错误、读 body 错误、解析错误 | 每次实际上游调用只增加一次 `success` 或 `error`；未调用上游时不增加 |
| 成功/失败终态、重复轮询、CAS 竞争 | 只有 CAS 获胜者增加 completion；CAS 失败者不增加 completion/duration |
| timeout sweep 与正常轮询竞争 | 最终只有一个 completion；退款/结算原有幂等行为保持不变 |
| 秒/毫秒时间戳、零值和负时长 | Task 秒值直接换算，Midjourney 毫秒值除以 1000；非法时长只跳过 Histogram，不丢 completion |
| completion 后计费失败 | completion 保持恰好一次；计费失败只进入计费指标和日志，不回滚或重复 completion |
| Master/Slave、空表、查询失败 | Master 导出 4×3 零填充 Gauge；Slave 不注册/不查询；失败时只导出 collector down、增加 error 并限频日志 |
| Dashboard 与规则 | 查询保留 `cluster/job/platform` 边界；Histogram 按 `le,platform,result` 聚合；No data 与 0 积压含义可区分 |

## 十、数据库与 Redis（P0/P1）

### 数据库（P0）

- [x] 从 GORM 的 `DB.DB().Stats()` 暴露连接池指标，不侵入业务查询代码。
- [x] 使用 `database="main|log"` 区分主库和日志库。
- [x] 当 `LOG_DB == DB` 时只能导出一份连接池状态，避免重复。
- [x] 连接池指标包括 open、in-use、idle、max-open、wait count、wait duration、max-idle-closed、max-idle-time-closed、max-lifetime-closed。
- [x] `sql.DB.Stats()` 的累计字段使用 Prometheus CounterValue 导出，当前连接数和上限使用 GaugeValue；不能把累计 wait count 当 Gauge。
- [x] `DB.DB()` 或 `LOG_DB.DB()` 取连接池失败时终止启用监控的启动流程；scrape gather 错误增加 `newapi_collector_errors_total{collector="gather"}` 并继续输出可用指标。
- [x] gather 错误通过现有日志系统输出。
- [x] 渠道状态查询失败时每次 scrape 都增加 collector 错误计数，但同类日志最多每分钟输出一次，避免数据库故障时刷屏。
- [x] 连接池聚合口径已确定：数量按实例展示，集群容量可 `sum`，利用率先按实例计算再取 `max`。
- [x] 无上限口径已确定：`max_open_connections == 0` 表示未设置上限，此时不计算连接池利用率，也不触发利用率告警。
- [x] Recording Rules 和告警已排除 `max_open_connections == 0`，并通过固定输入规则测试。
- [x] Grafana DB 利用率面板读取 `newapi:db_pool_utilization:instance`，因此自动隐藏 `max_open_connections == 0` 的无上限连接池；连接数面板仍保留。

### Redis（P1）

| 指标 | 类型/单位 | 标签 | 记录点 | 集群聚合 |
| --- | --- | --- | --- | --- |
| `newapi_redis_enabled` | Gauge/布尔 | 无 | Runtime 创建时按本实例 Redis 客户端状态设置 | 按实例展示；排查时使用 `min`，不能 `sum` 后解释为布尔值 |
| `newapi_redis_operations_total` | Counter/操作 | `command,operation_type,result` | go-redis Hook 的单命令及 pipeline 完成点 | `sum(rate())`，排障时保留 `instance` |
| `newapi_redis_operation_duration_seconds` | Histogram/秒 | `command,operation_type,result` | 单命令和 aggregate pipeline 完成点 | bucket 先 `sum by (le,...)` 再计算分位数 |
| `newapi_cache_lookups_total` | Counter/读取 | `backend,result` | `common` Redis 缓存 helper 与 `pkg/cachex.HybridCache.Get` | `sum(rate())`；命中率按 backend 分别计算 |
| `newapi_redis_rate_limit_failures_total` | Counter/失败 | `limiter` | Redis 限流检查返回错误时 | `sum(increase())` 或 `sum(rate())` |
| `newapi_redis_degradations_total` | Counter/降级 | `reason` | Redis 错误后确实进入业务 fallback 时 | `sum(increase())` 或 `sum(rate())` |

- [x] 通过 go-redis hook 记录操作量、失败结果和耗时。
- [x] hook 同时覆盖单命令和 pipeline；pipeline 既记录一次 aggregate pipeline 调用，也按固定命令名记录内部命令，二者使用不同 `operation_type`，避免混在一个计数口径中；内部命令不重复 Observe 耗时。
- [x] `operation_type` 固定为 `command`、`pipeline`、`pipeline_command`；`result` 固定为 `success`、`miss`、`error`。
- [x] 应用缓存读取单独记录 hit/miss/error；`backend` 固定为 `redis`、`memory`、`other`，`result` 固定为 `hit`、`miss`、`error`。不能把普通 Redis 写操作解释成缓存命中。
- [x] 已记录固定窗口、模型成功请求数和模型总请求数 Redis 限流检查失败；只有邮箱验证 Redis 错误后实际进入内存 fallback 时记录 `rate_limit_fallback`，直接返回 500 的路径不伪造降级。
- [x] Redis command 只使用固定命令名作为标签，未知命令归 `other`；禁止把 Key、限流 Redis Key 或错误文本写入标签。
- [x] Redis 未启用时不产生伪造的操作成功序列，只暴露 `newapi_redis_enabled 0`；启用且客户端存在时为 `1` 并安装 Hook。
- [x] Redis 验收已覆盖单命令、pipeline、禁用、deadline、连接错误、缓存 hit/miss/error、限流失败和降级，测试同时断言指标输出不包含私有 Key。

## 十一、Grafana、Recording Rules 和告警（P0/P1）

### Grafana 面板

- [x] 四层核心 dashboard JSON：主机总览、程序总览、中间件总览、渠道总览；全部使用中文标题、行分组、面板和说明。
- [x] 渠道总览：1 分钟 attempt/失败/重试 RPM、成功率、超时率；5 分钟 P90/P95、TTFT P95、上游首字节 P95、上游缓存率和 Token 吞吐；同时保留并发、启用状态、错误与重试诊断。
- [x] 错误分析静态面板：固定错误类型、HTTP `status_class` 和渠道失败排行。
- [x] 计费额度：Token、收费、退款、净额度、实际额度、生命周期、订阅拒绝和饱和事件。
- [x] 异步任务：Task Dashboard 已展示提交 RPM、完成成功率、poll 错误率、结果趋势、P50/P95 处理时长、按状态积压和 Master collector 健康状态。
- [x] P0 基础设施面板：Node Exporter 主机 CPU/内存/磁盘，Go Runtime/进程 CPU，PostgreSQL/MySQL/Redis 中间件和应用数据库连接池。
- [x] P1 限流面板：按固定 `scope/reason` 展示限流拒绝 RPM，图例使用文本区分，No data 明确表示当前范围内没有拒绝。
- [x] P1 Redis 面板：按实例展示启用状态，并提供操作 RPM、P95 耗时、缓存命中率和错误/限流失败/降级趋势；未产生操作或异常时 No data 不等同于 Redis 未启用。
- [x] P1 DB 等待面板：System Overview 展示单实例/单数据库的 5 分钟等待次数和平均等待时长；`0` 表示窗口内无等待，No data 表示原始 DB 指标缺失。
- [x] P1 Relay 延迟阈值线：System Overview 在原有 P50/P95/P99 上叠加按 `cluster/job/relay_format` 配置的 P95/P99 虚线；默认阈值文件为空时显示 No data，不解释为 0 秒。
- [x] 6 个 dashboard 均提供 `cluster` 与各自所需的 `instance`、`device`、`database`、`relay_format`、`channel_id`、`billing_source` 或 `platform` 变量。Task Dashboard 的 Master-only 队列面板故意忽略 `instance` 变量。
- [x] Grafana 12.1 临时容器已真实验证 6 个 dashboard provisioning，4 个核心页进入 `new-api 监控`，计费/任务进入 `new-api 扩展监控`；生产环境仍需复现加载。

### Recording Rules

- [x] 已预计算服务成功率、事件量、渠道成功 RPM、尝试 RPM、失败比例、重试比例和 P95/P99。
- [x] 基于时间窗口的 Recording Rule 名称包含窗口，例如 `newapi:relay_success_ratio:5m`；瞬时 DB 利用率使用 `:instance` 后缀。
- [x] 比率查询使用 `clamp_min` 避免除零；比例告警另加独立事件量门槛，不把 `clamp_min` 当作低流量保护。
- [x] 名称严格区分 `rpm`、每秒 `rate` 和无单位 `ratio`；重试数与 attempt 数之比命名为 `retry_ratio`。
- [x] D3-D10 累计形成 35 条基础规则；D11 新增 11 条渠道实时/性能/缓存/Token 聚合规则，当前基础 Recording Rules 为 46 条。

P0 已提供以下规则。当前默认配置同时保留 `cluster` 和 `job` 作为部署边界；完整可执行版本以 `deploy/monitoring/recording-rules.yml` 为准：

```yaml
groups:
  - name: newapi-traffic
    rules:
      - record: newapi:relay_request_rate:5m
        expr: sum by (cluster, job) (rate(newapi_relay_requests_total[5m]))
      - record: newapi:relay_request_increase:5m
        expr: sum by (cluster, job) (increase(newapi_relay_requests_total[5m]))
      - record: newapi:relay_request_increase_by_format:5m
        expr: sum by (cluster, job, relay_format) (increase(newapi_relay_requests_total[5m]))
      - record: newapi:relay_inflight_by_format
        expr: sum by (cluster, job, relay_format) (newapi_relay_inflight)
      - record: newapi:relay_success_ratio:5m
        expr: sum by (cluster, job) (rate(newapi_relay_requests_total{result="success"}[5m])) / clamp_min(sum by (cluster, job) (rate(newapi_relay_requests_total[5m])), 0.000001)
      - record: newapi:relay_error_increase:5m
        expr: sum by (cluster, job, error_type) (increase(newapi_relay_requests_total{result="failure"}[5m]))
      - record: newapi:relay_error_ratio:5m
        expr: sum by (cluster, job, error_type) (rate(newapi_relay_requests_total{result="failure"}[5m])) / on (cluster, job) group_left clamp_min(sum by (cluster, job) (rate(newapi_relay_requests_total[5m])), 0.000001)
      - record: newapi:channel_success_rpm:5m
        expr: sum by (cluster, job, channel_id) (rate(newapi_channel_attempts_total{result="success"}[5m])) * 60
      - record: newapi:channel_attempt_rpm:5m
        expr: sum by (cluster, job, channel_id) (rate(newapi_channel_attempts_total[5m])) * 60
      - record: newapi:channel_attempt_increase:5m
        expr: sum by (cluster, job, channel_id) (increase(newapi_channel_attempts_total[5m]))
      - record: newapi:channel_success_increase:5m
        expr: sum by (cluster, job, channel_id) (increase(newapi_channel_attempts_total{result="success"}[5m]))
      - record: newapi:channel_failure_ratio:5m
        expr: sum by (cluster, job, channel_id) (rate(newapi_channel_attempts_total{result="failure"}[5m])) / clamp_min(sum by (cluster, job, channel_id) (rate(newapi_channel_attempts_total[5m])), 0.000001)
      - record: newapi:channel_retry_ratio:5m
        expr: sum by (cluster, job, channel_id) (rate(newapi_channel_retries_total[5m])) / clamp_min(sum by (cluster, job, channel_id) (rate(newapi_channel_attempts_total[5m])), 0.000001)
      - record: newapi:relay_duration_seconds:p95_5m
        expr: histogram_quantile(0.95, sum by (cluster, job, relay_format, le) (rate(newapi_relay_duration_seconds_bucket[5m])))
      - record: newapi:relay_duration_seconds:p99_5m
        expr: histogram_quantile(0.99, sum by (cluster, job, relay_format, le) (rate(newapi_relay_duration_seconds_bucket[5m])))
      - record: newapi:db_pool_utilization:instance
        expr: (max by (cluster, job, instance, database) (newapi_db_connections{state="in_use"}) / max by (cluster, job, instance, database) (newapi_db_max_open_connections)) and on (cluster, job, instance, database) (max by (cluster, job, instance, database) (newapi_db_max_open_connections) > 0)
      - record: newapi:db_wait_increase:5m
        expr: sum by (cluster, job, instance, database) (increase(newapi_db_wait_total[5m]))
      - record: newapi:db_wait_duration_seconds_increase:5m
        expr: sum by (cluster, job, instance, database) (increase(newapi_db_wait_duration_seconds_total[5m]))
      - record: newapi:db_wait_average_seconds:5m
        expr: newapi:db_wait_duration_seconds_increase:5m / clamp_min(newapi:db_wait_increase:5m, 1)
      - record: newapi:go_goroutines_growth_per_second:30m
        expr: max by (cluster, job, instance) (deriv(go_goroutines[30m]))
      - record: newapi:go_heap_alloc_bytes_growth_per_second:30m
        expr: max by (cluster, job, instance) (deriv(go_memstats_heap_alloc_bytes[30m]))
```

- [x] 已使用 `promtool test rules` 为成功率、低流量门槛、Histogram 聚合、按格式请求量、按格式 inflight、Relay 延迟阈值缺失/低流量/标签隔离、Relay 并发阈值持续时长/等值边界/缺失/标签隔离/短峰值、DB 无上限连接池、DB 等待次数/时长/平均值和 absent 告警增加固定输入/期望测试。

### Prometheus 告警规则与 Alertmanager

本节区分两层：`deploy/monitoring/alert-rules.yml` 负责告警表达式、持续时间和严重级别；Alertmanager 负责通知接收器、恢复通知、静默和 warning/critical 抑制。两层均已完成静态配置和本地运行联调；生产接收端、阈值与值班路由仍需在发布验收中确认。

- [x] 服务成功率持续低于阈值，且窗口内请求量达到最低门槛。
- [x] 单渠道错误率持续升高，且 attempt 数量达到最低门槛。
- [x] 单渠道窗口内持续失败：使用“窗口内 attempt 达到门槛且成功数为 0”的可观测定义，不声称还原严格的请求序列。
- [x] P95/P99 延迟持续过高的告警基础设施已完成：阈值按 `cluster/job/relay_format/quantile` 配置，P95/P99 分别带 50/100 个最终请求门槛并持续 10 分钟；默认空阈值文件使告警休眠。
- [x] 重试比例持续过高。
- [x] Relay 并发持续异常告警基础设施已完成：按 `cluster/job/relay_format` 汇总实际 inflight，并由默认空规则文件提供独立 warning/critical 绝对阈值；本批不引入“接近固定上限”的比例告警或请求拒绝器。
- [ ] 生产 Relay 并发阈值已根据真实的按格式基线完成校准并写入 `relay-concurrency-thresholds.yml`；仓库默认文件有意保持为空。
- [x] 429、5xx 和超时数量异常：Relay `rate_limit`、`upstream_5xx`、`timeout` 使用比例、总请求量和错误事件量三重门槛；本地限流拒绝使用独立 Counter 门槛。
- [x] 数据库连接池利用率过高。
- [x] 数据库连接等待持续异常：单实例 5 分钟等待次数 `>= 20` 且平均等待时长 `> 0.1s`，持续 10 分钟触发 warning；次数与平均耗时双门槛避免大量瞬时等待或低流量单次慢等待误报。
- [x] Redis 持续命令错误或降级次数异常：5 分钟窗口事件数 `>= 5` 且持续 5 分钟触发 warning；合法的 `newapi_redis_enabled 0` 不直接告警。
- [x] Goroutine、Go heap 持续增长：使用 `deriv(...[30m])` 计算单实例每秒增长率，并同时要求绝对量门槛、实例已运行 30 分钟和持续 15 分钟，避免启动期与低基数正常波动误报。
- [x] 计费失败和额度饱和已有规则。
- [x] 异步任务已提供队列分组/汇总 Recording Rules、积压持续告警、collector down 和 absent 告警，并通过固定输入边界测试；积压 `> 100` 且持续 `15m` 是候选阈值，生产阈值待批次 E 校准。
- [x] `up == 0` 或 Master-only collector `absent()`。
- [x] Prometheus 告警规则已配置 `for` 持续时间、统一 `severity` 分级和可操作 annotation。
- [x] Alertmanager 示例已配置 webhook `send_resolved: true`、静默操作说明，以及服务、DB、同格式 Relay P99 critical→P95 warning、同格式 Relay inflight critical→warning 抑制规则。
- [x] 测试环境已验证 Alertmanager firing/resolved Webhook、warning/critical 抑制和 silence；真实 Prometheus `NewAPIInstanceDown` 规则也已进入 firing 并在实例恢复后 resolved。

初始告警必须采用“比例 + 最低事件量”双门槛，避免低流量单次失败触发：

- [x] 服务 5 分钟成功率 `< 95%` 且 5 分钟最终请求数 `>= 100`，持续 10 分钟触发 warning；`< 80%` 且请求数达到同一门槛，持续 5 分钟触发 critical。
- [x] 单渠道 5 分钟失败率 `> 20%` 且 attempt 数 `>= 50`，持续 10 分钟触发 warning。
- [x] 单渠道 5 分钟重试比例 `> 20%` 且 attempt 数 `>= 50`，持续 10 分钟触发 warning。
- [x] 单渠道 5 分钟 attempt 数 `>= 20` 且成功数为 `0`，持续 5 分钟触发 critical；规则名称使用“no_success”，不写成无法严格证明的“consecutive failures”。
- [x] Relay `rate_limit` 5 分钟比例 `> 10%`、最终请求数 `>= 100` 且错误数 `>= 10`，持续 10 分钟触发 warning；本地限流拒绝 5 分钟事件数 `>= 20` 且持续 10 分钟使用独立 warning。
- [x] Relay `upstream_5xx` 5 分钟比例 `> 5%`、最终请求数 `>= 100` 且错误数 `>= 10`，持续 10 分钟触发 warning。
- [x] Relay `timeout` 5 分钟比例 `> 5%`、最终请求数 `>= 50` 且错误数 `>= 5`，持续 10 分钟触发 warning。
- [x] Relay P95 高于已配置阈值且同格式 5 分钟最终请求数 `>= 50`，持续 10 分钟触发 warning；P99 高于已配置阈值且请求数 `>= 100`，持续 10 分钟触发 critical。
- [x] DB 连接池单实例利用率 `> 80%` 持续 10 分钟触发 warning，`> 95%` 持续 5 分钟触发 critical。
- [x] DB 等待 warning 保留 `cluster/job/instance/database`；5 分钟等待次数 `>= 20` 且平均等待 `> 0.1s`，持续 10 分钟触发。阈值为初始候选值，需在批次 E 按真实连接池和 SQL 延迟分布校准。
- [x] Goroutine warning：单实例当前值 `>= 500`、30 分钟增长率 `> 0.05/s`、进程运行 `>= 30m`，并持续 `15m`。
- [x] Go heap warning：单实例当前分配 `>= 512 MiB`、30 分钟增长率 `> 128 KiB/s`、进程运行 `>= 30m`，并持续 `15m`。两组 Runtime 阈值均为候选值，需在批次 E 结合实例规格、请求量与 profile 校准。
- [x] `up == 0` 持续 2 分钟触发 critical；Master-only 指标 `absent()` 持续 5 分钟触发 warning。
- [x] `absent()` 规则已带固定 `job="new-api"`、`cluster="default"` 和 `collector="channel_state"` 选择器，不会跨所有部署做全局 absent。
- [x] Alertmanager 已分别按 `cluster/job`、`cluster/job/instance/database` 和 `cluster/job/relay_format` 配置服务、DB 与 Relay 延迟 critical 抑制对应 warning。
- [x] 延迟阈值文件默认不产生任何阈值序列；严格校验只接受允许的 `relay_format`、`p95|p99`、有限正数 `vector(<秒>)` 和唯一标签键，避免用统一秒数误报不同格式。
- [ ] 生产 P95/P99 阈值已根据真实的按格式基线完成校准并写入 `relay-latency-thresholds.yml`；仓库默认文件有意保持为空。

## 十二、部署产物（P0）

- [x] 新增 `docker-compose.monitoring.yml`，独立提供 Prometheus、Grafana 和 Alertmanager，不修改默认业务部署。
- [x] 新增 `deploy/monitoring/prometheus.yml`。
- [x] 新增 `deploy/monitoring/recording-rules.yml` 与 `recording-rules.test.yml`。
- [x] 新增 `deploy/monitoring/alert-rules.yml` 与 `alert-rules.test.yml`。
- [x] 新增 `deploy/monitoring/alertmanager.yml.example`，Webhook URL 通过 secret 文件读取，示例中不包含真实密钥。
- [x] 新增 `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml` 和 dashboard provisioning。
- [x] 新增 `deploy/monitoring/grafana/dashboards/system-overview.json` 与 `channel-overview.json`；P1 面板在对应指标实现后再增加，不提交长期空白面板。
- [x] 示例默认 `scrape_interval: 15s`、`scrape_timeout: 10s`、数据保留 15 天，并允许通过 Compose 端口/secret 路径和配置文件调整部署参数。
- [x] 独立监控栈不启动应用；部署文档要求每个应用实例使用稳定唯一的 `NODE_NAME`，Prometheus target 同时显式设置稳定唯一的 `instance`，并说明二者不会自动映射。
- [x] 部署文档已补充单实例、多实例和多集群抓取示例。
- [x] Prometheus 使用 `authorization.credentials_file` 读取 Bearer Token；示例配置不把 Token 写入仓库、命令行参数或 dashboard JSON。
- [x] Grafana 管理员密码和 Alertmanager 通知凭据通过 Compose secret 文件注入；secret 目录忽略非 `.example` 文件，降低误提交风险。
- [x] 容器健康检查分别验证 Prometheus `/-/ready`、Grafana `/api/health`、Alertmanager `/-/ready`；健康检查不依赖外部通知服务。
- [x] 当前项目没有 Kubernetes/Helm 部署目录，P0 不新增 Kubernetes 模板；需要时单独规划。

## 十三、测试与发布验收

测试必须保护业务口径，不为每个 Counter/Gauge 机械增加“加一”测试。

- [x] 验证普通 Relay 非法请求、任务 Relay 早退失败和三类 controller panic 的最终失败只记录一次。
- [x] 已补普通非流式 Relay 与任务 Relay 成功路径的 controller 集成测试，验证最终成功和渠道 attempt 各只记录一次，inflight 归零。
- [x] 已验证 controller 真实重试循环中第一次渠道 500、第二次渠道成功：最终 Relay 只记录一次，attempt 为 2、retry 为 1，retry reason 为第一次失败的 `upstream_5xx`，成功 RPM 只增加第二次 attempt。
- [x] 已验证 Midjourney Submit 仅业务码 `1/21/22`、SwapFace 仅 HTTP 200 且业务码 `1`、ImageSeed 仅 HTTP 2xx 计为成功；handler 记录的业务结果会传递到 controller 最终 Relay 指标。
- [x] 验证普通 Relay、任务 Relay 和 Midjourney controller panic 后 Relay inflight Gauge 归零并继续向 Gin Recovery 抛出。
- [x] 已验证客户端取消和 deadline 分别归类为 `client_cancelled`、`timeout`，Relay/channel inflight Gauge 全部归零，失败请求不增加成功 RPM。
- [x] 已验证首包后客户端取消计为失败，stream duration 记录 failure，TTFT 不进入成功 Histogram；clean EOF 仍计为成功并正常记录 stream duration、TTFT 和成功 RPM。
- [x] 已验证 `done`、clean `eof`、无错误的 `handler_stop`、timeout、client gone、scanner error、panic、ping fail、空/未知结束原因、`EndError`、soft error、nil status 和 handler 失败的最终成功判定；Prometheus 与性能看板共用该结果。
- [x] 验证未知错误只进入固定 `internal` 枚举。
- [x] 验证 route 使用模板路径，动态 ID 不会进入标签。
- [x] 已实现的指标 schema 不包含用户 ID、Token ID、IP、Request ID、Key 和错误文本，HTTP 路由测试同时验证查询参数中的 Token 不会出现在 `/metrics`。
- [x] 验证共享渠道状态只由 Master 导出，Slave 不调用数据源。
- [x] 固定输入规则测试已使用两个应用实例验证 Counter/Histogram 跨实例聚合，并保留 `cluster/job` 边界与 DB `instance` 维度。
- [x] 已验证收费和退款 Counter 分开记录，净额度由 charged - refunded 查询得到。
- [x] 已验证额度饱和事件复用同一 `QuotaClamp` 审计来源且只记录一次。
- [x] 验证 `/metrics` 默认关闭、认证失败拒绝、正确认证可抓取。
- [x] 使用独立 Prometheus registry 测试，避免污染全局 registry 和测试间状态。
- [x] D4 定向普通/race、D4-6 监控静态验证和当前工作树的 `go test ./... -count=1` 已重新执行并通过，全量回归没有被定向测试或 `promtool` 替代。
- [x] `pkg/prometheus_metrics`、`middleware`、`controller`、`router` 的相关 `go test -race` 已通过；`relay/helper` 的取消、timeout、clean EOF 用例以及 `relay/common`、`service` 的 StreamStatus/ChannelRuntime 用例已定向通过 race。
- [ ] 扩大到全部 `relay/helper` 测试的 race 验收尚未通过：现有大量 `t.Parallel()` 用例会触发 `logger/logger.go` 全局状态竞态；该问题与本批取消传播及流式成功判定无关，需单独修复后重跑。
- [ ] 扩大到全部 `service` 测试的 race 验收尚未通过：当前被 `service/task_polling_test.go` 触发的既有 logger 全局状态和异步 Task 对象竞态阻塞，需单独修复后重跑。
- [x] 使用 `promtool check rules --lint=all --lint-fatal` 和 `promtool test rules` 校验 Recording/告警规则。
- [x] `deploy/monitoring/validate.sh` 校验 Prometheus 配置、47 条基础 Recording Rules、72 条告警、0 条默认 Relay/渠道延迟与并发阈值规则、PostgreSQL/MySQL 两种 Compose profile、Exporter target/Secret、YAML/JSON 与 6 个 dashboard 的 108 条 PromQL。
- [x] 已在本地测试环境启动应用、Prometheus、Grafana、Alertmanager，验证安全抓取、规则加载、dashboard provisioning、告警通知与恢复链路。
- [x] 已补充指标口径、部署决策表、抓取示例、常用 PromQL、排障、基数、备份和升级文档。

## 十四、关键实现文件（规划）

建议依赖边界：`main.go` 负责组装，业务包只依赖 recorder 接口，router 只接收已经构建好的只读 handler。避免 `router`、`model` 和指标包形成循环依赖。

```go
type Runtime struct {
	Enabled bool
	Handler http.Handler
}

func NewRuntime(cfg Config, version string, mainDB, logDB *sql.DB, opts ...RuntimeOption) (*Runtime, error)
func SetMetricsRouter(router *gin.Engine, runtime *prometheusmetrics.Runtime)
```

- [x] `NewRuntime` 在禁用时返回 `Enabled=false` 且不创建 handler；启用但配置非法时返回 error。
- [x] Relay/channel 代码通过指标包的生命周期 API 记录，不持有 router；全局 runtime 由 `atomic.Pointer` 发布，未启用时 lifecycle 自动 no-op。
- [x] P1 计费指标已沿用 recorder 边界，业务包不依赖 router。
- [x] P1 任务指标已完成真实提交、poll、首次终态和 Master-only 队列生命周期接入，Task Dashboard、Recording/Alert Rules 和部署说明已通过静态验证，D4-7 定向普通/race、全量回归和 diff 检查已通过。

### 新建文件

- [x] `pkg/prometheus_metrics/config.go`：仅解析环境变量、校验 fail-closed 安全策略，不注册到普通系统设置。
- [x] `pkg/prometheus_metrics/registry.go`：独立 registry、Go/process/build collector 和 collector 自监控。
- [x] `pkg/prometheus_metrics/http.go`：HTTP Counter/Histogram。
- [x] `pkg/prometheus_metrics/rate_limit.go`：固定标签的限流拒绝 Counter 和 no-op recorder。
- [x] `pkg/prometheus_metrics/redis.go`：Redis 启用状态、go-redis Hook、缓存读取、限流失败和降级指标及固定标签归一化。
- [x] `common/cache_metrics.go`、`pkg/cachex/metrics.go`：无 Prometheus 依赖的轻量缓存观察回调，未安装 observer 时 no-op。
- [x] `pkg/prometheus_metrics/relay.go`：最终 Relay 和流式生命周期 recorder。
- [x] `pkg/prometheus_metrics/channel.go`：渠道 attempt/retry/inflight/duration recorder。
- [x] `pkg/prometheus_metrics/error_type.go`：唯一错误映射实现。
- [x] `pkg/prometheus_metrics/billing.go`：D3 指标 collector、固定标签归一化、持久化元数据解析和 no-op recorder，包含溢出防护、实际额度和饱和事件。
- [x] `pkg/prometheus_metrics/task.go`：D4 提交、完成、轮询 recorder，包含固定 platform/result 枚举、秒/毫秒换算和非法时长保护；completion 的真实终态接入仍由 9.2/D4-5 单独跟踪。
- [x] `pkg/prometheus_metrics/task_queue_collector.go`：Master-only 队列 collector、4×3 零填充、失败自监控和限频日志。
- [x] `pkg/prometheus_metrics/database_collector.go`：主库/日志库连接池 collector 与去重。
- [x] `pkg/prometheus_metrics/channel_state_collector.go`：Master-only 渠道启用状态、共享 collector 健康度、错误计数和日志限频。
- [x] `router/metrics-router.go`：按配置注册受保护的 `/metrics`。
- [x] `middleware/prometheus_http.go`：业务 HTTP 指标中间件；执行后依据 `route_tag` 决定是否记录。
- [x] `middleware/rate-limit.go`、`model-rate-limit.go`、`email-verification-rate-limit.go`：在实际 429 拒绝点记录固定 scope/reason，不记录用户、IP 或 Redis Key。
- [x] `deploy/monitoring/recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`：首批聚合与告警规则及固定输入测试，包含 D3 最小事件量。
- [x] `deploy/monitoring/` 其余产物：Prometheus 配置、Alertmanager、Grafana provisioning/dashboard（含 billing）、secret 示例、静态验证脚本和部署文档。
- [x] `docker-compose.monitoring.yml`：可选监控部署栈。

### 修改文件

- [x] `main.go`：`InitResources()` 成功后解析监控配置并创建 registry，注入仅查询 `id,type,status` 的 GORM 渠道状态数据源，再把依赖传给路由；配置非法时终止启动。
- [x] `router/main.go`：已在 `SetRelayRouter` 前调用 `SetMetricsRouter`；HTTP 指标中间件已在根 Engine 接入。
- [x] `controller/relay.go`：最终 Relay 完成点、任务提交结果和统一 channel attempt outcome。
- [x] `service/channel_runtime_metrics.go`：保留页面内存统计，并提供统一 channel attempt 生命周期入口。
- [x] `relay/mjproxy_handler.go`：SwapFace、ImageSeed 和 Submit 的上游 HTTP 调用统一进入渠道 attempt 生命周期，并复用各自业务成功判定。
- [x] `relay/common/relay_info.go`、`pkg/perf_metrics/metrics.go`：定义并复用最终成功判定，不读取 Prometheus 数据。
- [x] `model/log.go`：`RecordConsumeLog`/`RecordTaskBillingLog` 在 `createLog` 成功后记录最终 consume/refund、Token 和已附加的饱和事件。
- [x] `service/billing.go`、`service/billing_session.go`：记录预扣、结算、异步退款和订阅最终拒绝，不改变现有计费行为。
- [x] `service/task_billing.go`：在真正获胜的任务退款/二次结算路径记录 operation，额度 Counter 仍由成功持久化的任务日志驱动。
- [x] `service/log_info_generate.go`、`common/quota_math.go`：保持现有 `QuotaClamp` 审计事件，Prometheus 只消费该事件，不复制饱和判定。
- [x] `main.go`：D4 Master-only 队列 collector 注册和两次 GORM 分组查询数据源注入。
- [x] `controller/relay.go`、`service/task_polling.go`、`relay/relay_task.go`、`controller/midjourney.go`：D4 的两套任务提交和实际上游 poll 已接入。
- [x] `service/task_polling.go`、`relay/relay_task.go`、`controller/midjourney.go`、`relay/mjproxy_handler.go`：D4 首次终态 CAS 获胜点、缺失终态时间持久化和 completion/duration 已接入。
- [x] `relay/mjproxy_handler.go`：Midjourney Notify 已改为 `UpdateWithStatus(oldStatus)`，重复通知/CAS 失败保持幂等响应语义。
- [x] `model/task.go`、`model/midjourney.go`：已有可复用的 `UpdateWithStatus` CAS API；D4 不新建第二套状态更新机制。
- [x] `common/redis.go`、`pkg/cachex/hybrid_cache.go`：在真实缓存读取返回点记录 hit/miss/error；Hook 由 `main.go` 注入 Redis client 后在指标 Runtime 内安装。
- [x] `go.mod`、`go.sum`：将现有 Prometheus 依赖提升为 direct，版本保持 `v1.22.0`。

### 对应测试文件

- [x] `pkg/prometheus_metrics/config_test.go`：安全决策表、非法配置、敏感值不泄露和渠道 Histogram 开关。
- [x] `pkg/prometheus_metrics/registry_test.go`：独立 registry、Go/process/build 指标、重复注册和 gather 错误计数。
- [x] `pkg/prometheus_metrics/database_collector_test.go`：main/log、同库去重和累计值类型。
- [x] `pkg/prometheus_metrics/channel_state_collector_test.go`：启用/禁用状态、空表、查询失败、日志限频和 Master/Slave 导出边界。
- [x] `pkg/prometheus_metrics/error_type_test.go`：固定错误映射优先级、协议类型兜底和未知类型降级。
- [x] `router/metrics_router_test.go`：默认关闭、Token、IP、Public、路由顺序和中间件隔离。
- [x] `middleware/prometheus_http_test.go`：模板路由、动态 ID、业务 route tag、panic 后 5xx 和 `/metrics`/静态资源排除。
- [x] `pkg/prometheus_metrics/rate_limit_test.go`、`middleware/rate_limit_test.go`、`model_rate_limit_test.go`：验证标签归一化、全局/IP、邮箱验证和模型总量/成功量拒绝口径。
- [x] `pkg/prometheus_metrics/redis_test.go`、`common/redis_metrics_test.go`、`pkg/cachex/hybrid_cache_metrics_test.go`：验证单命令、pipeline、禁用、deadline、连接错误、缓存 hit/miss/error、标签归一化和 Key 不泄露。
- [x] `controller/relay_metrics_test.go`：流式最终判定、普通 Relay 非法请求、任务 Relay 早退失败、三类 controller panic、客户端取消、deadline、首包后取消和 clean EOF 的完成与 inflight 归还。
- [x] `relay/common/relay_info_test.go`、`pkg/perf_metrics/metrics_test.go`：表驱动验证 FinalSuccess 矩阵，以及性能看板采样与该结果一致。
- [x] `relay/helper/stream_scanner_test.go`、`relay/common/stream_status_test.go`：客户端取消中止上游、timeout、clean EOF 和流式终止状态回归。
- [x] `pkg/prometheus_metrics/channel_test.go`、`relay_test.go`：验证生命周期、固定标签、`sync.Once`、inflight、TTFT 和渠道 Histogram 关闭后的 Counter 保留行为。
- [x] `service/channel_runtime_metrics_test.go`：验证成功 RPM、60 秒窗口、空闲回收、panic 归还和两套统计同一生命周期。
- [x] `relay/mjproxy_handler_test.go`：验证 Submit、SwapFace、ImageSeed 的业务成功边界，以及上游结果同时驱动渠道 attempt 和 controller 最终结果。
- [x] `pkg/prometheus_metrics/billing_test.go`、`service/billing_metrics_test.go`及计费会话/任务计费测试：覆盖溢出、固定标签、持久化成功/失败、预扣/结算/退款、订阅拒绝、任务正/负差额、日志关闭和幂等早退。
- [x] `pkg/prometheus_metrics/task_test.go`、`task_queue_collector_test.go`、`main_prometheus_test.go`：已覆盖固定枚举、秒/毫秒换算、非法时长、零填充、查询失败、collector 自监控、Master/Slave 注册和两表分组数据源。
- [x] `controller/relay_metrics_test.go`、`service/task_polling_test.go`、`controller/midjourney_metrics_test.go`、`relay/relay_task_metrics_test.go`：已覆盖第 9.4 节提交与 poll 的成功、失败、早退和 panic 边界。
- [x] `service/task_polling_test.go`、`controller/midjourney_metrics_test.go`、`relay/relay_task_metrics_test.go`、`relay/mjproxy_handler_test.go`、`model/task_cas_test.go`：覆盖首次终态、重复轮询/通知、CAS 竞争、Notify 与轮询竞争、超时 sweep、秒/毫秒换算和负时长保护。
- [x] 客户端取消、deadline、首包后取消和 clean EOF 已补齐 controller/stream scanner 回归，并通过对应 race 测试。
- [x] 性能看板与 Prometheus 已共用 FinalSuccess；普通 Relay/任务 Relay 成功、真实重试和 B2 流式边界不再重复补测。
- [x] Midjourney controller 成功边界已补齐，P0-B 代码与自动测试完成。

## 十五、分批实施清单

每个批次应单独形成可审查提交。测试步骤失败时先修复本批问题，不得通过删测试、放宽断言或提前勾选绕过。

### 批次 A：P0-A 基础接入（已完成）

**文件：** `pkg/prometheus_metrics/config.go`、`registry.go`、`database_collector.go`、`channel_state_collector.go`、`router/metrics-router.go`、`main.go`。

- [x] 完成安全配置、独立 registry、Runtime/Build/DB 指标和 Master-only 渠道状态。
- [x] 完成配置、collector、路由隔离和 race 测试。

已通过的验收命令：

```bash
go test ./pkg/prometheus_metrics ./router ./middleware -count=1
go test -race ./pkg/prometheus_metrics ./router ./middleware -count=1
```

### 批次 B1：P0-B 成功与真实重试回归

**文件：**

- 修改测试：`controller/relay_metrics_test.go`
- 按失败测试最小修改：`controller/relay.go`、`service/channel_runtime_metrics.go`

- [x] 已增加普通非流式 Relay 成功测试，断言最终 `relay_requests_total{result="success"}` 只增加一次，Relay/channel inflight 均归零。
- [x] 已增加任务 Relay 成功提交测试，断言最终请求和渠道 attempt 各成功一次，并验证任务成功落库。
- [x] 已构造真实渠道选择重试：第一次渠道返回 500，第二次渠道成功；最终 Relay 只成功一次、attempt 为 `2`、retry 为 `1`、重试原因为 `upstream_5xx`。
- [x] 同一测试已验证渠道页 60 秒 RPM 只给成功的第二次 attempt 增加 `1`，失败的第一次不增加。
- [x] 目标测试及 race 测试均已通过，Relay/channel inflight 最终均为 `0`。

```bash
go test ./controller ./service -run 'RelayMetrics|ChannelRuntime' -count=1
go test -race ./controller ./service -run 'RelayMetrics|ChannelRuntime' -count=1
```

### 批次 B2：P0-B 取消、超时与异常流回归（已完成）

**文件：**

- 测试：`controller/relay_metrics_test.go`、`relay/helper/stream_scanner_test.go`、`relay/common/stream_status_test.go`
- 实现：`controller/relay.go`、`relay/channel/api_request.go`、`relay/common/stream_status.go`、`relay/helper/stream_scanner.go`

- [x] 已增加客户端取消测试，最终错误分类为 `client_cancelled`，Relay/channel inflight 都回到 `0`，成功 RPM 不增加。
- [x] 已增加请求 context deadline 测试，最终错误分类为 `timeout`，所有 inflight 回到 `0`；上游 HTTP 请求使用同一 context，取消和 deadline 可传递到 transport。
- [x] 已增加首包后的客户端取消测试，最终请求和渠道 attempt 均计为失败，stream duration 记录 failure，TTFT 不进入成功 TTFT Histogram。
- [x] 已增加 clean EOF 回归，按现有协议语义计为成功，并正常记录成功 stream duration、TTFT 和成功 RPM；普通上游 EOF 不按异常断开处理。
- [x] panic 仍重新抛给 Gin Recovery；生命周期完成函数继续由 `sync.Once` 防止重复归还和重复计数。
- [x] 目标测试及与本批相关的定向 race 测试全部 PASS；未把既有 logger 全局状态竞态误记为本批通过。

```bash
go test ./controller ./relay/helper ./relay/common ./service -run 'RelayMetrics|StreamScanner|StreamStatus|ChannelRuntime' -count=1
go test -race ./controller -run 'RelayMetrics' -count=1
go test -race ./relay/helper -run '^TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns$' -count=1
go test -race ./relay/helper -run '^TestStreamScannerHandler_StreamStatus_Timeout$' -count=1
go test -race ./relay/helper -run '^TestStreamScannerHandler_StreamStatus_EOFWithoutDone$' -count=1
go test -race ./relay/common ./service -run 'StreamStatus|ChannelRuntime' -count=1
```

### 批次 B3：统一性能看板与 Prometheus 成功判定（已完成）

**文件：**

- 修改：`relay/common/relay_info.go`、`controller/relay.go`、`pkg/perf_metrics/metrics.go`
- 测试：`relay/common/relay_info_test.go`、`pkg/perf_metrics/metrics_test.go`、`controller/relay_metrics_test.go`

- [x] 已在 `RelayInfo` 上增加 `FinalSuccess(handlerSuccess bool) bool` 作为唯一的流式最终成功判定：非流式不额外否决；流式有状态时要求正常结束、`EndError == nil` 且无 soft error，状态为 nil 时沿用 handler 结果。
- [x] 已列出尚未初始化 `StreamStatus` 的流式 handler；后续逐个接入状态后再收紧 nil 兼容分支，不能在本批直接把现有成功请求改成失败。
- [x] `relayMetricsOutcome` 与 `RecordRelaySample` 已复用该判定，不存在两处复制不同条件。
- [x] 表驱动覆盖 `done`、clean `eof`、无错误的 `handler_stop`、客户端离开、超时、scanner error、panic、ping fail、带 `EndError`、记录过 soft error、未知/空结束原因和 nil status。
- [x] 相关测试和 race 测试通过，性能看板与 Prometheus 对同一请求给出一致 success 值。

#### `StreamStatus == nil` 兼容路径审计

本批不改变这些既有流式路径的成功结果。当前静态检索确认下列路径未调用 `helper.StreamScannerHandler`，因此尚未初始化 `StreamStatus`，继续以 handler 的最终返回结果为准：

- AWS Bedrock：`relay/channel/aws/relay-aws.go` 的 `awsStreamHandler`。
- WebSocket：`relay/channel/openai/relay_realtime.go` 的 `OpenaiRealtimeHandler`，以及 `relay/channel/volcengine/tts.go` 的 `handleTTSWebSocketResponse`。
- 自定义 SSE/分块处理：`cloudflare`、`cohere`、`coze`、`ollama`、`palm`、`tencent`、`xunfei`、`zhipu` 对应的 stream handler。

Gemini native/Responses 路径最终复用 `geminiStreamHandler`，不属于该兼容清单。移除 nil 兼容分支前，必须重新执行此静态审计并为每条迁移路径补齐结束状态测试。

```bash
go test ./relay/common ./pkg/perf_metrics ./controller -count=1
go test -race ./relay/common ./pkg/perf_metrics ./controller -count=1
```

### 批次 B4：Midjourney controller 成功边界（已完成）

**文件：**

- 修改：`relay/mjproxy_handler.go`、`controller/relay.go`
- 测试：`relay/mjproxy_handler_test.go`、`controller/relay_metrics_test.go`

- [x] `doTrackedMidjourneyHttpRequest` 将与渠道 attempt 完全相同的业务成功结果记录到当前请求上下文，controller 不重新解析响应正文。
- [x] Submit 仅业务码 `1/21/22` 成功；SwapFace 仅 HTTP 200 且业务码 `1` 成功；ImageSeed 仅 HTTP 2xx 成功。
- [x] handler 返回 nil 但上游业务失败时，最终 `newapi_relay_requests_total{relay_format="mj_proxy"}` 记录 failure，inflight 正常归零；原有客户端响应、任务落库和计费分支不因监控判定被改写。
- [x] 普通测试和 race 测试均已通过。

```bash
go test ./relay ./controller ./service -run 'Midjourney|RelayMetrics|ChannelRuntime' -count=1
go test -race ./relay ./controller -run 'Midjourney|RelayMetrics' -count=1
```

### 批次 C1：P0-C Recording Rules 与告警规则（已完成）

**新建文件：**

- `deploy/monitoring/recording-rules.yml`
- `deploy/monitoring/recording-rules.test.yml`
- `deploy/monitoring/alert-rules.yml`
- `deploy/monitoring/alert-rules.test.yml`

- [x] 已写 `promtool test rules` 固定输入，覆盖服务成功比例和最小请求量、渠道失败/重试比例、P95/P99、DB `max_open=0`、`up==0` 和 Master collector absent。
- [x] C1 首批实现 12 条 Recording Rules，D3 增加 6 条计费规则，D4 增加 8 条任务规则，D6 增加 2 条固定错误规则，D7 增加 3 条 DB 等待规则，D8 增加 2 条 Runtime 增长规则，D9 增加 1 条按格式请求量规则，当前基础总数为 34 条；比例、平均值和增长率规则均保留必要的部署/实例维度。
- [x] C1 首批实现 11 条 warning/critical 告警，D3 增加 3 条计费告警，D4 增加 3 条任务积压/collector 告警，D6 增加 4 条 429/5xx/timeout 异常告警，D7 增加 1 条 DB 等待告警，D8 增加 2 条 Runtime 增长告警，D9 增加 2 条 Relay 延迟告警，当前总数为 26 条；均包含 `for`、统一 `severity` 标签和可操作 annotation，固定输入测试验证低流量、绝对量、标签隔离、进程运行时间、增长率、DB 等待双门槛以及 collector down/absent 行为。
- [x] 已运行规则校验，Recording Rules 与告警规则的语法、lint 和固定输入测试全部通过。

已通过的验收命令：

```bash
promtool check rules --lint=all --lint-fatal \
  deploy/monitoring/recording-rules.yml \
  deploy/monitoring/alert-rules.yml \
  deploy/monitoring/relay-latency-thresholds.yml
promtool test rules deploy/monitoring/recording-rules.test.yml
promtool test rules deploy/monitoring/alert-rules.test.yml
```

### 批次 C2：P0-C Grafana 与独立部署栈（静态产物与本地联调已完成）

**新建文件：**

- `docker-compose.monitoring.yml`
- `deploy/monitoring/prometheus.yml`
- `deploy/monitoring/alertmanager.yml.example`
- `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml`
- `deploy/monitoring/grafana/provisioning/dashboards/default.yml`
- `deploy/monitoring/grafana/dashboards/system-overview.json`
- `deploy/monitoring/grafana/dashboards/channel-overview.json`
- `deploy/monitoring/grafana/dashboards/billing-overview.json`
- `deploy/monitoring/grafana/dashboards/task-overview.json`
- `deploy/monitoring/validate.sh`
- `docs/prometheus-monitoring.md`

- [x] Prometheus 配置加载 Recording/Alert/Relay 延迟阈值/Relay 并发阈值 Rules，并提供单实例、多实例 target 示例和 Bearer credentials file 说明。
- [x] Grafana 系统面板实现最终请求、成功比例、延迟、inflight、Go/Process 和 DB；渠道面板实现成功/attempt/retry RPM、失败比例、耗时、inflight 和 enabled。
- [x] dashboard 提供 `instance`、`relay_format` 或 `channel_id` 等受控变量，并说明 Counter reset、无数据、Master 缺失和渠道 Histogram 关闭状态。
- [x] Compose 使用固定镜像版本、持久化卷、健康检查和外部 secret 文件；不修改默认业务部署栈。
- [x] 部署文档给出配置决策表、抓取示例、常用 PromQL、故障排查、基数、备份和升级步骤。
- [x] 静态验收通过：Prometheus 配置、35 条基础 Recording Rules、28 条告警规则、0 条默认延迟阈值规则、0 条默认并发阈值规则、Compose、Alertmanager YAML 契约、Grafana provisioning/JSON 和四个 dashboard 的 75 条 PromQL 均通过校验。
- [x] 本地启动验收已覆盖三个监控组件 healthy、应用 target 为 UP、原有两个 dashboard 自动加载、Webhook 恢复/抑制/静默生效；新增 billing/task dashboard 和 D3/D4 告警的容器加载需在发布环境复现。

```bash
PROMTOOL_BIN=/path/to/promtool AMTOOL_BIN=/path/to/amtool deploy/monitoring/validate.sh
docker compose -f docker-compose.monitoring.yml up -d
docker compose -f docker-compose.monitoring.yml ps
```

### 批次 C3：P0 发布验收

- [x] 本地测试环境记录 `R=1`、`N=0`，预算公式结果为 `1,880`；未达到关闭渠道 Histogram 的阈值。生产环境仍需代入真实 `R/N`。
- [x] 本地记录 `prometheus_tsdb_head_series=686`、应用自定义序列 13 条，并保存按 metric name 的拆分结果；生产发布仍需重新记录。
- [x] 两个应用实例的 HTTP Counter 分实例为 7/5，集群求和为 12，没有重复；共享渠道 collector 只由 Master 实例导出。
- [ ] 仍需用一个真实活动 Relay 请求验证 `newapi_relay_inflight` 和 `newapi_channel_inflight` 可按 `instance` 下钻；零流量时 GaugeVec 尚未产生标签序列。
- [x] 已在独立运行实例验证 Bearer、IP allowlist、Token/IP OR、Public 和 fail-closed 启动行为。
- [x] 已验证真实 `NewAPIInstanceDown` 规则 firing/resolved，以及 Alertmanager Webhook、抑制和静默链路。
- [x] 已人工触发 Master collector absent 告警：持续 5 分钟后 firing，Master 恢复后 resolved，Alertmanager 路由到 warning Webhook。
- [ ] 服务失败率表达式已由固定输入规则测试覆盖，Alertmanager 侧已用同名 warning/critical 验证抑制和恢复；发布环境是否执行高流量真实规则实测需写入发布记录。
- [x] 已重跑 `go test ./... -count=1`、P0 相关 race 测试和 `git diff --check`；完整 `service` race 的既有 `task_polling_test.go` 竞态仍单独保留为已知问题，不阻塞本批 P0 指标验收。

```bash
go test ./... -count=1
go test -race ./pkg/prometheus_metrics ./controller ./middleware ./relay ./router -count=1
git diff --check
```

### 批次 D1/D2：P1 限流与 Redis（已完成）

- [x] D1 限流拒绝：固定 `scope/reason` Counter、真实 429 拒绝点、Grafana RPM 面板、普通测试和 race 测试。
- [x] D2 Redis：go-redis Hook、启用状态、单命令/pipeline、耗时、cache hit/miss/error、限流失败、真实 fallback 降级、Dashboard 和持续异常告警。
- [x] Redis 验收覆盖禁用、deadline、连接错误和 Key 不泄露。

### 批次 D3：P1 计费、Token 与实际额度

**文件：** `pkg/prometheus_metrics/billing.go`、`model/log.go`、`service/billing.go`、`service/billing_session.go`、`service/task_billing.go`、`service/log_info_generate.go`，对应测试、Dashboard、Recording/Alert Rules 和部署文档。

- [x] 先用失败测试锁定第 8.4 节事件矩阵，再接入 recorder；不通过修改原有扣费/退款返回值来让指标测试通过。
- [x] 完成成功持久化 consume/refund、预扣/结算/退款 operation、订阅拒绝和 quota saturation 恰好一次。
- [x] 完成 Token、charged/refunded/net、actual charged/refunded/net、计费失败、订阅拒绝和饱和面板；No data 已区分“无事件”、“消费日志关闭”和“Prometheus 缺口”。
- [x] 为计费失败、订阅拒绝和饱和事件增加有最低事件量的 Recording/Alert Rules 固定输入测试，避免单次事件触发比例告警。

```bash
go test ./pkg/prometheus_metrics ./model ./service -run 'Billing|Quota|Subscription|TaskBilling' -count=1
go test -race ./pkg/prometheus_metrics ./model ./service -run 'Billing|Quota|Subscription|TaskBilling' -count=1
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D4：P1 异步任务

**文件：** `pkg/prometheus_metrics/task.go`、`task_queue_collector.go`、`main.go`、`controller/relay.go`、`service/task_polling.go`、`relay/relay_task.go`、`model/task.go`、`relay/mjproxy_handler.go`、`controller/midjourney.go`、`model/midjourney.go`，对应测试、Task Dashboard、Recording/Alert Rules、静态校验和部署文档。

> 最近一次 D4 基础测试（2026-07-29）：`go test ./pkg/prometheus_metrics -run 'TaskMetrics|TaskQueue' -count=1` 与对应 `-race` 命令均已通过。失败 collector 测试改为先采集自定义 collector、再独立断言 error Counter，避免依赖同一次 Registry 并发 gather 中两个 collector 的执行顺序。
>
> D4-5 的初始 RED 命令已经转为 PASS，且已扩展覆盖 Midjourney Notify、批量轮询、实时查询和 Notify/轮询竞争。保留该命令作为通用 Task 终态的最小回归入口：
>
> ```bash
> go test ./service -run 'TestUpdateVideoSingleTaskRecordsFirstTerminalCASOnce|TestUpdateSunoTasksStalePollsRefundExactlyOnce|TestSweepTimedOutTasksHonorsRefundRolloutBoundary' -count=1
> ```

- [x] D3 已完成定向普通/race 测试和监控静态验证，满足进入 D4 的前置条件。
- [x] 阶段 D4-1：`task.go` 与 `task_queue_collector.go` 的固定枚举、时间单位、零填充、错误自监控普通/race 测试已通过；此项只证明指标包基础能力，不代表业务指标已完成。
- [x] 阶段 D4-2：已在 `main.go` 注入两次分组查询组成的 Master-only 队列数据源，并验证 Master/Slave、空表、查询失败及通用 GORM 查询形态；普通/race 测试均通过。
- [x] 阶段 D4-3：已接入通用 Task 与 Midjourney 提交最终边界，覆盖成功、上游失败、本地插入失败、非提交 Midjourney 路径和 panic 恰好一次；普通/race 测试均通过。
- [x] 阶段 D4-4：已接入 Suno、Video、Midjourney 和实时查询的实际上游 poll，并覆盖成功、Fetch/HTTP/read/parse error 与本地早退不计数；普通/race 测试均通过。
- [x] 阶段 D4-5：已接入 Task/Midjourney 首次终态 CAS 与 duration，覆盖正常轮询、超时 sweep、Notify、重复轮询/通知、Notify 与轮询竞争、CAS 竞争和非法时长；定向普通/race 测试通过。
- [x] 阶段 D4-6：Task Dashboard、8 条任务 Recording Rules、3 条任务积压/collector 告警、固定输入规则测试和部署文档已完成；D4 当时验证 66 条 dashboard PromQL，D5 加入渠道首字节 P50/P95 后为 68 条，D7 加入 DB 等待次数/平均时长后当前总数为 70 条。积压 `> 100` 且持续 `15m` 仅是候选阈值，生产阈值在批次 E 校准前不得标记为最终值。
- [x] 阶段 D4-7：第 9.4 节定向行为测试、race、监控静态验证、`go test ./... -count=1` 全量回归和 `git diff --check` 全部通过。

```bash
go test ./pkg/prometheus_metrics ./controller ./relay ./service -run 'TaskMetrics|TaskPolling|TaskQueue|Midjourney|RealtimeTaskFetch|SweepTimedOutTasks|UpdateSunoTasks|UpdateVideoSingleTask' -count=1
go test -race ./pkg/prometheus_metrics ./controller ./relay ./service -run 'TaskMetrics|TaskPolling|TaskQueue|Midjourney|RealtimeTaskFetch|SweepTimedOutTasks|UpdateSunoTasks|UpdateVideoSingleTask' -count=1
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D5：P1 渠道 transport 首字节

**文件：** `pkg/prometheus_metrics/channel.go`、`service/channel_runtime_metrics.go`、`relay/channel/api_request.go`、`relay/channel/aws/relay-aws.go`、`relay/channel/xunfei/adaptor.go`、`relay/channel/xunfei/relay-xunfei.go`、`relay/channel/volcengine/tts.go`、`deploy/monitoring/grafana/dashboards/channel-overview.json`、`deploy/monitoring/validate.sh`、`docs/prometheus-monitoring.md`，以及对应测试和本 TODO。

- [x] 阶段 D5-1：先用失败测试锁定指标 API 和单请求去重，再注册 `newapi_channel_first_byte_seconds{channel_id,channel_type}`；非正 channel ID、非正耗时和监控未启用时 no-op。
- [x] 阶段 D5-2：共享 `relay/channel.doRequest` 注入统一 `httptrace`，以第一次 `GetConn` 到 `GotFirstResponseByte` 记录响应头首字节耗时；每次 HTTP 请求使用 `sync.Once`，并在 trace 创建时冻结 channel ID/type，避免并发回调读到后续 retry 的可变 `RelayInfo`；失败且未收到首字节时不伪造样本。
- [x] 阶段 D5-3：渠道 Dashboard 新增 transport 首字节 P50/P95 趋势，面板明确 HTTP、AWS SDK、WebSocket 使用相同响应头首字节口径，并说明 Histogram 关闭时的 No data 语义；静态验证脚本将 dashboard PromQL 精确总数固定为 68。
- [x] 阶段 D5-4：基数预算已增加 `14N` 首字节 Histogram，总上界更新为 `80R + 111N + 1,800`；`PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true` 同时关闭渠道总耗时和首字节 Histogram，保守渠道数阈值下调为 `N >= 300`。
- [x] 阶段 D5-5a：已完成非共享传输盘点；AWS Bedrock 普通、流式和 Nova 调用通过 SDK context 接入统一 trace，并用行为测试锁定标签冻结与单次记录。自建 HTTP client 中的鉴权、上传、模型管理、任务 polling 和内部后续查询已明确排除，不与主传输首字节混算。
- [x] 阶段 D5-5b：核对 Gorilla WebSocket v1.5.0 官方实现后确认 Upgrade 已原生触发 `GetConn`/`GotFirstResponseByte`；共享 Realtime、讯飞和火山 TTS 均通过 traced `DialContext` 接入同一 Histogram，并用真实 Upgrade 服务器完成 RED→GREEN 行为测试，不新增含义不同的 handshake/首帧指标。
- [x] 阶段 D5-6：包含 HTTP、AWS 和三类 WebSocket 路径的定向普通/race、当时的监控静态验证（26 条 Recording Rules、17 条告警、68 条 Dashboard PromQL）、`go test ./... -count=1` 和 `git diff --check` 全部通过；D6 完成后为 28/21，D7 完成后为 31/22，D8 完成后为 33/24，D9 完成后为 34/26，D10 完成后当前为 35/28。

```bash
go test ./pkg/prometheus_metrics ./relay/channel ./relay/channel/aws ./relay/channel/advancedcustom ./relay/channel/xunfei ./relay/channel/volcengine ./service -run 'AwsInvokeContext|ChannelFirstByte|SharedHTTPFirstByte|WebSocketFirstResponseByte|XunfeiMakeRequestRecords|HandleTTSWebSocketResponseRecords|ChannelDurationHistogram' -count=1
go test -race ./pkg/prometheus_metrics ./relay/channel ./relay/channel/aws ./relay/channel/advancedcustom ./relay/channel/xunfei ./relay/channel/volcengine ./service -run 'AwsInvokeContext|ChannelFirstByte|SharedHTTPFirstByte|WebSocketFirstResponseByte|XunfeiMakeRequestRecords|HandleTTSWebSocketResponseRecords|ChannelDurationHistogram' -count=1
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D6：P1 429、5xx 与 timeout 异常告警

**文件：** `deploy/monitoring/recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`、`validate.sh`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] 阶段 D6-1：先用失败的 `promtool test rules` 用例锁定固定 `error_type` 的 5 分钟事件量和比例，再新增 `newapi:relay_error_increase:5m`、`newapi:relay_error_ratio:5m`；分母为全部最终 Relay 请求，聚合保留 `cluster/job/error_type`。
- [x] 阶段 D6-2：新增 Relay `rate_limit`、`upstream_5xx`、`timeout` 三条 warning，全部同时要求比例、总请求量和对应错误事件量，并持续 10 分钟；严重的全局影响继续由现有服务成功率 critical 覆盖，避免重复增加同类 critical。
- [x] 阶段 D6-3：新增本地限流拒绝 warning，5 分钟事件数 `>= 20` 且持续 10 分钟；它只读取 `newapi_rate_limit_rejections_total`，与 Relay 上游 `rate_limit` 分开，annotation 要求先按固定 `scope/reason` 下钻再调整限额。
- [x] 阶段 D6-4：固定输入测试已覆盖高比例/高事件量触发和低流量不触发；D6 完成时为 28 条 Recording Rules、21 条告警和 68 条 Dashboard PromQL。
- [x] 阶段 D6-5：`promtool check/test`、监控静态验证、`go test ./... -count=1` 和 `git diff --check` 全部通过。

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D7：P1 数据库连接等待

**文件：** `deploy/monitoring/recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`、`grafana/dashboards/system-overview.json`、`validate.sh`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] 阶段 D7-1：使用 `sql.DB.Stats()` 已存在的 `WaitCount`/`WaitDuration` Counter，新增 5 分钟等待次数、累计等待时长和平均等待时长 3 条 Recording Rules；全部保留 `cluster/job/instance/database`，不跨实例求平均。
- [x] 阶段 D7-2：先用失败规则测试锁定“50 次/10 秒/平均 0.2 秒”的记录规则口径，再实现规则；`promtool test rules` 已经过 RED→GREEN。
- [x] 阶段 D7-3：新增 `NewAPIDBConnectionWaitHigh` warning，同时要求 5 分钟等待次数 `>= 20` 与平均等待 `> 0.1s`，持续 `10m`；固定输入测试分别证明高次数+高延迟触发、高次数+低延迟不触发、低次数+高延迟不触发。
- [x] 阶段 D7-4：System Overview 在现有 DB 连接池区域后新增等待次数和平均时长两个趋势面板，图例保留 `instance/database`，单位、告警候选阈值和 No data 语义已写入描述。
- [x] 阶段 D7-5：静态总数更新为 31 条 Recording Rules、22 条告警和 70 条 Dashboard PromQL；`promtool check/test`、Compose/JSON/PromQL 静态校验已通过。该阈值仍属于生产校准项，不代表所有部署的固定容量上限。
- [x] 阶段 D7-6：`go test ./... -count=1`、`git diff --check` 和未跟踪监控文件的行尾空白检查已通过，本批没有修改 Relay、计费、退款或客户端响应语义。

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D8：P1 Goroutine 与 Go heap 持续增长

**文件：** `deploy/monitoring/recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`、`grafana/dashboards/system-overview.json`、`validate.sh`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] 阶段 D8-1：复用 Go collector 已存在的 `go_goroutines`、`go_memstats_heap_alloc_bytes` 和 Process collector 的 `process_start_time_seconds`，不新增应用端指标或高基数标签。
- [x] 阶段 D8-2：按 Prometheus 官方 `deriv()` 口径新增 Goroutine 和 heap 的 30 分钟单实例每秒增长率 Recording Rules；先用线性固定输入验证 `0.5 goroutines/s` 与 `349525.33 bytes/s` 的 RED→GREEN。
- [x] 阶段 D8-3：新增 `NewAPIGoroutinesGrowing` 和 `NewAPIGoHeapGrowing` 两条 warning；除增长率外同时要求当前绝对量、实例运行至少 30 分钟与 `for: 15m`，防止启动期和低基数正常增长误报。
- [x] 阶段 D8-4：固定输入测试已覆盖高基数持续增长触发、高基数稳定不触发、低基数增长不触发、运行不满 30 分钟不触发。
- [x] 阶段 D8-5：System Overview 已有 heap/goroutine 单实例趋势面板，本批在面板描述中补充了告警口径和候选阈值，不重复创建相同数据面板；Dashboard PromQL 维持 70 条。
- [x] 阶段 D8-6：静态总数更新为 33 条 Recording Rules、24 条告警和 70 条 Dashboard PromQL；两组 Runtime 阈值仍属于生产校准项。
- [x] 阶段 D8-7：`promtool check/test`、监控静态校验、`go test ./... -count=1`、`git diff --check` 和未跟踪监控文件行尾空白检查已通过；本批只修改规则、测试、Dashboard 描述和文档。

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D9：P1 Relay P95/P99 延迟告警基础设施

**文件：** `deploy/monitoring/relay-latency-thresholds.yml`、`prometheus.yml`、`recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`、`alertmanager.yml.example`、`grafana/dashboards/system-overview.json`、`validate.sh`、`docker-compose.monitoring.yml`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] 阶段 D9-1：新增独立 Relay 延迟阈值规则文件并由 Prometheus/Compose 加载；仓库默认 `rules: []`，因此不导出阈值序列，P95/P99 告警默认休眠。
- [x] 阶段 D9-2：严格契约校验只接受固定 record 名、`cluster/job/relay_format/quantile` 四个标签、允许的 Relay 格式、`p95|p99` 和有限正数 `vector(<秒>)`；固定自测已拒绝未知格式、p90、0、负数、NaN、非常量、额外标签和重复键。
- [x] 阶段 D9-3：先用失败规则测试锁定按格式 5 分钟最终请求量，再新增 `newapi:relay_request_increase_by_format:5m`；成功和失败最终请求均计入样本量门槛，聚合保留 `cluster/job/relay_format`。
- [x] 阶段 D9-4：新增 `NewAPIRelayP95LatencyHigh` warning 和 `NewAPIRelayP99LatencyHigh` critical；P95/P99 分别要求同格式 5 分钟请求数 `>= 50`/`>= 100` 且持续 `10m`，阈值缺失时向量匹配自然为空。
- [x] 阶段 D9-5：固定输入测试已覆盖 P95/P99 高延迟触发、阈值缺失不触发、低流量不触发，以及 `cluster/job/relay_format` 不串用。
- [x] 阶段 D9-6：Alertmanager `group_by` 增加 `relay_format`，并由同 `cluster/job/relay_format` 的 P99 critical 抑制 P95 warning；契约校验锁定抑制范围。
- [x] 阶段 D9-7：System Overview 原延迟面板增加 P95/P99 阈值虚线和 No data 说明；阈值查询故意忽略 `instance`，静态 Dashboard PromQL 总数更新为 72 条。
- [x] 阶段 D9-8：静态总数为 34 条基础 Recording Rules、26 条告警、0 条默认延迟阈值规则和 72 条 Dashboard PromQL；`promtool check/test`、Compose/Alertmanager/Grafana 契约和完整监控校验已通过。
- [x] 阶段 D9-9：本批只修改 Prometheus/Alertmanager/Grafana 配置、规则测试和文档，不修改 Relay、计费、退款、重试、路由或客户端响应语义。
- [ ] 生产 P95/P99 阈值已根据真实的按格式基线完成校准并写入 `relay-latency-thresholds.yml`；仓库默认文件有意保持为空。

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D10：P1 Relay 并发异常告警基础设施

**文件：** `deploy/monitoring/relay-concurrency-thresholds.yml`、`prometheus.yml`、`recording-rules.yml`、`recording-rules.test.yml`、`alert-rules.yml`、`alert-rules.test.yml`、`alertmanager.yml.example`、`grafana/dashboards/system-overview.json`、`validate.sh`、`docker-compose.monitoring.yml`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] 阶段 D10-1：新增独立 Relay 并发阈值规则文件并由 Prometheus/Compose 加载；仓库默认 `rules: []`，因此不导出阈值序列，warning/critical 告警默认休眠。
- [x] 阶段 D10-2：严格契约校验只接受固定 record 名、`cluster/job/relay_format/severity` 四个标签、允许的 Relay 格式、`warning|critical` 和正整数 `vector(<integer>)`；固定自测已拒绝未知格式、未知严重级别、0、负数、小数、NaN、非常量、额外标签、重复键以及 critical 不高于 warning，并接受只配置单个严重级别。
- [x] 阶段 D10-3：先用失败规则测试锁定多实例、流式状态按格式求和，再新增 `newapi:relay_inflight_by_format`；聚合保留 `cluster/job/relay_format`，Gauge 求和明确属于抓取时刻趋势。
- [x] 阶段 D10-4：新增 `NewAPIRelayInflightHigh` warning 和 `NewAPIRelayInflightCritical` critical；实际并发必须严格大于阈值，warning/critical 分别持续 `10m`/`5m`，阈值缺失时向量匹配自然为空。
- [x] 阶段 D10-5：固定输入测试已覆盖 warning/critical 持续触发、等于阈值不触发、阈值缺失不触发、`cluster/job/relay_format` 不串用和短暂峰值不触发。
- [x] 阶段 D10-6：Alertmanager 由同 `cluster/job/relay_format` 的 inflight critical 抑制 warning；契约校验锁定抑制范围。
- [x] 阶段 D10-7：System Overview 新增按格式实际 inflight 与 warning/critical 阈值面板；阈值查询故意忽略 `instance`，不同严重级别使用文字图例、不同虚线和线宽区分，并说明默认 No data 与多实例抓取错峰语义。
- [x] 阶段 D10-8：静态总数为 35 条基础 Recording Rules、28 条告警、75 条 Dashboard PromQL、0 条默认延迟阈值规则和 0 条默认并发阈值规则；`promtool check/test`、Compose/Alertmanager/Grafana 契约和完整监控校验已通过。
- [x] 阶段 D10-9：本批只修改 Prometheus/Alertmanager/Grafana 配置、规则测试和文档，不修改 Relay、渠道状态、亲和性、优先级、计费、退款、重试、路由、并发拒绝或客户端响应语义；`go test ./... -count=1`、`git diff --check` 和监控文件行尾空白检查均已通过。
- [ ] 生产 Relay 并发阈值已根据真实的按格式基线完成校准并写入 `relay-concurrency-thresholds.yml`；仓库默认文件有意保持为空。

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

### 批次 D11：四层中文监控重构

**文件：** `pkg/prometheus_metrics/channel.go`、`service/channel_runtime_metrics.go`、`relay/common/relay_info.go`、各渠道 Usage/TTFT 结算接入点、`docker-compose.monitoring.yml`、`deploy/monitoring/prometheus.yml`、`deploy/monitoring/targets/`、`deploy/monitoring/recording-rules.yml`、`deploy/monitoring/grafana/dashboards/core/`、`deploy/monitoring/grafana/dashboards/extended/`、`deploy/monitoring/validate.sh`、`docs/prometheus-monitoring.md` 和本 TODO。

- [x] D11-1：渠道指标新增 `newapi_channel_ttft_seconds` 与 `newapi_channel_tokens_total`；Token 类型只允许 `input` / `output` / `cache_read`，不增加模型、用户、Token ID、请求 ID 或原始错误等高基数标签。
- [x] D11-2：TTFT 按当前渠道 attempt 独立计时，重试后的新渠道不包含前一失败渠道耗时；Cloudflare/Cohere 等路径统一使用 `SetFirstResponseTime()`。
- [x] D11-3：Token 只在最终结算点记录，文本使用 `billingUsage`，音频使用归一化 `Usage`；不在预扣费、失败路径或重试中间态重复记录。Usage 存在时会显式导出固定的三种 Token 类型，因此零缓存命中显示 `0%`而不是 No data。
- [x] D11-4：新增 11 条渠道 Recording Rules，提供 1 分钟 attempt/失败/重试 RPM、成功率/超时率，以及 5 分钟 P90/P95、TTFT P95、上游首字节 P95、`cache_read / input` 上游缓存率和 Token 每分钟吞吐。
- [x] D11-5：监控 Compose 新增固定版本 Node Exporter、PostgreSQL Exporter、MySQL Exporter 和 Redis Exporter；Exporter 不发布公网端口，通过外部业务 Docker 网络和运行时 Secret 连接，MySQL target 默认为空。Redis Secret 使用官方要求的地址到密码 JSON 映射，校验脚本拒绝原始密码文本。
- [x] D11-6：Grafana 重构为 `new-api 监控` 下的主机/程序/中间件/渠道总览，以及 `new-api 扩展监控` 下的计费/任务总览；6 页标题、行、面板和说明均为中文，刷新周期统一为 15 秒。
- [x] D11-7：Grafana 12.1 临时容器已真实验证双文件夹和 6 个 dashboard provisioning；108 条 Dashboard PromQL 已通过 Prometheus 语法校验。
- [x] D11-8：当前静态验收口径为 46 条 Recording Rules、28 条告警、108 条 Dashboard PromQL；Go 定向测试覆盖指标注册、渠道 attempt 归因、RelayInfo 与最终结算路径。
- [x] D11-9：生产 PostgreSQL profile 已启动，new-api、Node、PostgreSQL、Redis target 均为 `UP`，且未破坏现有数据卷。
- [ ] D11-10：生产 Grafana 已验证 6 个中文 dashboard 与双文件夹；真实多渠道流量已产生至少两个 `channel_id` 的 RPM/P95/TTFT/缓存率/Token 证据。当前生产只有 1 个启用渠道，已完成 `channel_id=1` 的真实流量验收，第二个渠道未上线前不勾选本项。Provider 无 Usage 的面板允许显示“暂无数据”。

#### D11 生产验收记录（2026-07-29）

- [x] new-api 应用镜像基于 `6712fbc2f`构建并健康运行；PostgreSQL、Redis 业务容器和 7 个原有数据卷原样保留。
- [x] Prometheus 的 `new-api`、`node-exporter`、`postgres-exporter`、`redis-exporter` 四个 target 均为 `UP`；46 条 Recording Rules 和 28 条告警的 rule health 均为 `ok`。
- [x] Grafana 只保留 `new-api 监控`、`new-api 扩展监控` 两个中文文件夹，6 个 dashboard UID 已通过 API 确认。
- [x] `channel_id=1` 真实成功流量已验证 1 分钟 attempt RPM `1.33`、成功率 `100%`、TTFT P95 `1.95s`、Token 吞吐以及上游缓存命中率 `0%`；零缓存命中已不再显示 No data。
- [ ] 多渠道对比的生产数据证据待第二个渠道实际启用后补齐，不为了验收人为伪造渠道。

```bash
go test ./pkg/prometheus_metrics ./service ./controller ./relay/common ./relay -count=1
PROMTOOL_BIN=/path/to/promtool deploy/monitoring/validate.sh
jq empty deploy/monitoring/grafana/dashboards/core/*.json deploy/monitoring/grafana/dashboards/extended/*.json
git diff --check
```

### 批次 D12：完整告警体系与实时首字等待

**文件：** `pkg/prometheus_metrics/channel.go`、`service/channel_runtime_metrics.go`、`relay/common/relay_info.go`、`controller/relay.go`、`deploy/monitoring/recording-rules.yml`、`alert-rules.yml`、渠道阈值文件、Prometheus/Alertmanager/Compose 配置、规则测试、`validate.sh` 和监控文档。

- [x] D12-1：新增 `newapi_channel_stream_first_token_waiting` 实时 Gauge，固定统计等待首个有效流式内容超过 30/60 秒的当前渠道 attempt；首字、取消、失败、超时、panic 和正常结束都能归还。
- [x] D12-2：新增渠道/集群首字等待 warning/critical，渠道默认门槛为 30 秒并发 `>=3`、60 秒并发 `>=5`，集群默认门槛为 10/20，均持续 2 分钟。
- [x] D12-3：新增主机 CPU、内存、磁盘、只读、I/O、swap，应用进程 CPU/FD，Exporter/collector 可用性，PostgreSQL/MySQL 连接与 PostgreSQL 死锁，Redis 可用性、内存、淘汰和拒绝连接告警。
- [x] D12-4：新增渠道 P95 总耗时、TTFT、上游首字节和 inflight 的独立阈值文件；默认空规则保证未校准渠道不会误报。
- [x] D12-5：Alertmanager 增加渠道、主机和中间件 warning/critical 抑制，新增规则 annotation 使用中文。
- [x] D12-6：当前静态口径为 47 条 Recording Rules、72 条告警、0 条默认 Relay/渠道延迟与并发阈值规则；固定输入测试覆盖实时首字、主机资源、PostgreSQL/Redis、任务成功率/轮询错误和渠道阈值匹配。
- [ ] D12-7：发布到生产并观察告警噪声；生产阈值和外部通知链路验收完成后勾选。

### 批次 E：生产校准

- [ ] 上线观察两周，记录时间序列数量、scrape duration/size、Histogram 分布、应用开销和告警噪声。
- [ ] 根据真实的每个 `cluster/job/relay_format` P95/P99 分布校准延迟阈值并记录选择依据；仓库默认阈值文件继续保持为空。
- [ ] 根据真实的每个 `cluster/job/relay_format` inflight 分布校准 warning/critical 并发阈值并记录选择依据；仓库默认阈值文件继续保持为空。
- [ ] 根据真实分布调整 buckets、阈值和 SLO；每次规则修改同步更新 `promtool test rules` 固定用例和变更记录。
- [ ] 将确认稳定的告警接入值班流程；无法给出明确处置动作的告警只保留面板，不发送通知。
