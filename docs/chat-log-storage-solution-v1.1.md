# 聊天接口调用数据存储实施方案（V1.1）

## 1. 文档信息

- 文档名称：聊天接口调用数据存储实施方案
- 版本：V1.1
- 状态：可评审/可开发
- 适用项目：`new-api`（Go + Gin + GORM，支持 SQLite/MySQL/PostgreSQL）
- 目标：在不显著影响主链路时延的前提下，实现统一、完整、可追溯、可分析的聊天调用数据存储体系

## 2. 现状与差距

当前系统已有 `model.Log` 与 `RecordConsumeLog/RecordErrorLog`，但主要用于额度与错误统计，不满足本需求的以下关键点：

1. 未统一保存所有厂商完整请求/响应体。
2. 现有日志写入以同步 DB 写为主，不是“主链路只投递事件”的异步链路。
3. 无“成功/失败分表 + 按日分表”机制。
4. 无标准化 `normalized_response` 主分析口径。
5. 对流式数据缺少“chunk 聚合成最终非流式结果”的统一存储。
6. 无严格的 `request_id` 幂等去重写入规范。

结论：应新增独立的 `chat log` 子系统，和现有额度日志并存，避免直接侵入原有统计口径。

## 3. 目标与范围

### 3.1 覆盖模型

- OpenAI
- Claude
- Gemini
- Grok
- 未来新增厂商（通过协议适配器扩展）

### 3.2 覆盖数据

- 原始请求体 `raw_request`（完整）
- 原始响应体 `raw_response`（完整，流式场景为最终聚合结果 + 可选原始 chunk）
- tokens 统计
- tool 定义、调用参数、调用结果
- 统一时间线 `merged_timeline`
- 统一结构 `normalized_response`
- 主检索文本 `final_answer_text`

### 3.3 关键约束

1. 主链路不得同步写 DB。
2. 入库不得截断。
3. 成功失败分表，且按日分表。
4. 流式必须自动转非流存储。
5. 三数据库兼容（SQLite/MySQL/PostgreSQL）。
6. 所有落库时间与分表日期统一采用 `Asia/Shanghai`。

### 3.4 时间基准（强约束）

为避免跨天错表和统计口径不一致，时间口径统一如下：

1. 系统时区基准固定为 `Asia/Shanghai`（IANA 时区名）。
2. `created_at`、`created_date`、分表后缀 `YYYYMMDD` 均按上海时间计算。
3. 分表日界线为上海自然日：`00:00:00` 到 `23:59:59.999`。
4. 每日预建表任务的触发时刻按上海时间（建议 `23:55 Asia/Shanghai`）。
5. 查询时间筛选（`start/end`）默认按上海时间解释；若外部传 UTC，需先转换到上海时间再路由分表。
6. 保留/归档任务按上海日历日执行，不使用 UTC 日切。
7. 禁止同一链路混用 `time.Local/UTC/Asia/Shanghai`，统一使用 `Asia/Shanghai`。

## 4. 总体架构

```mermaid
flowchart LR
A[API/Relay 主链路] --> B[ChatLogEvent Producer]
B --> C[(Redis Stream/Kafka)]
C --> D[ChatLog Consumer Group]
D --> E[协议标准化]
E --> F[流式聚合转非流]
F --> G[成功/失败分流]
G --> H[(chat_log_success_YYYYMMDD)]
G --> I[(chat_log_fail_YYYYMMDD)]
D --> J[(DLQ)]
```

说明：

1. 主链路只负责组装并投递 `ChatLogEvent`，立即返回业务响应。
2. Consumer 负责重活：标准化、聚合、分表路由、幂等写入。
3. 失败重试后进入 DLQ，支持告警与回放。

## 5. 逻辑设计

### 5.1 统一事件模型 ChatLogEvent

建议事件字段分三层：

