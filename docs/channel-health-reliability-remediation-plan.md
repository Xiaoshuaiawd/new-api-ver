# 渠道转发可靠性与健康选路优化方案

## 背景与目标

当前渠道运行时健康、流式转发和重试逻辑已具备基础隔离、恢复、平滑加权及可观测性能力，但流式响应的下游写入时机、跨尝试状态复用和 Redis 健康门禁热路径仍存在一致性与性能风险。

本方案优先保证以下不变量：

1. 在尚未确认上游响应结果前，不向流式客户端写入会锁定 HTTP 状态的内容。
2. 一旦下游开始发送有业务语义的数据，不再重试，也不追加普通 JSON 错误响应。
3. 每一次渠道尝试都有独立的首字、响应计数和时延状态；请求级计费、身份和请求元数据继续复用。
4. 单次选路对同一渠道只读取一次运行时健康结果，Redis 不成为候选扫描的 miss 热路径。
5. 健康统计、隔离范围、亲和缓存及选路权重使用一致且可解释的状态口径。

## 当前问题

### P0：流式 Ping 可能提前锁定 HTTP 200

`relay/channel/api_request.go` 的 `doRequest()` 会在 `client.Do()` 前启动 Ping 保活。`relay/helper/common.go` 的 `PingData()` 会直接写出并 Flush 下游响应；此时 HTTP 状态已被提交为 200。

后续上游若返回 401、余额不足或 5xx，网关将无法返回正确 HTTP 状态。同时还会出现以下连锁问题：

- `controller/relay.go` 的 `shouldRetry()` 未检查下游是否已经输出响应；
- 最外层错误处理仍可能尝试追加普通 JSON 错误；
- distributor 仅按最终 Writer 状态小于 400 记录亲和关系，可能将实际失败渠道重新写入亲和缓存。

### P0：重试时首字状态没有重置

所有渠道重试共用同一个 `RelayInfo`，而 `isFirstResponse` 仅在初始化时设为 `true`。例如：

```text
渠道 A 返回 500 响应头
-> SetFirstResponseTime()
-> isFirstResponse=false
-> 重试渠道 B
-> B 的 SetFirstResponseTime() 不再生效
```

因此，渠道 B 的真实首字不会进入健康统计，指标可能继续使用渠道 A 的时间；`ReceivedResponseCount` 等尝试级状态也可能跨渠道残留。

### P0：健康选路存在 Redis miss 热路径

Redis 开启后，`pkg/cachex/hybrid_cache.go` 的 `HybridCache.Get()` 会绕过内存缓存。选路扫描每个候选渠道时都会检查运行时健康，健康门禁又优先读取隔离缓存。

健康渠道通常没有隔离 Key，导致每次读取都是 Redis miss。权重计算还可能再次读取健康状态，使同一次选路产生约 `2 x 候选渠道数` 次 Redis 请求。

### P1：Cohere、Cloudflare 未使用统一首字回调

以下适配器直接赋值 `FirstResponseTime`，未调用 `SetFirstResponseTime()`：

- `relay/channel/cohere/relay-cohere.go`
- `relay/channel/cloudflare/relay_cloudflare.go`

渠道健康模块因此收不到首字通知；开启 stuck 检测后，正常请求可能被误判为无首字。

### P1：健康窗口每次请求均为 O(n) 清理

`service/channel_health.go` 在每次请求完成时扫描整个样本切片并清理窗口外数据。高 QPS 渠道在 180 秒窗口中可能累积大量样本，锁内扫描成本会持续增长。

### P1：平滑加权和选择追踪存在全局竞争

`model/channel_cache.go` 的 `runtimeSmoothSelection` 为全局 Mutex，所有分组、模型和渠道共用。状态超过 10000 条后直接清空，会造成瞬时选路抖动。

`service/channel_selection_trace.go` 的全局聚合 Map 也有同类问题：超过 2000 条后，每新增高基数 Key 都可能重新排序整个 Map。

### P1：亲和缓存按渠道清理时扫描整个 Redis 命名空间

`service/channel_affinity.go` 的 `ClearChannelAffinityByChannelID()` 会扫描全部反向索引 Key，再逐条读取。大量亲和关系或批量隔离时，延迟和 Redis 负载会随命名空间规模增长。

