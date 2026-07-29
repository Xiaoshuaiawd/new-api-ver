# 四层 Prometheus 监控重构设计

## 1. 目标

将现有混杂的 Grafana 监控重构为四个边界清晰的核心仪表盘：

1. 主机总览：服务器 CPU、内存和磁盘。
2. 程序总览：new-api 的 Go 堆内存分配、协程和进程 CPU。
3. 中间件总览：PostgreSQL、MySQL 和 Redis 服务自身状态。
4. 渠道总览：以 `channel_id` 为核心维度，对多个渠道进行实时横向比较。

现有计费总览和任务总览不删除，移动到“new-api 扩展监控”文件夹。四个核心仪表盘放在“new-api 监控”文件夹。

## 2. 范围与非目标

### 2.1 本次范围

- 接入 Node Exporter、PostgreSQL Exporter、Redis Exporter。
- 提供 MySQL Exporter 配置，但当前 PostgreSQL 部署不启动 MySQL Exporter。
- 扩展 new-api 渠道 Prometheus 指标，使 TTFT 和 Token 用量支持 `channel_id`。
- 增加渠道 1 分钟实时记录规则。
- 重构 Grafana provisioning、目录和仪表盘。
- 更新监控部署文档、校验脚本和回归测试。
- 将完成后的配置同步到当前服务器并验证真实数据链路。

### 2.2 非目标

- 不增加用户、令牌、模型、请求 ID、Redis Key 等高基数标签。
- 不修改渠道调度、计费、重试或亲和性行为。
- 不把应用内部 Redis/内存缓存命中率解释为渠道上游缓存命中率。
- 不在本次重构中增加容器级 cAdvisor 监控；主机指标由 Node Exporter 提供，new-api 进程指标由应用自身提供。
- 不强制 PostgreSQL 和 MySQL Exporter 同时运行。

## 3. 指标口径

### 3.1 时间与聚合

- Prometheus 抓取间隔：15 秒。
- Grafana 默认刷新间隔：15 秒。
- 渠道实时 RPM、失败 RPM、重试 RPM、成功率和超时率：最近 1 分钟滑动窗口。
- 渠道延迟分位数：最近 5 分钟直方图窗口，避免 1 分钟低流量下分位数失真。
- 多实例 Counter：按 `channel_id` 求和。
- 多实例渠道并发 Gauge：按 `channel_id` 求和，用于观察抓取时刻趋势。
- 状态 Gauge：使用 `max` 或按单实例展示，不将状态值相加。

### 3.2 渠道上游缓存命中率

渠道缓存命中率使用 Token 比例口径：

```text
缓存命中率 = 缓存命中 Token / 输入 Token × 100%
```

- 输入 Token 和缓存 Token 使用项目已归一化的 `dto.Usage`，不直接解析各 provider 的原始响应。
- 缓存 Token 优先使用规范化的 `PromptTokensDetails.CachedTokens`；兼容 Responses 的 `InputTokensDetails.CachedTokens` 和已有 `PromptCacheHitTokens`，但同一请求不得重复累计。
- 输入 Token 为归一化后的总输入 Token，包含其中被缓存命中的部分。
- 输入 Token 为 0 时不计算比例。
- Provider 没有返回 Usage 时不伪造 Token 数据。

### 3.3 TTFT 与上游首字节延迟

- TTFT：成功流式请求从当前渠道 attempt 开始到该 attempt 首个内容响应的耗时，按渠道统计 P95。发生渠道重试时，每个 attempt 独立计时，不能把前一个失败渠道的耗时归到后续渠道。
- 上游首字节延迟：从 transport 获取连接到上游响应头首字节可用的耗时，按渠道统计 P95。
- 两者含义不同，必须使用独立指标和独立面板。

## 4. 采集架构

```text
Linux 主机 ── Node Exporter ───────────────┐
new-api ──── /metrics ─────────────────────┤
PostgreSQL ─ PostgreSQL Exporter ──────────┤
MySQL ────── MySQL Exporter（可选）────────┼─ Prometheus ─ Grafana
Redis ────── Redis Exporter ───────────────┤
                                            └─ Alertmanager（保留现状）
```

### 4.1 网络与安全

