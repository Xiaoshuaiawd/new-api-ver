# Prometheus Relay 并发异常告警设计

## 目标

为集群内各 Relay 格式增加可配置、默认休眠、支持多实例聚合的持续高并发告警。该告警描述“当前并发超过运维人员根据生产基线配置的异常阈值”，不声称应用已经存在并发硬上限，也不把阈值解释为限流容量。

实现不修改 Relay、渠道选择、重试、计费、退款、请求队列或客户端响应语义，只复用现有 `newapi_relay_inflight` Gauge，并增加 Prometheus 规则、Alertmanager 抑制、Grafana 面板、部署文档和 TODO 证据。

## 方案选择

采用独立 Prometheus 阈值规则文件方案：

- 新增 `deploy/monitoring/relay-concurrency-thresholds.yml`。
- 仓库默认文件不定义任何阈值序列，因此告警默认不会进入 pending。
- 阈值按 `cluster/job/relay_format/severity` 配置，修改后通过 Prometheus reload 生效，不需要重启应用。
- 一个 Prometheus 管理多个集群时，每条阈值必须显式带 `cluster` 和 `job`，不得跨部署共享。

未采用的方案：

- 复用 Relay 延迟阈值文件：会把秒和请求数两种单位、两套标签约束及两类排障语义混在同一文件中。
- 使用历史均值或标准差自动检测：冷启动、扩缩容和业务周期变化容易产生噪声，且难以给出稳定、可回归的触发口径。
- 在应用中导出所谓“固定并发上限”：当前项目没有通用并发拒绝器或请求队列，导出未被业务实际执行的上限会误导运维。

## 阈值指标与配置契约

阈值统一导出为：

```text
newapi_relay_inflight_threshold{cluster,job,relay_format,severity}
```

约束如下：

- `severity` 只允许 `warning` 和 `critical`。
- `relay_format` 只允许项目已有固定枚举：`openai`、`claude`、`gemini`、`openai_responses`、`openai_responses_compaction`、`openai_alpha_search`、`openai_audio`、`openai_image`、`openai_realtime`、`rerank`、`embedding`、`task`、`mj_proxy`、`other`。
- 表达式必须为正整数请求数，写成 `vector(<positive integer>)`。
- 相同 `cluster/job/relay_format/severity` 不允许重复。
- warning 和 critical 可独立配置；同一 `cluster/job/relay_format` 同时配置两者时，critical 必须严格大于 warning。
- 禁止增加 `instance`、channel ID、模型、用户、Token、IP、Request ID、错误文本或其他动态标签。

生产校准后的示例：

```yaml
groups:
  - name: newapi-relay-concurrency-thresholds
    rules:
      - record: newapi_relay_inflight_threshold
        expr: vector(80)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          severity: warning
      - record: newapi_relay_inflight_threshold
        expr: vector(120)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          severity: critical
```

阈值文件由统一校验脚本检查 Prometheus 语法、record 名称、标签集合、固定枚举、正整数、重复键和 warning/critical 大小关系。

## 多实例聚合

新增无窗口 Recording Rule：

```text
newapi:relay_inflight_by_format{cluster,job,relay_format}
```

表达式为：

```promql
sum by (cluster, job, relay_format) (newapi_relay_inflight)
```

该规则合并所有实例和 `stream=true|false`，用于集群级格式容量趋势与告警。原始 `newapi_relay_inflight` 继续保留 `instance`（抓取标签）、`relay_format` 和 `stream`，现有按实例下钻和流式连接面板不变。

Gauge 来自不同 target 的抓取时间存在秒级错峰，因此聚合值是持续容量趋势，不是严格同一纳秒的并发快照。告警必须带持续时间，不能因一次 scrape 峰值触发。

## 告警规则

### Warning

`NewAPIRelayInflightHigh` 同时满足以下条件才触发：

- 当前 `cluster/job/relay_format` 存在 `severity="warning"` 阈值。
- `newapi:relay_inflight_by_format` 高于该阈值。
- 条件持续 `10m`。

### Critical