### P1：多 Key 渠道按整个 channelID 隔离

单个 Key 返回 401 或余额不足时，当前逻辑会隔离整个多 Key 渠道，其他可用 Key 也会停止使用。

### P1：首字时间口径不统一

当前至少混用了以下时间点：

- distributor 的请求开始时间；
- 渠道健康 attempt 开始时间；
- 读取到上游数据的时间；
- 下游真正 Flush 数据的时间。

当前 `SetFirstResponseTime()` 在读取到上游数据后、写入下游前调用，语义更接近上游首事件，而非客户端收到数据的真实 TTFT。

### P2：慢首字降权是硬阈值，P95 未参与选路

平均首字达到 18 秒时直接减半，17.9 秒则不处理，容易在阈值边界来回抖动。P95 当前仅用于展示，少量极慢请求可能被平均值掩盖。

## 目标状态模型

为每次渠道尝试建立显式生命周期。以下字段均属于 attempt 级状态：

| 字段 | 含义 | 写入时机 |
| --- | --- | --- |
| `upstream_headers_received` | 已收到上游 HTTP 响应头 | `client.Do()` 成功返回响应后 |
| `downstream_ack_sent` | 已向下游发送无业务语义的 SSE ACK | 确认上游 2xx 后 |
| `downstream_semantic_started` | 已向下游发送业务语义内容 | 首个有效 SSE/JSON 数据 Flush 后 |
| `upstream_first_event_at` | 收到首个上游事件的时间 | 首次读取有效上游事件时 |
| `downstream_first_data_at` | 首次向下游 Flush 任意内容的时间 | ACK 或数据首次 Flush 后 |
| `downstream_first_semantic_chunk_at` | 首次向下游 Flush 业务语义内容的时间 | 首个有效数据块 Flush 后 |

### 重试与错误输出规则

1. 仅当 `downstream_semantic_started=false` 时允许重试。
2. 已发送 ACK 但尚未发送语义数据时，可以重试；此时错误以 SSE error event 或流结束表达，不再尝试写普通 JSON。
3. 已发送语义数据时，禁止重试，禁止追加普通 JSON；记录渠道失败、结束流并保留协议一致的错误信息。
4. 仅在 `upstream_headers_received=true` 且响应为 2xx 后，允许发送一次 SSE 注释 ACK。禁止在 `client.Do()` 前启动会写下游数据的 Ping。
5. 亲和关系仅在本次 attempt 成功完成且未产生渠道错误时写入；不得仅根据 Writer 的最终 HTTP 状态判断成功。

## 实施方案

### P0.1：流式响应生命周期与安全重试

1. 在 `RelayInfo` 中新增 attempt 生命周期字段，并提供线程安全的读写方法。
2. 将 Ping 保活的启动时机移至上游响应头确认且状态为 2xx 之后；不再在 `client.Do()` 前写下游。
3. 首次发送 SSE ACK 时只发送一次注释帧，并记录 `downstream_ack_sent` 和 `downstream_first_data_at`。
4. 首个业务语义块成功 Flush 后设置 `downstream_semantic_started` 和 `downstream_first_semantic_chunk_at`。
5. `shouldRetry()` 首先检查本 attempt 的 `downstream_semantic_started`；已开始时直接返回 `false`。
6. 最外层错误处理根据生命周期选择输出：未写下游时输出正常 HTTP/JSON 错误；已写流时仅输出协议内错误或关闭流。
7. distributor 的亲和写入改为使用显式 attempt 成功结果，而不是 `Writer.Status() < 400`。

### P0.2：每次重试重置 attempt 状态

在 `RelayInfo` 中新增：

```go
func (info *RelayInfo) BeginRelayAttempt(startedAt time.Time)
```

每次选定渠道并完成上下文绑定后调用。该方法必须重置至少以下字段：

- `isFirstResponse`；
- `FirstResponseTime` 及首字相关标记；
- `ReceivedResponseCount`；
- 生命周期状态及各时间戳；
- 本次 attempt 的渠道错误和完成结果。

请求级字段不得重置，包括请求 ID、用户、令牌、原始模型、计费上下文、请求开始总时间及跨尝试日志信息。日志和健康事件需携带 `attempt_index` 与实际 `channel_id`，以便区分失败尝试与最终尝试。