- Exporter 只加入 Docker 内部网络，不向公网暴露端口。
- PostgreSQL、MySQL 和 Redis 凭据继续来自运行时 secrets/env 文件，不写入 Git。
- Prometheus 通过内部服务名抓取 Exporter。
- Node Exporter 以只读方式挂载主机 `/proc`、`/sys` 和根文件系统。
- MySQL Exporter 使用独立 profile 或独立启用开关，默认不启动。

## 5. 应用指标扩展

### 5.1 渠道 TTFT

新增：

```text
newapi_channel_ttft_seconds{channel_id,channel_type}
```

- 类型：Histogram。
- Bucket 复用现有 TTFT bucket。
- 只记录成功流式请求且 TTFT 大于 0 的样本。
- 使用已选择的真实渠道 ID，不使用模型、用户或请求标签。
- 保留现有 `newapi_stream_ttft_seconds{relay_format}`，避免破坏已有看板和告警。

### 5.2 渠道 Token 吞吐

新增：

```text
newapi_channel_tokens_total{channel_id,channel_type,token_type}
```

`token_type` 只允许固定枚举：

- `input`
- `output`
- `cache_read`

规则：

- 类型：Counter。
- 只记录大于等于 0 的归一化 Usage 数量。
- 同一请求只在最终 Usage 确认点记录一次。
- 不把缓存写入 Token 混入缓存读取命中率。
- 不增加 provider、model、user、token_name 等标签。

### 5.3 复用的现有渠道指标

- `newapi_channel_attempts_total`
- `newapi_channel_retries_total`
- `newapi_channel_inflight`
- `newapi_channel_duration_seconds`
- `newapi_channel_first_byte_seconds`
- 渠道启用状态 collector

失败 RPM、超时率、429、鉴权、上游 4xx/5xx、客户端取消和流式中断均从现有 `result`、`error_type`、`stream` 固定标签派生，不再新增重复 Counter。

## 6. Recording Rules

新增或调整以下渠道记录规则：

- `newapi:channel_attempt_rpm:1m`
- `newapi:channel_failure_rpm:1m`
- `newapi:channel_retry_rpm:1m`
- `newapi:channel_success_ratio:1m`
- `newapi:channel_timeout_ratio:1m`
- `newapi:channel_duration_seconds:p90_5m`
- `newapi:channel_duration_seconds:p95_5m`
- `newapi:channel_ttft_seconds:p95_5m`
- `newapi:channel_first_byte_seconds:p95_5m`
- `newapi:channel_cache_hit_ratio:5m`
- `newapi:channel_tokens_per_minute:5m`

缓存命中率必须先分别聚合 Counter rate，再做除法：

```promql
sum by (cluster, job, channel_id) (
  rate(newapi_channel_tokens_total{token_type="cache_read"}[5m])
)
/
clamp_min(
  sum by (cluster, job, channel_id) (
    rate(newapi_channel_tokens_total{token_type="input"}[5m])
  ),
  0.000001
)
```

## 7. Grafana 信息架构

### 7.1 文件夹

```text
new-api 监控
├── 主机总览
├── 程序总览
├── 中间件总览
└── 渠道总览

new-api 扩展监控
├── 计费总览
└── 任务总览
```

Grafana provisioning 使用两个 provider，每个 provider 使用独立目录和固定文件夹。渠道、计费和任务总览沿用原 UID；主机、程序和中间件总览使用新 UID。原 `newapi-system-overview` 在内容拆分完成后退役，避免同时出现新旧两套系统页面。

### 7.2 主机总览

数据源：Node Exporter。

- CPU 使用率及 user/system/iowait 等模式趋势。
- 1/5/15 分钟 Load。
- 内存已用、可用、缓存和 Swap。
- 每个有效挂载点的磁盘容量与使用率。
- 磁盘读写吞吐、IOPS 和 I/O 耗时。
- 默认排除 tmpfs、overlay、容器伪文件系统和只读无关挂载点。

### 7.3 程序总览

数据源：new-api `/metrics`。

- 顶部实例表：实例、Go 堆内存分配、协程数、进程 CPU 使用核数。
- Go 堆内存分配趋势和 30 分钟增长趋势。
- 协程数量趋势和 30 分钟增长趋势。
- 进程 CPU 使用核数趋势。
- 多实例时按 `instance` 拆分，不与主机 CPU 百分比混用。

### 7.4 中间件总览