`NewAPIRelayInflightCritical` 同时满足以下条件才触发：

- 当前 `cluster/job/relay_format` 存在 `severity="critical"` 阈值。
- `newapi:relay_inflight_by_format` 高于该阈值。
- 条件持续 `5m`。

阈值序列缺失时，PromQL 向量匹配没有右侧样本，告警自然休眠，不使用默认请求数兜底。Alertmanager 使用 `cluster/job/relay_format` 匹配，由 critical 抑制同格式 warning。

告警 annotation 必须说明这是“校准阈值”而不是应用硬上限，并建议检查流式长连接、上游变慢、请求堆积、实例分布和扩容需求。告警本身不触发自动禁用渠道、路由修改、请求拒绝或进程扩缩容。

## Grafana

System Overview 新增 `Relay inflight by format` 时间序列面板：

- 实际并发按 `relay_format` 使用实线展示。
- warning 和 critical 阈值使用不同虚线样式，并在图例中明确写出格式和级别，不能只依赖颜色区分。
- 阈值查询忽略 `instance` 变量，因为阈值属于集群/作业/格式配置。
- 实际并发查询继续响应 `cluster` 和 `relay_format` 变量；它是集群聚合面板，不响应 `instance`，避免把全局阈值与单实例值直接比较。
- 默认空阈值文件时只显示实际并发，阈值线为 No data，不解释为 0。
- 面板描述明确多实例 scrape 错峰、持续时间和生产校准语义。

现有 `Relay inflight` stat、`Inflight by instance` 和按格式流式 inflight 面板保留，分别用于总览、实例倾斜和长连接排查。

## 测试与验证

阈值契约测试覆盖：

- 默认空规则文件通过校验。
- 未知 `relay_format` 或 severity 被拒绝。
- 0、负数、小数、NaN 和非常量表达式被拒绝。
- 额外标签、重复键和 critical 不大于 warning 被拒绝。
- 只配置 warning 或只配置 critical 可以通过。

Recording Rule 固定输入使用两个实例、流式和非流式序列，证明按 `cluster/job/relay_format` 求和并去除 `instance/stream`。

Alert Rule 固定输入覆盖：

- warning 高于阈值并持续 10 分钟触发。
- critical 高于阈值并持续 5 分钟触发。
- 等于阈值不触发。
- 阈值缺失不触发。
- 不同 `cluster`、`job` 或 `relay_format` 的阈值不会串用。
- 短暂峰值不会满足持续时间。

Alertmanager 契约校验覆盖 critical 对同 `cluster/job/relay_format` warning 的抑制。Dashboard 静态校验覆盖实际值、warning 阈值、critical 阈值三条 PromQL，以及阈值虚线和文本图例。

本批完成后的静态总数固定为 35 条基础 Recording Rules、28 条告警、75 条 Dashboard PromQL，以及默认 0 条并发阈值规则。Prometheus 配置共加载 4 个规则文件：基础 Recording Rules、Alert Rules、Relay 延迟阈值和 Relay 并发阈值。

完整验收继续运行：

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
go test ./... -count=1
git diff --check
```

## 文档和 TODO

`docs/prometheus-monitoring.md` 增加阈值配置、校验、reload、PromQL 和排障说明。`docs/prometheus-monitoring-todolist.md` 新增 D10 批次，记录阈值文件、聚合规则、两级告警、抑制、Dashboard、固定输入测试和验证总数。

“并发告警基础设施已完成”和“生产并发阈值已校准”必须保持两个独立状态：规则和测试完成后可勾选前者；未取得真实生产基线前，生产阈值校准必须保持未勾选。

## 非目标

- 不增加渠道级阈值或按 channel ID 告警。
- 不增加模型、用户、Token、IP、Request ID 或错误文本维度。
- 不新增并发拒绝器、请求队列或 `newapi_concurrency_rejections_total`。
- 不把阈值描述为应用硬上限、订阅限额或上游供应商容量。
- 不根据告警自动禁用渠道、改变亲和性/优先级、修改路由、拒绝请求或终止连接。