### P0.3：健康快照 L1 与单次选路复用

1. 为隔离快照增加进程内 L1：隔离正缓存按远端 TTL 或不超过数秒的 TTL 缓存；不存在的健康 Key 使用 1 至 3 秒负缓存。
2. Redis 仍作为跨实例真源；通过 Pub/Sub 或隔离版本号使本地 L1 在状态变更后快速失效。
3. 每次选路开始构建 `ChannelHealthSnapshot` 映射，候选门禁、有效权重和选择追踪复用同一份快照。
4. 将同一轮选路的单渠道健康查询限制为一次，避免门禁和权重各自读取。
5. 监控 L1 命中、负缓存命中、Redis hit/miss、快照构建耗时和单次选路 Redis 请求数。

### P1.1：统一首字回调与指标口径

1. Cohere、Cloudflare 及其他适配器禁止直接赋值 `FirstResponseTime`，统一调用 `SetFirstResponseTime()` 或新建的语义明确方法。
2. 将指标拆分并分别落入日志、健康统计和监控：

```text
gateway_processing_ms
upstream_headers_ms
upstream_first_event_ms
downstream_first_flush_ms
downstream_first_semantic_chunk_ms
```

3. 健康模块的慢首字和 stuck 判定使用 `upstream_first_event_ms`；面向用户体验的 TTFT 使用 `downstream_first_semantic_chunk_ms`。
4. 为各渠道适配器增加覆盖测试，确保读取首个有效上游事件时恰好触发一次统一回调。

### P1.2：健康样本改为时间桶

采用 1 秒时间桶替代逐请求切片扫描。每个渠道、健康 Scope 与窗口维护固定数量桶，桶内至少聚合：

- 成功数与失败数；
- 首字样本数、首字总和；
- 慢首字样本数；
- 用于 P95 的固定分桶直方图；
- 最近事件和尝试计数。

窗口滚动时仅推进和清空过期桶，聚合复杂度由每请求 O(n) 降为 O(1) 或受固定桶数约束。为控制内存，使用固定容量且可回收的 channel/Scope 状态。

### P1.3：分片平滑加权与选择追踪

1. 将平滑加权状态按 `group + model` 分片，每片独立锁和带 TTL 的有界 LRU。
2. 淘汰最久未使用的状态，禁止超过阈值后整体清空。
3. 将选择追踪计数器按哈希分片；使用固定容量 LRU 管理高基数 Key。
4. 排序和报表聚合转移至定时异步任务，选路热路径只进行 O(1) 计数更新。

### P1.4：亲和反向索引与多 Key 健康 Scope

亲和关系改用每渠道 Redis Set：

```text
channel_affinity_reverse:{channel_id}
```

写入亲和关系时同时将亲和 Key 写入该集合。隔离时执行 `SMEMBERS`，通过 pipeline 删除亲和 Key 和集合本身，避免扫描全命名空间；清理必须处理过期成员和并发写入。

运行时健康 Scope 扩展为：

```text
channelID + keyFingerprint + model
```

鉴权失败、余额不足等 Key 级故障优先隔离对应 Key。仅当全部可用 Key 均不可用，或渠道级网络/协议故障满足条件时，才隔离整个渠道。不得记录原始 Key，`keyFingerprint` 必须为不可逆且稳定的短指纹。

### P2：平滑首字降权与尾延迟保护

权重由硬阈值改为分段函数，并为恢复引入滞回：

| 首字表现 | 建议权重系数 |
| --- | --- |
| 小于 12 秒 | 100% |
| 12 至 18 秒 | 80% |
| 18 至 30 秒 | 50% |
| 大于 30 秒 | 20% |

降权阈值建议为 18 秒，恢复阈值建议为 14 秒。除平均首字外，满足以下任一条件时触发降权：

- 平均首字达到阈值；
- P95 首字达到更高阈值；
- 慢请求比例达到阈值。

具体阈值应配置化，并在健康事件中记录触发指标与最终权重系数。

## 建议实施顺序