PostgreSQL 区域：

- Exporter 可用性。
- 数据库连接数与最大连接数占用。
- 事务提交/回滚速率。
- Buffer/Block Cache 命中率。
- 锁、死锁和数据库容量。

MySQL 区域：

- Exporter 可用性。
- 当前连接、Threads running 和最大连接占用。
- QPS/事务速率。
- InnoDB Buffer Pool 命中率。
- 慢查询、连接错误和数据库容量。

Redis 区域：

- Exporter 可用性。
- 内存使用、最大内存和碎片率。
- 连接客户端与拒绝连接。
- 命令 OPS。
- Keyspace 命中率。
- 驱逐 Key、过期 Key 和数据库 Key 数量。

未启用的数据库 Exporter 显示“未启用/暂无数据”，不显示伪造的 0。

### 7.5 渠道总览

#### 区域一：渠道实时状态表

每行一个渠道，支持按列排序：

- 状态
- 实时 RPM
- 失败 RPM
- 重试 RPM
- 成功率
- 超时率
- 并发
- 请求耗时 P90
- 请求耗时 P95
- TTFT P95
- 上游缓存命中率
- 上游首字节延迟 P95

#### 区域二：流量与稳定性

- 实时 RPM
- 成功率与失败 RPM
- 重试 RPM
- 渠道并发

所有趋势均按渠道分线。

#### 区域三：性能与缓存

- 请求耗时 P90/P95
- TTFT P95
- 上游缓存命中率
- 上游首字节延迟 P95

#### 区域四：错误与重试诊断

- 按渠道和固定 `error_type` 展示错误分布。
- 按渠道和固定 `reason` 展示重试原因排行。
- 流式中断通过 `stream="true"` 且失败的 attempt 计算。
- 客户端取消单独使用 `client_cancelled` 展示。

#### 区域五：Token 吞吐

- 输入 Token/分钟。
- 输出 Token/分钟。
- 缓存命中 Token/分钟。
- 按渠道分组展示。

## 8. 部署迁移

1. 先增加 Exporter、Prometheus scrape job 和应用指标。
2. 验证 Prometheus targets 全部健康，并确认真实业务流量产生渠道 TTFT/Token 序列。
3. 再启用 recording rules。
4. 最后切换 Grafana provisioning 和目录结构。
5. 当前服务器启用 Node、PostgreSQL、Redis Exporter；MySQL Exporter 保持关闭。
6. Grafana 切换后通过 API 验证两个文件夹和六个 Dashboard UID。

迁移过程中不得删除 Prometheus TSDB、Grafana 数据卷或现有告警配置。

## 9. 测试与验收

### 9.1 Go 测试

- 渠道 TTFT 只记录成功流式请求。
- 渠道 Token Counter 对 input/output/cache_read 精确计数。
- 同一请求不会重复记录 Usage。
- 无 Usage、无渠道 ID、监控关闭和非法数值路径安全 no-op。
- Prometheus registry 不发生重复注册。

### 9.2 配置测试

- `promtool check config`。
- `promtool check rules --lint=all --lint-fatal`。
- `promtool test rules`。
- `docker compose config` 覆盖 PostgreSQL 默认模式和 MySQL profile。
- 所有 Dashboard JSON 通过 `jq empty`。
- Grafana provisioning YAML 可解析。

### 9.3 线上验收

- Node、PostgreSQL、Redis 和 new-api targets 为 UP。
- Grafana 容器健康。
- “new-api 监控”只包含四个核心仪表盘。
- “new-api 扩展监控”包含计费和任务总览。
- 真实请求后，至少两个渠道可在实时表中分别显示 RPM、延迟、TTFT 和 Token 数据。
- PromQL 聚合后仍保留 `channel_id`，不得把多渠道误合并为单个总量。

## 10. 已确认的产品决定

- 使用四个独立核心仪表盘。
- 系统级指标统计整台服务器，不增加 cAdvisor。
- 渠道实时指标使用 1 分钟窗口。
- 渠道上游缓存命中率使用缓存 Token/输入 Token。
- PostgreSQL 和 MySQL 面板同时预置，但只启动当前使用的数据库 Exporter。
- 主渠道页采用表格优先、多渠道横向比较、分区趋势与诊断面板。
- 计费和任务总览保留并移动到扩展监控文件夹。
