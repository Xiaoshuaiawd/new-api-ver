# 渠道运行时健康与自动负载均衡优化 Todo

## 目标

把当前运行时健康检查从“基础隔离”升级为“自动检测、自动恢复、平滑负载、可解释排查”的闭环。核心目标是低成本优先，同时避免慢首字、错误率、陈旧运行态导致误隔离或恢复后反复隔离。

## P0：防误杀和卡死

- [x] 增加运行态自愈清理：定期清理已取消、超时过久、无法再 finish 的 inflight attempt，避免内存状态永久污染。
- [x] 手动恢复时重置运行态：清理 inflight、probeInProgress、probeBackoff、stuck 标记和窗口失败样本，确保恢复后不会被历史状态重新隔离。
- [x] stuck inflight 去重：同一批 stuck attempt 只能触发一次隔离，后续检查不应重复刷新 openedAt、nextProbeAt 或事件。
- [x] 多实例一致性兜底：Redis 隔离快照与本机内存状态不一致时，本机选择逻辑应优先采用可恢复、可过期的状态，不让旧快照永久压住渠道。

## P1：自动恢复质量

- [x] 恢复判定升级：从“固定 probe 成功次数”升级为“连续成功 + 错误率正常 + 首响正常”的组合条件。
- [x] 预热动态升流：warming 期间根据真实成功率、慢首字比例、当前 inflight 动态调整放流比例，异常时回退或暂停升流。
- [x] probe 失败原因分类：余额不足、鉴权失败、模型不存在、限流、上游 5xx、网络超时走不同 backoff 和恢复策略。
- [x] 最大隔离时间兜底：open 超过配置时间且没有新失败样本时，强制进入 probe 或 warming，避免长期不恢复。

## P1：负载均衡质量

- [x] 选择权重加入健康评分：综合价格倍率、错误率、首响延迟、inflight、probe/warmup 状态，而不是只按 priority/weight/inflight。
- [x] 同优先级桶内做平滑负载：降低短时间随机偏斜，避免单渠道被突发打满。
- [x] 降级渠道保留探测流量：被降权或刚恢复的渠道保留少量真实流量，用真实请求验证稳定性。
- [x] DB 路径和内存缓存路径统一健康选择逻辑：避免 `model/channel_cache.go` 与 `model/ability.go` 两套路由策略漂移。

## P2：可观测性与排查

- [x] 健康事件记录完整快照：触发时的 active inflight、stuck count、样本数、失败数、错误率、首响均值/P95、probeBackoff。
- [x] 渠道详情显示不可用原因：runtime unavailable、probe lock、nextProbeAt、warming percent、manual disabled 等原因要可解释。
- [x] 增加健康状态时间线：open -> probing -> warming -> healthy 全链路可追踪。
- [x] 增加选择链路聚合报表：统计各渠道被选中、被跳过、因健康降级、因优先级降级的次数。

## 当前实施批次

本批实施 P0 + P1 + P2 的后端核心能力和新版 UI 可观测性：

1. [x] 运行态自愈清理。
2. [x] 手动恢复彻底重置运行态。
3. [x] stuck 去重和隔离时间不被重复刷新。
4. [x] probe 失败原因分类和最大隔离时间兜底。
5. [x] warming 动态升流的后端基础评分。
6. [x] 健康评分接入同优先级桶权重。
7. [x] Redis 隔离快照过期/到期后可被本机恢复 probe。
8. [x] probe 恢复前校验窗口错误率和首响延迟。
9. [x] 同优先级桶内平滑权重选择。
10. [x] 降级渠道保留少量 due probe 真实流量。
11. [x] DB 路径和内存缓存路径复用同一套运行时候选选择策略。
12. [x] 健康事件携带触发时完整快照。
13. [x] 运行时健康快照输出可用性和不可用原因。
14. [x] 健康报告输出 open -> probing -> warming -> recovered 时间线。
15. [x] 健康报告输出选路聚合统计。

## 验证计划

- `go test ./service -run 'TestChannelHealth|TestCacheGetRandomSatisfiedChannel|TestChannelSelection|TestChannelAffinity' -count=1`
- `go test ./model -run 'Test.*Channel|Test.*Ability' -count=1`
- `git diff --check`