- [x] P0：建立流式响应生命周期，禁止发送语义内容后重试或追加普通 JSON。
- [x] P0：每次渠道重试调用 `BeginRelayAttempt(startedAt)`，重置 attempt 级首字与响应状态。
- [x] P0：健康选路增加 L1 正缓存和负缓存；一次选路仅构建一次健康快照。
- [x] P1：上游确认 2xx 后发送一次 SSE 注释 ACK，并独立记录下游首包时间。
- [x] P1：修复 Cohere、Cloudflare 的首字回调，并增加适配器覆盖测试。
- [x] P1：健康样本替换为时间桶或固定容量环形缓冲。
- [x] P1：平滑加权锁和选择追踪锁改为分片有界状态。
- [x] P1：多 Key 健康隔离细化到 Key Scope。
- [x] P1：亲和缓存使用按渠道维护的反向 Redis Set。
- [x] P2：首字降权改为平滑区间，并引入 P95 与慢请求比例。

## 验收标准

### 流式响应与重试

- 上游在首个下游写入前返回 401、余额不足或 5xx 时，客户端收到对应 HTTP 状态，不产生 SSE Ping。
- 上游 2xx 后发送 ACK，再在首个业务块前失败时，不追加普通 JSON，不写入亲和缓存；重试行为与流协议一致。
- 已发送首个业务语义块后发生渠道错误时，不再重试，不追加 JSON，且错误日志带有 attempt 与生命周期状态。
- 渠道 A 失败、渠道 B 重试成功时，渠道 B 的首字独立进入健康统计；A、B 的响应计数和时延不互相污染。

### 缓存与选路性能

- 健康渠道的重复选路在 L1 命中后不产生 Redis GET。
- 单次选路中每个候选渠道最多读取一次健康快照。
- 隔离、恢复在多实例间于预设同步窗口内失效本地 L1。
- 平滑加权和选择追踪在高基数负载下不发生全局清空或全局排序热路径。

### 健康统计与隔离

- Cohere、Cloudflare 的有效首个上游事件均触发一次统一首字回调。
- 时间桶在窗口滚动后不保留过期样本，聚合结果与受控测试样本一致。
- 单 Key 鉴权或余额故障不影响同渠道其他可用 Key；渠道级故障仍能触发渠道隔离。
- 隔离时仅删除目标渠道对应的亲和关系，不扫描无关 Redis 命名空间。
- 权重决策可由平均首字、P95、慢请求比例和滞回状态复现。

## 回归测试建议

```text
go test ./relay/common ./relay/channel/... -run 'Test.*FirstResponse|Test.*Stream|Test.*Retry' -count=1
go test ./controller -run 'Test.*Retry|Test.*Relay' -count=1
go test ./service -run 'TestChannelHealth|TestChannelAffinity|TestChannelSelection|TestChannelSelectionTrace' -count=1
go test ./model -run 'Test.*Channel|Test.*Ability' -count=1
git diff --check
```

## 风险控制

- 生命周期字段进入并发读写路径前，先明确锁粒度或原子访问方式，避免 Ping、转发和错误处理之间出现竞态。
- SSE ACK 仅用于在已确认上游成功后维持连接，不得代替真实业务首字指标。
- 引入 Key Scope 前，先梳理渠道密钥轮询、模型映射和错误分类路径，避免将渠道级故障误降级为单 Key 故障。
- Redis Pub/Sub 只能加速失效，正确性仍依赖 TTL、版本校验和 Redis 真源读取回退。
- 时间桶、LRU 和缓存 TTL 均需通过配置或常量集中管理，并暴露命中率、淘汰、桶数量和聚合误差监控。

## 结论

最先处理流式响应状态机和重试首字重置。在此之前直接增加“假首字 ACK”会放大 HTTP 状态、重试、亲和缓存和首字统计之间的不一致。完成 P0 后，再推进健康快照缓存与 P1 的统计、并发和 Scope 优化。

## 实施验证

已执行并通过：

```text
go test ./service ./model ./relay/common ./relay/helper ./relay/channel/... ./controller ./middleware ./pkg/cachex -count=1
go test ./service ./model ./relay/common ./pkg/cachex -race -run 'Test(ChannelHealth|ChannelSelection|ChannelRuntime|RelayInfo|HybridCache)' -count=1
git diff --check
```