1. 元数据层
- `event_id`
- `request_id`
- `provider_request_id`
- `provider`
- `model_name`
- `user_id`
- `channel_id`
- `conversation_id`
- `message_id`
- `message_round`
- `created_at`
- `time_zone`（固定为 `Asia/Shanghai`，用于审计与跨系统对账）
- `is_stream`
- `is_tools`

2. 内容层
- `raw_request`
- `raw_response`（非流为原始完整响应；流式为聚合后完整响应）
- `stream_chunks`（可选审计）
- `tool_trace`
- `merged_timeline`
- `normalized_response`
- `final_answer_text`

3. 运行层
- `status_code`
- `error_code`
- `error_message`
- `latency_ms`
- `prompt_tokens`
- `completion_tokens`
- `total_tokens`
- `payload_bytes`
- `payload_sha256`
- `storage_ref`（超大包对象存储引用）

### 5.2 主链路接入点

按当前代码结构，建议从以下入口采集事件：

1. `controller/relay.go`
- `Relay`：覆盖 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/v1/audio/*`、`/v1/images/*`、`/v1/embeddings`、`/v1/rerank`、`/v1/realtime`、`/v1beta/models/*path`。
- `RelayTask` / `RelayTaskFetch`：覆盖视频、Suno、Kling、Jimeng。
- `RelayMidjourney`：覆盖 MJ 行为与任务查询。

2. `controller/playground.go`
- `Playground`：用户态调试请求也进入同一事件体系。

3. Producer 投递策略
- 请求进入后记录“开始事件”（含 raw_request）。
- 响应结束后补齐“完成事件”（含 tokens、raw_response、状态）。
- 流式通过增量事件 + 完成事件汇总。

### 5.3 异步队列方案

默认方案：`Redis Stream`（项目已有 Redis 依赖，落地成本最低）。

- Stream：`chat_log_events`
- Consumer Group：`chat_log_cg`
- Consumer：`chat_log_consumer_<node_id>`
- DLQ Stream：`chat_log_events_dlq`

重试策略：

1. 可重试错误（连接失败、建表瞬时失败、锁冲突）按指数退避重试。
2. 超过 `max_retry` 写入 DLQ。
3. DLQ 保留原事件 + 错误原因 + 重试次数 + 最后时间。

可插拔：后续可加 Kafka 适配器，但不作为 P0。

### 5.4 成功/失败判定

成功：

1. HTTP 2xx 且响应结构合法。
2. 流式收到完成标记并聚合成功。

失败：

1. 非 2xx 或协议解析失败。
2. 流式异常中断。
3. 上游超时/网络错误。
4. 客户端主动取消（单独 `error_code=client_cancelled`）。

落表规则：

- 成功 -> `chat_log_success_YYYYMMDD`
- 失败 -> `chat_log_fail_YYYYMMDD`

### 5.5 流式转非流算法

目标：`is_stream=1` 的请求最终必须产生可检索的非流式结果。

统一状态机字段：

- `stream_state`: collecting | finished | failed
- `chunk_seq`
- `assistant_text_buffer`
- `tool_calls_buffer`
- `timeline_buffer`

聚合规则：

1. 按 `chunk_seq` 顺序拼接文本。
2. tool call 的 `name/arguments` 增量片段按 `tool_call_id` 合并。
3. 收到 finish 事件后输出：
- `normalized_response`
- `final_answer_text`
- `tool_calls_merged`
- `stream_merged=1`
4. 异常中断：
- 保留已聚合部分
- 写入 `error_code/error_message`
- 写入 fail 分表

厂商差异适配：

1. OpenAI/Grok：按 `choices[].delta` 与 `tool_calls` 规则聚合。
2. Claude：按 `content_block_*`、`message_delta` 聚合。
3. Gemini：按 `candidates[].content.parts` 聚合。
4. 统一输出 `normalized_response`，查询分析只用该字段和 `final_answer_text`。

### 5.6 幂等与去重

幂等键优先级：

1. `request_id`
2. `provider_request_id`
3. `payload_sha256 + created_at(秒级窗口)`（兜底）

数据库约束：

- 唯一索引：`uniq_request_id(request_id)`

写入策略：

- 使用 GORM `clause.OnConflict` 做跨库 Upsert，避免手写数据库方言 SQL。

### 5.7 无截断与大包策略

1. JSON 大字段统一用 `TEXT/LONGTEXT` 存储（跨库兼容优先）。
2. 入库前计算 `payload_bytes` 与 `payload_sha256`。
3. 当 payload 超阈值（建议 512KB 或 1MB）时：
- 原文落对象存储（S3/OSS）
- DB 存 `storage_ref + sha256 + bytes`
- 逻辑上仍视为“完整保存”

## 6. 数据库设计

### 6.1 分表命名

- `chat_log_success_YYYYMMDD`
- `chat_log_fail_YYYYMMDD`

说明：`YYYYMMDD` 必须按 `Asia/Shanghai` 计算，不按 UTC 计算。

### 6.2 字段定义

保留你规格中的必填字段 + 增强字段，建议如下：

```text
id BIGINT PK
user_id VARCHAR(64) NOT NULL
created_at DATETIME(3) NOT NULL
created_date DATE NOT NULL
time_zone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai'
conversation_id VARCHAR(64) NOT NULL
model_name VARCHAR(128) NOT NULL
message_id VARCHAR(64) NOT NULL
channel_id VARCHAR(64) NOT NULL
prompt_tokens INT NOT NULL DEFAULT 0
completion_tokens INT NOT NULL DEFAULT 0
total_tokens INT NOT NULL DEFAULT 0
is_stream TINYINT(1) NOT NULL
message_round INT NOT NULL
is_tools TINYINT(1) NOT NULL
provider VARCHAR(32) NOT NULL
request_id VARCHAR(64) NOT NULL
provider_request_id VARCHAR(128) NULL
status_code INT NULL
error_code VARCHAR(64) NULL
error_message TEXT NULL
latency_ms INT NULL
raw_request LONGTEXT/TEXT NOT NULL
raw_response LONGTEXT/TEXT NULL
merged_timeline LONGTEXT/TEXT NULL
tool_trace LONGTEXT/TEXT NULL
stream_chunks LONGTEXT/TEXT NULL
normalized_response LONGTEXT/TEXT NULL
final_answer_text LONGTEXT/TEXT NULL
tool_calls_merged LONGTEXT/TEXT NULL
stream_merged TINYINT(1) NOT NULL DEFAULT 0
payload_bytes BIGINT NULL
payload_sha256 CHAR(64) NULL
storage_ref VARCHAR(512) NULL
```

说明：

1. 为兼容三库，JSON 建议统一存文本字段（序列化/反序列化走 `common/json.go` 包装）。
2. `created_at` 与 `created_date` 统一使用上海时间语义。
3. `created_date` 用于快速分区路由和离线统计。

### 6.3 索引

必备索引：

1. `uniq_request_id (request_id)`
2. `idx_user_time (user_id, created_at)`
3. `idx_conv_round (conversation_id, message_round)`
4. `idx_message_id (message_id)`
5. `idx_channel_time (channel_id, created_at)`
6. `idx_model_time (model_name, created_at)`

可选：

- `final_answer_text` 全文索引（按数据库能力分别实现）

### 6.4 分表创建机制

1. 启动时创建“今天 + 明天”分表。
2. 每日定时任务预建后一天分表。
3. 写入时发现缺表，执行兜底创建。
4. 建表失败触发告警并重试。
5. 以上“今天/明天/每日”均以 `Asia/Shanghai` 为准。

跨库实现建议：

- 使用 GORM Migrator + 动态 `TableName` 创建表。
- 避免依赖 MySQL 分区表、PG 继承分区等数据库特性，保证一致行为。

### 6.5 三数据库时区落地建议

1. Go 应用层：
- 启动时加载 `loc := time.LoadLocation("Asia/Shanghai")` 并在 chatlog 子系统统一使用 `time.Now().In(loc)`。
- 分表路由、`created_at`、`created_date` 全部从同一 `loc` 派生。

2. MySQL：
- 连接参数确保 `parseTime=true`，并显式设置 `loc=Asia%2FShanghai`。
- 建议连接后执行会话时区设置（如 `SET time_zone = '+08:00'`），避免实例默认时区不一致。

3. PostgreSQL：
- DSN 增加 `TimeZone=Asia/Shanghai`（或连接后 `SET TIME ZONE 'Asia/Shanghai'`）。
- 确保写入与查询会话时区一致。

4. SQLite：
- SQLite 无会话时区能力，按应用层写入上海时间字符串/时间值。
- 查询侧按上海时区解释与展示。

## 7. 与现有代码的落地改造清单

建议新增模块：

1. `dto/chat_log_event.go`
- 定义 `ChatLogEvent`、`NormalizedResponse`、`TimelineItem`。

2. `service/chatlog/producer.go`
- 负责从请求上下文生成并投递事件。

3. `service/chatlog/queue_redis_stream.go`
- Redis Stream 生产者/消费者封装。

4. `service/chatlog/consumer.go`
- 消费、重试、DLQ、分表写入调度。

5. `service/chatlog/normalizer/*.go`
- 按 provider 标准化请求/响应。

6. `service/chatlog/stream_merge/*.go`
- 流式 chunk 聚合器（OpenAI/Claude/Gemini/Grok）。

7. `model/chat_log_partition.go`
- 分表模型、建表/路由、幂等 upsert。

8. `model/chat_log_registry.go`
- 可选：记录分表元信息与建表状态。

9. `controller/relay.go`
- 在 `Relay/RelayTask/RelayMidjourney` 接入 producer 钩子。

10. `controller/playground.go`
- 补充 playground 场景事件投递。

11. `setting/operation_setting/*`
- 增加配置项：开关、阈值、重试、DLQ、对象存储阈值等。

## 8. 配置项建议

```text
CHAT_LOG_ENABLED=true
CHAT_LOG_QUEUE=redis_stream
CHAT_LOG_STREAM_KEY=chat_log_events
CHAT_LOG_DLQ_KEY=chat_log_events_dlq
CHAT_LOG_CONSUMER_GROUP=chat_log_cg
CHAT_LOG_CONSUMER_START_ID=0
CHAT_LOG_MAX_RETRY=8
CHAT_LOG_FLUSH_INTERVAL_MS=500
CHAT_LOG_MAX_BATCH=200
CHAT_LOG_CONSUMER_WORKERS=8
CHAT_LOG_LOCAL_WORKERS=2
CHAT_LOG_STREAM_MAX_LEN=200000
CHAT_LOG_DLQ_MAX_LEN=50000
CHAT_LOG_PAYLOAD_INLINE_MAX_BYTES=1048576
CHAT_LOG_PARTITION_PRECREATE_DAYS=1
CHAT_LOG_RETENTION_DAYS=180
CHAT_LOG_TIMEZONE=Asia/Shanghai
LOG_SQL_DSN=postgresql://user:pass@host:5432/new-api
MES_SQL_DSN=postgresql://user:pass@host:5432/new-api
```

说明：
1. `LOG_SQL_DSN` 用于常规 `logs` 表。
2. `MES_SQL_DSN` 仅用于聊天数据分表 `chat_log_success_YYYYMMDD / chat_log_fail_YYYYMMDD`。

## 9. 安全与合规

1. 敏感字段脱敏：token、手机号、证件号、密钥、Authorization。
2. 存储加密：传输 TLS + 静态加密。
3. 权限控制：仅管理员可查询原始 payload。
4. 审计：查询操作记录审计日志。
5. 生命周期：在线 180 天 + 归档策略。

## 10. 观测与运维

核心指标：

1. `chatlog_event_ingress_qps`
2. `chatlog_consume_success_rate`
3. `chatlog_consume_latency_p95/p99`
4. `chatlog_retry_count`
5. `chatlog_dlq_size`
6. `chatlog_partition_create_success_rate`
7. `chatlog_stream_merge_success_rate`

告警：

1. 入库失败率超阈值。
2. DLQ 持续增长。
3. 分表创建失败。
4. 流式聚合异常率升高。

## 11. 测试与验收

### 11.1 功能验收（对应你的 UAT）

1. OpenAI/Claude/Gemini/Grok 均可入库。
2. 成功/失败分流正确。
3. 流式请求 `stream_merged=1` 且产出 `normalized_response`。
4. `conversation_id + message_round` 可完整回放多轮与 tools。
5. 抽样校验 `payload_bytes + payload_sha256` 一致。
6. 跨天场景（上海时间 `23:59:59` 到次日 `00:00:01`）分表路由准确，无错表。

### 11.2 性能验收

1. 主链路增量延迟：P99 < 10ms。
2. 99% 事件 60s 内入库。
3. 压测下无明显请求吞吐下降。

## 12. 里程碑（P0 首期）

M1（数据与基础设施）

1. 建立 `ChatLogEvent` 模型。
2. Redis Stream + Consumer + DLQ 打通。
3. 成功/失败日分表建表器。

M2（主链路接入）

1. `Relay/RelayTask/RelayMidjourney/Playground` 接入 Producer。
2. 采集请求、响应、tokens、错误上下文。

M3（标准化与流转非）

1. OpenAI/Grok 适配。
2. Claude 适配。
3. Gemini 适配。
4. `normalized_response` 与 `final_answer_text` 固化。

M4（稳定性与验收）

1. 重试和 DLQ 回放工具。
2. 监控与告警接入。
3. 全量 UAT 与压测。

## 13. 风险与规避

1. 风险：流式协议差异导致聚合错误。
- 规避：按 provider 单独适配器 + 回归样本库。

2. 风险：分表动态创建在高峰期抖动。
- 规避：预创建 + 缓存 + 兜底创建重试。

3. 风险：大 payload 影响 DB IO。
- 规避：阈值外置对象存储 + DB 仅存引用。

4. 风险：至少一次消费导致重复写入。
- 规避：`request_id` 唯一键 + Upsert 幂等。

## 14. 需求映射（FR/NFR）

| 需求项 | 方案落点 |
|---|---|
| FR-01 统一采集 | `ChatLogEvent` 统一模型 + `controller/relay.go` 全入口采集 |
| FR-02 必填字段 | 6.2 字段定义包含全部必填字段 |
| FR-03 异步存储 | 5.3 Redis Stream + Consumer + DLQ |
| FR-04 无截断 | 5.7 大字段文本存储 + 对象存储外置 + 哈希校验 |
| FR-05 按日期分表 | 6.1 命名规范 + 6.4 预建/兜底建表 + 3.4 上海时区日切规则 |
| FR-06 成功失败分流 | 5.4 成功/失败判定与路由 |
| FR-07 多轮与 tools | 5.1 `merged_timeline` + `tool_trace` + `message_round` |
| FR-08 流转非 | 5.5 聚合状态机 + `stream_merged` |
| FR-09 幂等去重 | 5.6 `request_id` 唯一键 + Upsert |
| NFR-性能 | 11.2 验收指标（P99 < 10ms） |
| NFR-可靠性 | 5.3 重试 + DLQ + 回放 |
| NFR-安全性 | 9 脱敏、加密、RBAC |
| NFR-可观测性 | 10 指标与告警 |

## 15. 结论

该方案可在现有 `new-api` 架构上渐进落地，满足你规格中的全部 P0 要求：

1. 统一采集。
2. 异步存储。
3. 无截断。
4. 成功/失败按日分表。
5. 流式自动转非流。
6. 幂等去重。

并且与项目约束保持一致：

1. 三数据库兼容。
2. JSON 统一序列化可走 `common/json.go`。
3. 不阻塞主调用链路。
