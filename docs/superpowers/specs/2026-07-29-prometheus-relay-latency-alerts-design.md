# Prometheus Relay 延迟告警设计

## 目标

为 Relay P95/P99 耗时增加可配置、默认休眠、低噪声的 Prometheus 告警。文本、图片、音频、Realtime 和异步任务的正常耗时差异很大，因此禁止使用一个全局秒数阈值。

实现不修改 Relay、计费、退款、重试或客户端响应语义，只使用已有 Histogram、Recording Rules、Grafana 和 Alertmanager。

## 方案选择

采用“独立 Prometheus 阈值规则文件”方案。

- 仓库提供 `deploy/monitoring/relay-latency-thresholds.yml`，默认不定义任何阈值序列。
- 部署人员在完成基线观察后，为指定 `cluster/job/relay_format/quantile` 添加常量 Recording Rule。
- Prometheus 重载规则即可生效，不需要重启应用实例。
- 一个 Prometheus 管理多个集群时，阈值必须显式带 `cluster` 和 `job`，不跨部署边界共享。

未采用的方案：

- 应用环境变量 JSON：需要每个实例保持一致，配置漂移时会导出冲突阈值，修改还需重启应用。
- 直接把秒数写入 `alert-rules.yml`：实现简单，但不同部署无法独立校准，也容易误伤慢任务格式。

## 阈值文件

阈值统一导出为：

```text
newapi_relay_latency_threshold_seconds{cluster,job,relay_format,quantile}
```

`quantile` 只允许 `p95` 和 `p99`，`relay_format` 只允许项目已有固定枚举，阈值必须是有限正数秒。默认文件不产生任何该指标，所以延迟告警默认不会进入 pending。

生产校准后的单条配置形式：

```yaml
- record: newapi_relay_latency_threshold_seconds
  expr: vector(10)
  labels:
    cluster: default
    job: new-api
    relay_format: openai
    quantile: p95
```

阈值文件由 Prometheus `rule_files` 加载，统一校验脚本检查：

- YAML 和 PromQL 语法有效。
- record 名称固定。
- 只出现允许的标签和枚举值。
- 相同 `cluster/job/relay_format/quantile` 不允许重复。
- 表达式为正数秒常量。

## Recording Rules

已有：

```text
newapi:relay_duration_seconds:p95_5m{cluster,job,relay_format}
newapi:relay_duration_seconds:p99_5m{cluster,job,relay_format}
```

新增：

```text
newapi:relay_request_increase_by_format:5m{cluster,job,relay_format}
```

请求量使用最终 `newapi_relay_requests_total` 的 5 分钟 `increase()`，成功和失败均进入分母。它只作为分位数告警的样本量门槛，不改变现有服务成功率口径。

## 告警

### P95 warning

`NewAPIRelayP95LatencyHigh` 同时满足以下条件才触发：

- 当前 `cluster/job/relay_format` 存在 `quantile="p95"` 阈值。
- 5 分钟 P95 高于该阈值。
- 5 分钟最终 Relay 请求数 `>= 50`。
- 条件持续 `10m`。

### P99 critical

`NewAPIRelayP99LatencyHigh` 同时满足以下条件才触发：

- 当前 `cluster/job/relay_format` 存在 `quantile="p99"` 阈值。
- 5 分钟 P99 高于该阈值。
- 5 分钟最终 Relay 请求数 `>= 100`。
- 条件持续 `10m`。

Alertmanager 使用 `cluster/job/relay_format` 匹配，由 P99 critical 抑制同格式的 P95 warning。阈值序列缺失时，向量匹配没有右侧样本，告警自然休眠，不使用默认秒数兜底。

## Grafana

System Overview 的 Relay latency percentiles 面板保留现有 P50/P95/P99，新增 P95/P99 阈值线：

- 阈值线忽略 `instance` 变量，因为它属于集群/作业/格式配置。
- 未配置阈值时不显示阈值线，不解释为 0 秒。
- 面板描述明确样本量门槛、默认休眠和生产校准语义。

## 测试

Recording Rule 固定输入覆盖按 `relay_format` 保留的 5 分钟请求数。

Alert Rule 固定输入覆盖：

- P95 高延迟 + 足够请求量触发 warning。
- P99 高延迟 + 足够请求量触发 critical。
- 阈值缺失时不触发。
- 请求量不足时不触发。
- 不同 `cluster`、`job` 或 `relay_format` 的阈值不会串用。

Alertmanager 配置校验覆盖 P99 critical 对同 `cluster/job/relay_format` P95 warning 的抑制。Dashboard 静态校验覆盖两条阈值 PromQL。

## 文档和 TODO

`docs/prometheus-monitoring.md` 增加阈值文件配置、重载、校验、PromQL 和排障说明。`docs/prometheus-monitoring-todolist.md` 新增一个批次，分别标记规则、告警、抑制、Dashboard、默认休眠和验证证据。

“延迟告警基础设施已完成”和“生产阈值已校准”必须保持两个独立状态：代码与规则实现后可勾选前者，未完成生产基线观察时不允许勾选后者。

## 非目标

- 不为每个模型、渠道或用户设置延迟阈值。
- 不把模型名称、用户、Token、IP、Request ID 或错误文本加入标签。
- 不根据延迟告警自动禁用渠道、修改路由或终止请求。
- 不在没有生产基线时预填所谓“合理默认值”。
