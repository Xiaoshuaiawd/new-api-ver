# new-api 代码与接口全景分析（源码扫描版）

> 生成时间：2026-04-13 18:02:13
> 扫描范围：`main.go`、`router/*.go`、`controller/*.go`、`service/*.go`、`model/*.go`、`middleware/*.go`
> 路由总数：**320**（含兼容路由与 `NoRoute`）
> 项目架构说明：见 `docs/project-architecture.md`
> AI 调用接口专题：见 `docs/project-architecture.md` 的「10. AI 调用接口分析（客户端调用入口）」

## 1. 架构与调用链

请求主链路：`router -> middleware -> controller -> service -> model -> DB/Upstream`。

关键入口：
- `main.go`：初始化配置/数据库/Redis/i18n/OAuth，启动 Gin，挂载全局中间件与路由。
- `router/main.go`：统一注册 `API + Dashboard + Relay + Video + Web` 五大路由域。
- `router/api-router.go`：控制台/用户/管理后台核心接口。
- `router/relay-router.go`：OpenAI/Claude/Gemini/MJ/Suno 等中继协议兼容接口。
- `router/video-router.go`：视频生成与视频代理。

目录职责速览：
- `router/`：路由分组与中间件编排。
- `controller/`：参数校验、鉴权上下文读取、HTTP 响应封装。
- `service/`：业务规则、配额计费、渠道选择、第三方 API 协调。
- `model/`：GORM 数据访问层与缓存。
- `relay/`：上游协议适配器与请求转发。
- `middleware/`：认证、限流、分发、性能保护、统计。

## 2. 中间件策略（高频）

- 鉴权：`UserAuth`、`AdminAuth`、`RootAuth`、`TokenAuth`、`TokenAuthReadOnly`、`TokenOrUserAuth`。
- 频控：`GlobalAPIRateLimit`、`CriticalRateLimit`、`SearchRateLimit`、`ModelRequestRateLimit`。
- Relay 核心：`SystemPerformanceCheck` + `Distribute`（渠道选择与上下游上下文注入）。
- 安全：`TurnstileCheck`、`SecureVerificationRequired`、`DisableCache`（敏感密钥读取）。

## 3. 路由统计

### 3.1 按方法

| 方法 | 数量 |
|---|---:|
| DELETE | 26 |
| GET | 135 |
| NOROUTE | 1 |
| PATCH | 1 |
| POST | 141 |
| PUT | 16 |

### 3.2 按鉴权类型

| 鉴权 | 数量 |
|---|---:|
| Admin | 117 |
| Public | 40 |
| Root | 22 |
| Token | 81 |
| TokenOrUser | 1 |
| TokenReadOnly | 2 |
| User | 57 |

### 3.3 按顶级路径

| 顶级路径段 | 数量 |
|---|---:|
| (root) | 1 |
| :mode | 16 |
| api | 234 |
| dashboard | 2 |
| jimeng | 1 |
| kling | 4 |
| mj | 16 |
| pg | 1 |
| suno | 3 |
| v1 | 39 |
| v1beta | 3 |

## 4. 热点处理函数（按路由挂载次数）

| Handler | 路由数 |
|---|---:|
| controller.RelayMidjourney | 30 |
| controller.RelayNotImplemented | 12 |
| controller.RelayTask | 7 |
| controller.RelayTaskFetch | 6 |
| inline: controller.Relay(c, types.RelayFormatGemini) | 3 |
| inline: controller.Relay(c, types.RelayFormatOpenAI) | 3 |
| inline: controller.Relay(c, types.RelayFormatOpenAIAudio) | 3 |
| inline: controller.Relay(c, types.RelayFormatOpenAIImage) | 3 |
| controller.EpayNotify | 2 |
| controller.GetSubscription | 2 |
| controller.GetUsage | 2 |
| controller.GetUserGroups | 2 |
| controller.SubscriptionEpayNotify | 2 |
| controller.SubscriptionEpayReturn | 2 |
| controller.TestIoNetConnection | 2 |
| inline handler | 2 |
| relay.RelayMidjourneyImage | 2 |
| controller.AddChannel | 1 |
| controller.AddRedemption | 1 |
| controller.AddToken | 1 |
| controller.Admin2FAStats | 1 |
| controller.AdminBindSubscription | 1 |
| controller.AdminClearUserBinding | 1 |
| controller.AdminCompleteTopUp | 1 |
| controller.AdminCreateSubscriptionPlan | 1 |
| controller.AdminCreateUserSubscription | 1 |
| controller.AdminDeleteUserSubscription | 1 |
| controller.AdminDisable2FA | 1 |
| controller.AdminInvalidateUserSubscription | 1 |
| controller.AdminListSubscriptionPlans | 1 |

## 5. /api 管理接口清单（234）

| Method | Path | 子域 | Auth | Handler | Router定义 | Controller定义 | 关键依赖(service/model/relay) |
|---|---|---|---|---|---|---|---|
| GET | /api/about | about | Public | controller.GetAbout | api-router.go:30 | misc.go:180 | - |
| GET | /api/channel/ | channel | Admin | controller.GetAllChannels | api-router.go:209 | channel.go:71 | model.CountAllTags, model.GetChannelsByTag, model.GetPaginatedTags |
| POST | /api/channel/ | channel | Admin | controller.AddChannel | api-router.go:219 | channel.go:566 | model.BatchInsertChannels, service.ResetProxyClientCache |
| PUT | /api/channel/ | channel | Admin | controller.UpdateChannel | api-router.go:220 | channel.go:842 | model.GetChannelById, model.InitChannelCache, service.ResetProxyClientCache |
| DELETE | /api/channel/:id | channel | Admin | controller.DeleteChannel | api-router.go:225 | channel.go:666 | model.InitChannelCache |
| GET | /api/channel/:id | channel | Admin | controller.GetChannel | api-router.go:213 | channel.go:361 | model.GetChannelById |
| POST | /api/channel/:id/codex/oauth/complete | channel | Admin | controller.CompleteCodexOAuthForChannel | api-router.go:233 | codex_oauth.go:117 | - |
| POST | /api/channel/:id/codex/oauth/start | channel | Admin | controller.StartCodexOAuthForChannel | api-router.go:232 | codex_oauth.go:66 | - |
| POST | /api/channel/:id/codex/refresh | channel | Admin | controller.RefreshCodexChannelCredential | api-router.go:234 | channel.go:495 | service.RefreshCodexChannelCredential |
| GET | /api/channel/:id/codex/usage | channel | Admin | controller.GetCodexChannelUsage | api-router.go:235 | codex_usage.go:20 | model.GetChannelById, model.InitChannelCache, service.FetchCodexWhamUsage, service.NewProxyHttpClient, service.RefreshCodexOAuthTokenWithProxy, service.ResetProxyClientCache |
| POST | /api/channel/:id/key | channel | Root | controller.GetChannelKey | api-router.go:214 | channel.go:385 | model.GetChannelById, model.RecordLog |
| POST | /api/channel/batch | channel | Admin | controller.DeleteChannelBatch | api-router.go:226 | channel.go:812 | model.BatchDeleteChannels, model.InitChannelCache |
| POST | /api/channel/batch/tag | channel | Admin | controller.BatchSetChannelTag | api-router.go:240 | channel.go:1093 | model.BatchSetChannelTag, model.InitChannelCache |
| POST | /api/channel/codex/oauth/complete | channel | Admin | controller.CompleteCodexOAuth | api-router.go:231 | codex_oauth.go:113 | - |
| POST | /api/channel/codex/oauth/start | channel | Admin | controller.StartCodexOAuth | api-router.go:230 | codex_oauth.go:62 | - |
| POST | /api/channel/copy/:id | channel | Admin | controller.CopyChannel | api-router.go:242 | channel.go:1164 | model.BatchInsertChannels, model.GetChannelById, model.InitChannelCache |
| DELETE | /api/channel/disabled | channel | Admin | controller.DeleteDisabledChannel | api-router.go:221 | channel.go:682 | model.DeleteDisabledChannel, model.InitChannelCache |
| POST | /api/channel/fetch_models | channel | Root | controller.FetchModels | api-router.go:229 | channel.go:973 | - |
| GET | /api/channel/fetch_models/:id | channel | Admin | controller.FetchUpstreamModels | api-router.go:228 | channel.go:203 | model.GetChannelById |
| POST | /api/channel/fix | channel | Admin | controller.FixChannelsAbilities | api-router.go:227 | channel.go:232 | model.FixAbility |
| GET | /api/channel/models | channel | Admin | controller.ChannelListModels | api-router.go:211 | model.go:243 | - |
| GET | /api/channel/models_enabled | channel | Admin | controller.EnabledListModels | api-router.go:212 | model.go:257 | model.GetEnabledModels |
| POST | /api/channel/multi_key/manage | channel | Admin | controller.ManageMultiKeys | api-router.go:243 | channel.go:1242 | model.GetChannelById, model.GetChannelPollingLock, model.InitChannelCache |
| DELETE | /api/channel/ollama/delete | channel | Admin | controller.OllamaDeleteModel | api-router.go:238 | channel.go:1846 | model.GetChannelById |
| POST | /api/channel/ollama/pull | channel | Admin | controller.OllamaPullModel | api-router.go:236 | channel.go:1701 | model.GetChannelById |
| POST | /api/channel/ollama/pull/stream | channel | Admin | controller.OllamaPullModelStream | api-router.go:237 | channel.go:1764 | model.GetChannelById |
| GET | /api/channel/ollama/version/:id | channel | Admin | controller.OllamaVersion | api-router.go:239 | channel.go:1909 | model.GetChannelById |
| GET | /api/channel/search | channel | Admin | controller.SearchChannels | api-router.go:210 | channel.go:248 | model.GetChannelsByTag, model.SearchChannels, model.SearchTags |
| PUT | /api/channel/tag | channel | Admin | controller.EditTagChannels | api-router.go:224 | channel.go:755 | model.EditChannelByTag, model.InitChannelCache |
| POST | /api/channel/tag/disabled | channel | Admin | controller.DisableTagChannels | api-router.go:222 | channel.go:709 | model.DisableChannelByTag, model.InitChannelCache |
| POST | /api/channel/tag/enabled | channel | Admin | controller.EnableTagChannels | api-router.go:223 | channel.go:732 | model.EnableChannelByTag, model.InitChannelCache |
| GET | /api/channel/tag/models | channel | Admin | controller.GetTagModels | api-router.go:241 | channel.go:1117 | model.GetChannelsByTag |
| GET | /api/channel/test | channel | Admin | controller.TestAllChannels | api-router.go:215 | channel-test.go:866 | - |
| GET | /api/channel/test/:id | channel | Admin | controller.TestChannel | api-router.go:216 | channel-test.go:735 | model.CacheGetChannel, model.GetChannelById |
| GET | /api/channel/update_balance | channel | Admin | controller.UpdateAllChannelsBalance | api-router.go:217 | channel-billing.go:484 | - |
| GET | /api/channel/update_balance/:id | channel | Admin | controller.UpdateChannelBalance | api-router.go:218 | channel-billing.go:424 | model.CacheGetChannel |
| POST | /api/channel/upstream_updates/apply | channel | Admin | controller.ApplyChannelUpstreamModelUpdates | api-router.go:244 | channel_upstream_update.go:663 | model.GetChannelById |
| POST | /api/channel/upstream_updates/apply_all | channel | Admin | controller.ApplyAllChannelUpstreamModelUpdates | api-router.go:245 | channel_upstream_update.go:827 | - |
| POST | /api/channel/upstream_updates/detect | channel | Admin | controller.DetectChannelUpstreamModelUpdates | api-router.go:246 | channel_upstream_update.go:716 | model.GetChannelById |
| POST | /api/channel/upstream_updates/detect_all | channel | Admin | controller.DetectAllChannelUpstreamModelUpdates | api-router.go:247 | channel_upstream_update.go:908 | - |
| POST | /api/creem/webhook | creem | Public | controller.CreemWebhook | api-router.go:50 | topup_creem.go:231 | - |
| GET | /api/custom-oauth-provider/ | custom-oauth-provider | Root | controller.GetCustomOAuthProviders | api-router.go:184 | custom_oauth.go:73 | model.GetAllCustomOAuthProviders |
| POST | /api/custom-oauth-provider/ | custom-oauth-provider | Root | controller.CreateCustomOAuthProvider | api-router.go:186 | custom_oauth.go:214 | model.CreateCustomOAuthProvider, model.IsSlugTaken |
| DELETE | /api/custom-oauth-provider/:id | custom-oauth-provider | Root | controller.DeleteCustomOAuthProvider | api-router.go:188 | custom_oauth.go:403 | model.DeleteCustomOAuthProvider, model.GetBindingCountByProviderId, model.GetCustomOAuthProviderById |
| GET | /api/custom-oauth-provider/:id | custom-oauth-provider | Root | controller.GetCustomOAuthProvider | api-router.go:185 | custom_oauth.go:93 | model.GetCustomOAuthProviderById |
| PUT | /api/custom-oauth-provider/:id | custom-oauth-provider | Root | controller.UpdateCustomOAuthProvider | api-router.go:187 | custom_oauth.go:292 | model.GetCustomOAuthProviderById, model.IsSlugTaken, model.UpdateCustomOAuthProvider |
| POST | /api/custom-oauth-provider/discovery | custom-oauth-provider | Root | controller.FetchCustomOAuthDiscovery | api-router.go:183 | custom_oauth.go:142 | - |
| GET | /api/data/ | data | Admin | controller.GetAllQuotaDates | api-router.go:295 | usedata.go:13 | model.GetAllQuotaDates |
| GET | /api/data/self | data | User | controller.GetUserQuotaDates | api-router.go:297 | usedata.go:45 | model.GetQuotaDataByUserId |
| GET | /api/data/users | data | Admin | controller.GetQuotaDatesByUser | api-router.go:296 | usedata.go:30 | model.GetQuotaDataGroupByUser |
| GET | /api/deployments/ | deployments | Admin | controller.GetAllDeployments | api-router.go:359 | deployment.go:206 | - |
| POST | /api/deployments/ | deployments | Admin | controller.CreateDeployment | api-router.go:367 | deployment.go:494 | - |
| DELETE | /api/deployments/:id | deployments | Admin | controller.DeleteDeployment | api-router.go:376 | deployment.go:469 | - |
| GET | /api/deployments/:id | deployments | Admin | controller.GetDeployment | api-router.go:369 | deployment.go:296 | - |
| PUT | /api/deployments/:id | deployments | Admin | controller.UpdateDeployment | api-router.go:373 | deployment.go:400 | - |
| GET | /api/deployments/:id/containers | deployments | Admin | controller.ListDeploymentContainers | api-router.go:371 | deployment.go:706 | - |
| GET | /api/deployments/:id/containers/:container_id | deployments | Admin | controller.GetContainerDetails | api-router.go:372 | deployment.go:761 | - |
| POST | /api/deployments/:id/extend | deployments | Admin | controller.ExtendDeployment | api-router.go:375 | deployment.go:430 | - |
| GET | /api/deployments/:id/logs | deployments | Admin | controller.GetDeploymentLogs | api-router.go:370 | deployment.go:646 | - |
| PUT | /api/deployments/:id/name | deployments | Admin | controller.UpdateDeploymentName | api-router.go:374 | deployment.go:345 | - |
| GET | /api/deployments/available-replicas | deployments | Admin | controller.GetAvailableReplicas | api-router.go:364 | deployment.go:564 | - |
| GET | /api/deployments/check-name | deployments | Admin | controller.CheckClusterNameAvailability | api-router.go:366 | deployment.go:621 | - |
| GET | /api/deployments/hardware-types | deployments | Admin | controller.GetHardwareTypes | api-router.go:362 | deployment.go:520 | - |
| GET | /api/deployments/locations | deployments | Admin | controller.GetLocations | api-router.go:363 | deployment.go:540 | - |
| POST | /api/deployments/price-estimation | deployments | Admin | controller.GetPriceEstimation | api-router.go:365 | deployment.go:600 | - |
| GET | /api/deployments/search | deployments | Admin | controller.SearchDeployments | api-router.go:360 | deployment.go:243 | - |
| GET | /api/deployments/settings | deployments | Admin | controller.GetModelDeploymentSettings | api-router.go:357 | deployment.go:28 | - |
| POST | /api/deployments/settings/test-connection | deployments | Admin | controller.TestIoNetConnection | api-router.go:358 | deployment.go:58 | - |
| POST | /api/deployments/test-connection | deployments | Admin | controller.TestIoNetConnection | api-router.go:361 | deployment.go:58 | - |
| GET | /api/group/ | group | Admin | controller.GetGroups | api-router.go:306 | group.go:14 | - |
| GET | /api/home_page_content | home_page_content | Public | controller.GetHomePageContent | api-router.go:32 | misc.go:220 | - |
| DELETE | /api/log/ | log | Admin | controller.DeleteHistoryLogs | api-router.go:286 | log.go:151 | model.DeleteOldLog |
| GET | /api/log/ | log | Admin | controller.GetAllLogs | api-router.go:285 | log.go:13 | model.GetAllLogs |
| GET | /api/log/channel_affinity_usage_cache | log | Admin | controller.GetChannelAffinityUsageCacheStats | api-router.go:289 | channel_affinity_cache.go:62 | service.GetChannelAffinityUsageCacheStats |
| GET | /api/log/search | log | Admin | controller.SearchAllLogs | api-router.go:290 | log.go:57 | - |
| GET | /api/log/self | log | User | controller.GetUserLogs | api-router.go:291 | log.go:35 | model.GetUserLogs |
| GET | /api/log/self/search | log | User | controller.SearchUserLogs | api-router.go:292 | log.go:65 | - |
| GET | /api/log/self/stat | log | User | controller.GetLogsSelfStat | api-router.go:288 | log.go:123 | model.SumUsedQuota |
| GET | /api/log/stat | log | Admin | controller.GetLogsStat | api-router.go:287 | log.go:96 | model.SumUsedQuota |
| GET | /api/log/token | log | TokenReadOnly | controller.GetLogByKey | api-router.go:301 | log.go:72 | model.GetLogByTokenId |
| GET | /api/mj/ | mj | Admin | controller.GetAllMidjourney | api-router.go:320 | midjourney.go:257 | model.CountAllTasks, model.GetAllTasks |
| GET | /api/mj/self | mj | User | controller.GetUserMidjourney | api-router.go:319 | midjourney.go:282 | model.CountAllUserTask, model.GetAllUserTask |
| GET | /api/models | models | User | controller.DashboardListModels | api-router.go:25 | model.go:250 | - |
| GET | /api/models/ | models | Admin | controller.GetAllModelsMeta | api-router.go:345 | model_meta.go:17 | model.GetAllModels, model.GetVendorModelCounts |
| POST | /api/models/ | models | Admin | controller.CreateModelMeta | api-router.go:348 | model_meta.go:81 | model.IsModelNameDuplicated, model.RefreshPricing |
| PUT | /api/models/ | models | Admin | controller.UpdateModelMeta | api-router.go:349 | model_meta.go:109 | model.IsModelNameDuplicated, model.RefreshPricing |
| DELETE | /api/models/:id | models | Admin | controller.DeleteModelMeta | api-router.go:350 | model_meta.go:148 | model.RefreshPricing |
| GET | /api/models/:id | models | Admin | controller.GetModelMeta | api-router.go:347 | model_meta.go:64 | - |
| GET | /api/models/missing | models | Admin | controller.GetMissingModels | api-router.go:344 | missing_models.go:14 | model.GetMissingModels |
| GET | /api/models/search | models | Admin | controller.SearchModelsMeta | api-router.go:346 | model_meta.go:45 | model.SearchModels |
| POST | /api/models/sync_upstream | models | Admin | controller.SyncUpstreamModels | api-router.go:343 | model_sync.go:268 | model.GetMissingModels |
| GET | /api/models/sync_upstream/preview | models | Admin | controller.SyncUpstreamPreview | api-router.go:342 | model_sync.go:499 | model.GetMissingModels |
| GET | /api/notice | notice | Public | controller.GetNotice | api-router.go:27 | misc.go:169 | - |
| GET | /api/oauth/:provider | oauth | Public | controller.HandleOAuth | api-router.go:46 | oauth.go:44 | - |
| POST | /api/oauth/email/bind | oauth | Public | controller.EmailBind | api-router.go:39 | user.go:978 | - |
| GET | /api/oauth/state | oauth | Public | controller.GenerateOAuthCode | api-router.go:38 | oauth.go:23 | - |
| GET | /api/oauth/telegram/bind | oauth | Public | controller.TelegramBind | api-router.go:44 | telegram.go:18 | model.IsTelegramIdAlreadyTaken |
| GET | /api/oauth/telegram/login | oauth | Public | controller.TelegramLogin | api-router.go:43 | telegram.go:72 | - |
| GET | /api/oauth/wechat | oauth | Public | controller.WeChatAuth | api-router.go:41 | wechat.go:56 | model.GetMaxUserId, model.IsWeChatIdAlreadyTaken |
| POST | /api/oauth/wechat/bind | oauth | Public | controller.WeChatBind | api-router.go:42 | wechat.go:129 | model.IsWeChatIdAlreadyTaken |
| GET | /api/option/ | option | Root | controller.GetOptions | api-router.go:171 | option.go:63 | - |
| PUT | /api/option/ | option | Root | controller.UpdateOption | api-router.go:172 | option.go:105 | model.UpdateOption |
| DELETE | /api/option/channel_affinity_cache | option | Root | controller.ClearChannelAffinityCache | api-router.go:174 | channel_affinity_cache.go:20 | service.ClearChannelAffinityCacheAll, service.ClearChannelAffinityCacheByRuleName |
| GET | /api/option/channel_affinity_cache | option | Root | controller.GetChannelAffinityCacheStats | api-router.go:173 | channel_affinity_cache.go:11 | service.GetChannelAffinityCacheStats |
| POST | /api/option/migrate_console_setting | option | Root | controller.MigrateConsoleSetting | api-router.go:176 | console_migrate.go:16 | model.AllOption, model.InitOptionMap, model.UpdateOption |
| POST | /api/option/rest_model_ratio | option | Root | controller.ResetModelRatio | api-router.go:175 | pricing.go:79 | model.UpdateOption |
| DELETE | /api/performance/disk_cache | performance | Root | controller.ClearDiskCache | api-router.go:194 | performance.go:143 | - |
| POST | /api/performance/gc | performance | Root | controller.ForceGC | api-router.go:196 | performance.go:169 | - |
| DELETE | /api/performance/logs | performance | Root | controller.CleanupLogFiles | api-router.go:198 | performance.go:268 | - |
| GET | /api/performance/logs | performance | Root | controller.GetLogFiles | api-router.go:197 | performance.go:232 | - |
| POST | /api/performance/reset_stats | performance | Root | controller.ResetPerformanceStats | api-router.go:195 | performance.go:159 | - |
| GET | /api/performance/stats | performance | Root | controller.GetPerformanceStats | api-router.go:193 | performance.go:83 | - |
| GET | /api/prefill_group/ | prefill_group | Admin | controller.GetPrefillGroups | api-router.go:312 | prefill_group.go:13 | model.GetAllPrefillGroups |
| POST | /api/prefill_group/ | prefill_group | Admin | controller.CreatePrefillGroup | api-router.go:313 | prefill_group.go:24 | model.IsPrefillGroupNameDuplicated |
| PUT | /api/prefill_group/ | prefill_group | Admin | controller.UpdatePrefillGroup | api-router.go:314 | prefill_group.go:51 | model.IsPrefillGroupNameDuplicated |
| DELETE | /api/prefill_group/:id | prefill_group | Admin | controller.DeletePrefillGroup | api-router.go:315 | prefill_group.go:78 | model.DeletePrefillGroupByID |
| GET | /api/pricing | pricing | Public | controller.GetPricing | api-router.go:33 | pricing.go:36 | model.GetPricing, model.GetSupportedEndpointMap, model.GetUserCache, model.GetVendors, service.GetUserAutoGroup, service.GetUserUsableGroups |
| GET | /api/privacy-policy | privacy-policy | Public | controller.GetPrivacyPolicy | api-router.go:29 | misc.go:200 | - |
| GET | /api/ratio_config | ratio_config | Public | controller.GetRatioConfig | api-router.go:47 | ratio_config.go:11 | - |
| GET | /api/ratio_sync/channels | ratio_sync | Root | controller.GetSyncableChannels | api-router.go:203 | ratio_sync.go:872 | model.GetAllChannels |
| POST | /api/ratio_sync/fetch | ratio_sync | Root | controller.FetchUpstreamRatios | api-router.go:204 | ratio_sync.go:70 | model.GetChannelById, model.GetChannelsByIds |
| GET | /api/redemption/ | redemption | Admin | controller.GetAllRedemptions | api-router.go:276 | redemption.go:15 | model.GetAllRedemptions |
| POST | /api/redemption/ | redemption | Admin | controller.AddRedemption | api-router.go:279 | redemption.go:61 | - |
| PUT | /api/redemption/ | redemption | Admin | controller.UpdateRedemption | api-router.go:280 | redemption.go:129 | model.GetRedemptionById |
| DELETE | /api/redemption/:id | redemption | Admin | controller.DeleteRedemption | api-router.go:282 | redemption.go:115 | model.DeleteRedemptionById |
| GET | /api/redemption/:id | redemption | Admin | controller.GetRedemption | api-router.go:278 | redemption.go:42 | model.GetRedemptionById |
| DELETE | /api/redemption/invalid | redemption | Admin | controller.DeleteInvalidRedemption | api-router.go:281 | redemption.go:168 | model.DeleteInvalidRedemptions |
| GET | /api/redemption/search | redemption | Admin | controller.SearchRedemptions | api-router.go:277 | redemption.go:28 | model.SearchRedemptions |
| GET | /api/reset_password | reset_password | Public | controller.SendPasswordResetEmail | api-router.go:35 | misc.go:302 | model.IsEmailAlreadyTaken |
| GET | /api/setup | setup | Public | controller.GetSetup | api-router.go:21 | setup.go:27 | model.RootUserExists |
| POST | /api/setup | setup | Public | controller.PostSetup | api-router.go:22 | setup.go:54 | model.RootUserExists, model.UpdateOption |
| GET | /api/status | status | Public | controller.GetStatus | api-router.go:23 | misc.go:42 | - |
| GET | /api/status/test | status | Admin | controller.TestStatus | api-router.go:26 | misc.go:23 | model.PingDB |
| POST | /api/stripe/webhook | stripe | Public | controller.StripeWebhook | api-router.go:49 | topup_stripe.go:148 | - |
| POST | /api/subscription/admin/bind | subscription | Admin | controller.AdminBindSubscription | api-router.go:154 | subscription.go:285 | model.AdminBindSubscription |
| GET | /api/subscription/admin/plans | subscription | Admin | controller.AdminListSubscriptionPlans | api-router.go:150 | subscription.go:91 | - |
| POST | /api/subscription/admin/plans | subscription | Admin | controller.AdminCreateSubscriptionPlan | api-router.go:151 | subscription.go:110 | model.InvalidateSubscriptionPlanCache, model.NormalizeResetPeriod |
| PATCH | /api/subscription/admin/plans/:id | subscription | Admin | controller.AdminUpdateSubscriptionPlanStatus | api-router.go:153 | subscription.go:261 | model.InvalidateSubscriptionPlanCache |
| PUT | /api/subscription/admin/plans/:id | subscription | Admin | controller.AdminUpdateSubscriptionPlan | api-router.go:152 | subscription.go:168 | model.InvalidateSubscriptionPlanCache, model.NormalizeResetPeriod |
| DELETE | /api/subscription/admin/user_subscriptions/:id | subscription | Admin | controller.AdminDeleteUserSubscription | api-router.go:160 | subscription.go:367 | model.AdminDeleteUserSubscription |
| POST | /api/subscription/admin/user_subscriptions/:id/invalidate | subscription | Admin | controller.AdminInvalidateUserSubscription | api-router.go:159 | subscription.go:348 | model.AdminInvalidateUserSubscription |
| GET | /api/subscription/admin/users/:id/subscriptions | subscription | Admin | controller.AdminListUserSubscriptions | api-router.go:157 | subscription.go:305 | model.GetAllUserSubscriptions |
| POST | /api/subscription/admin/users/:id/subscriptions | subscription | Admin | controller.AdminCreateUserSubscription | api-router.go:158 | subscription.go:324 | model.AdminBindSubscription |
| POST | /api/subscription/creem/pay | subscription | User | controller.SubscriptionRequestCreemPay | api-router.go:145 | subscription_payment_creem.go:21 | model.CountUserSubscriptionsByPlan, model.GetSubscriptionPlanById, model.GetUserById |
| GET | /api/subscription/epay/notify | subscription | Public | controller.SubscriptionEpayNotify | api-router.go:165 | subscription_payment_epay.go:114 | model.CompleteSubscriptionOrder |
| POST | /api/subscription/epay/notify | subscription | Public | controller.SubscriptionEpayNotify | api-router.go:164 | subscription_payment_epay.go:114 | model.CompleteSubscriptionOrder |
| POST | /api/subscription/epay/pay | subscription | User | controller.SubscriptionRequestEpay | api-router.go:143 | subscription_payment_epay.go:25 | model.CountUserSubscriptionsByPlan, model.ExpireSubscriptionOrder, model.GetSubscriptionPlanById, service.GetCallbackAddress |
| GET | /api/subscription/epay/return | subscription | Public | controller.SubscriptionEpayReturn | api-router.go:166 | subscription_payment_epay.go:169 | model.CompleteSubscriptionOrder |
| POST | /api/subscription/epay/return | subscription | Public | controller.SubscriptionEpayReturn | api-router.go:167 | subscription_payment_epay.go:169 | model.CompleteSubscriptionOrder |
| GET | /api/subscription/plans | subscription | User | controller.GetSubscriptionPlans | api-router.go:140 | subscription.go:26 | - |
| GET | /api/subscription/self | subscription | User | controller.GetSubscriptionSelf | api-router.go:141 | subscription.go:41 | model.GetAllActiveUserSubscriptions, model.GetAllUserSubscriptions, model.GetUserSetting |
| PUT | /api/subscription/self/preference | subscription | User | controller.UpdateSubscriptionPreference | api-router.go:142 | subscription.go:65 | model.GetUserById |
| POST | /api/subscription/stripe/pay | subscription | User | controller.SubscriptionRequestStripePay | api-router.go:144 | subscription_payment_stripe.go:24 | model.CountUserSubscriptionsByPlan, model.GetSubscriptionPlanById, model.GetUserById |
| GET | /api/task/ | task | Admin | controller.GetAllTask | api-router.go:325 | task.go:22 | model.TaskCountAllTasks, model.TaskGetAllTasks |
| GET | /api/task/self | task | User | controller.GetUserTask | api-router.go:324 | task.go:45 | model.TaskCountAllUserTask, model.TaskGetAllUserTask |
| GET | /api/token/ | token | User | controller.GetAllTokens | api-router.go:252 | token.go:34 | model.CountUserTokens, model.GetAllUserTokens |
| POST | /api/token/ | token | User | controller.AddToken | api-router.go:256 | token.go:167 | model.CountUserTokens |
| PUT | /api/token/ | token | User | controller.UpdateToken | api-router.go:257 | token.go:250 | model.GetTokenByIds |
| DELETE | /api/token/:id | token | User | controller.DeleteToken | api-router.go:258 | token.go:236 | model.DeleteTokenById |
| GET | /api/token/:id | token | User | controller.GetToken | api-router.go:254 | token.go:65 | model.GetTokenByIds |
| POST | /api/token/:id/key | token | User | controller.GetTokenKey | api-router.go:255 | token.go:80 | model.GetTokenByIds |
| POST | /api/token/batch | token | User | controller.DeleteTokenBatch | api-router.go:259 | token.go:319 | model.BatchDeleteTokens |
| POST | /api/token/batch/keys | token | User | controller.GetTokenKeysBatch | api-router.go:260 | token.go:338 | model.GetTokenKeysByIds |
| GET | /api/token/search | token | User | controller.SearchTokens | api-router.go:253 | token.go:48 | model.SearchUserTokens |
| GET | /api/uptime/status | uptime | Public | controller.GetUptimeKumaStatus | api-router.go:24 | uptime_kuma.go:131 | - |
| GET | /api/usage/token/ | usage | TokenReadOnly | controller.GetTokenUsage | api-router.go:269 | token.go:118 | model.GetTokenByKey |
| GET | /api/user/ | user | Admin | controller.GetAllUsers | api-router.go:116 | user.go:234 | model.GetAllUsers |
| POST | /api/user/ | user | Admin | controller.CreateUser | api-router.go:124 | user.go:804 | - |
| PUT | /api/user/ | user | Admin | controller.UpdateUser | api-router.go:126 | user.go:544 | model.GetUserById |
| POST | /api/user/2fa/backup_codes | user | User | controller.RegenerateBackupCodes | api-router.go:102 | twofa.go:313 | model.CreateBackupCodes, model.GetTwoFAByUserId, model.RecordLog |
| POST | /api/user/2fa/disable | user | User | controller.Disable2FA | api-router.go:101 | twofa.go:205 | model.DisableTwoFA, model.GetTwoFAByUserId, model.RecordLog |
| POST | /api/user/2fa/enable | user | User | controller.Enable2FA | api-router.go:100 | twofa.go:138 | model.GetTwoFAByUserId, model.RecordLog |
| POST | /api/user/2fa/setup | user | User | controller.Setup2FA | api-router.go:99 | twofa.go:34 | model.CreateBackupCodes, model.GetTwoFAByUserId, model.GetUserById, model.RecordLog |
| GET | /api/user/2fa/stats | user | Admin | controller.Admin2FAStats | api-router.go:131 | twofa.go:490 | model.GetTwoFAStats |
| GET | /api/user/2fa/status | user | User | controller.Get2FAStatus | api-router.go:98 | twofa.go:277 | model.GetTwoFAByUserId, model.GetUnusedBackupCodeCount |
| DELETE | /api/user/:id | user | Admin | controller.DeleteUser | api-router.go:127 | user.go:757 | model.GetUserById, model.HardDeleteUserById |
| GET | /api/user/:id | user | Admin | controller.GetUser | api-router.go:123 | user.go:265 | model.GetUserById |
| DELETE | /api/user/:id/2fa | user | Admin | controller.AdminDisable2FA | api-router.go:132 | twofa.go:505 | model.DisableTwoFA, model.GetUserById, model.RecordLog |
| DELETE | /api/user/:id/bindings/:binding_type | user | Admin | controller.AdminClearUserBinding | api-router.go:122 | user.go:587 | model.GetUserById, model.RecordLog |
| GET | /api/user/:id/oauth/bindings | user | Admin | controller.GetUserOAuthBindingsByAdmin | api-router.go:120 | custom_oauth.go:489 | model.GetUserById |
| DELETE | /api/user/:id/oauth/bindings/:provider_id | user | Admin | controller.UnbindCustomOAuthByAdmin | api-router.go:121 | custom_oauth.go:548 | model.DeleteUserOAuthBinding, model.GetUserById |
| DELETE | /api/user/:id/reset_passkey | user | Admin | controller.AdminResetPasskey | api-router.go:128 | passkey.go:329 | model.DeletePasskeyByUserID, model.GetPasskeyByUserID |
| GET | /api/user/aff | user | User | controller.GetAffCode | api-router.go:84 | user.go:348 | model.GetUserById |
| POST | /api/user/aff_transfer | user | User | controller.TransferAffQuota | api-router.go:94 | user.go:328 | model.GetUserById |
| POST | /api/user/amount | user | User | controller.RequestAmount | api-router.go:89 | topup_stripe.go:49 | model.GetUserGroup |
| GET | /api/user/checkin | user | User | controller.GetCheckinStatus | api-router.go:105 | checkin.go:16 | model.GetUserCheckinStats |
| POST | /api/user/checkin | user | User | controller.DoCheckin | api-router.go:106 | checkin.go:47 | model.RecordLog, model.UserCheckin |
| POST | /api/user/creem/pay | user | User | controller.RequestCreemPay | api-router.go:92 | topup_creem.go:145 | - |
| GET | /api/user/epay/notify | user | Public | controller.EpayNotify | api-router.go:66 | topup.go:283 | model.GetTopUpByTradeNo, model.IncreaseUserQuota, model.RecordLog |
| POST | /api/user/epay/notify | user | Public | controller.EpayNotify | api-router.go:65 | topup.go:283 | model.GetTopUpByTradeNo, model.IncreaseUserQuota, model.RecordLog |
| GET | /api/user/groups | user | Public | controller.GetUserGroups | api-router.go:67 | group.go:26 | model.GetUserGroup, service.GetUserGroupRatio, service.GetUserUsableGroups |
| POST | /api/user/login | user | Public | controller.Login | api-router.go:59 | user.go:32 | model.IsTwoFAEnabled |
| POST | /api/user/login/2fa | user | Public | controller.Verify2FALogin | api-router.go:60 | twofa.go:399 | model.GetTwoFAByUserId, model.GetUserById |
| GET | /api/user/logout | user | Public | controller.Logout | api-router.go:64 | user.go:119 | - |
| POST | /api/user/manage | user | Admin | controller.ManageUser | api-router.go:125 | user.go:851 | model.DecreaseUserQuota, model.IncreaseUserQuota, model.RecordLog |
| GET | /api/user/models | user | User | controller.GetUserModels | api-router.go:74 | user.go:517 | model.GetGroupEnabledModels, model.GetUserCache, service.GetUserUsableGroups |
| GET | /api/user/oauth/bindings | user | User | controller.GetUserOAuthBindings | api-router.go:109 | custom_oauth.go:469 | - |
| DELETE | /api/user/oauth/bindings/:provider_id | user | User | controller.UnbindCustomOAuth | api-router.go:110 | custom_oauth.go:523 | model.DeleteUserOAuthBinding |
| DELETE | /api/user/passkey | user | User | controller.PasskeyDelete | api-router.go:83 | passkey.go:144 | model.DeletePasskeyByUserID |
| GET | /api/user/passkey | user | User | controller.PasskeyStatus | api-router.go:78 | passkey.go:165 | model.GetPasskeyByUserID |
| POST | /api/user/passkey/login/begin | user | Public | controller.PasskeyLoginBegin | api-router.go:61 | passkey.go:203 | - |
| POST | /api/user/passkey/login/finish | user | Public | controller.PasskeyLoginFinish | api-router.go:62 | passkey.go:238 | model.GetPasskeyByCredentialID, model.NewPasskeyCredentialFromWebAuthn, model.UpsertPasskeyCredential |
| POST | /api/user/passkey/register/begin | user | User | controller.PasskeyRegisterBegin | api-router.go:79 | passkey.go:21 | model.GetPasskeyByUserID |
| POST | /api/user/passkey/register/finish | user | User | controller.PasskeyRegisterFinish | api-router.go:80 | passkey.go:81 | model.GetPasskeyByUserID, model.NewPasskeyCredentialFromWebAuthn, model.UpsertPasskeyCredential |
| POST | /api/user/passkey/verify/begin | user | User | controller.PasskeyVerifyBegin | api-router.go:81 | passkey.go:365 | model.GetPasskeyByUserID |
| POST | /api/user/passkey/verify/finish | user | User | controller.PasskeyVerifyFinish | api-router.go:82 | passkey.go:419 | model.GetPasskeyByUserID, model.UpsertPasskeyCredential |
| POST | /api/user/pay | user | User | controller.RequestEpay | api-router.go:88 | topup.go:166 | model.GetUserGroup, service.GetCallbackAddress |
| POST | /api/user/register | user | Public | controller.Register | api-router.go:58 | user.go:136 | model.CheckUserExistOrDeleted, model.GetUserIdByAffCode |
| POST | /api/user/reset | user | Public | controller.ResetPassword | api-router.go:36 | misc.go:336 | model.ResetUserPasswordByEmail |
| GET | /api/user/search | user | Admin | controller.SearchUsers | api-router.go:119 | user.go:249 | model.SearchUsers |
| DELETE | /api/user/self | user | User | controller.DeleteSelf | api-router.go:76 | user.go:783 | model.DeleteUserById, model.GetUserById |
| GET | /api/user/self | user | User | controller.GetSelf | api-router.go:73 | user.go:373 | model.GetUserById |
| PUT | /api/user/self | user | User | controller.UpdateSelf | api-router.go:75 | user.go:625 | model.GetUserById |
| GET | /api/user/self/groups | user | User | controller.GetUserGroups | api-router.go:72 | group.go:26 | model.GetUserGroup, service.GetUserGroupRatio, service.GetUserUsableGroups |
| PUT | /api/user/setting | user | User | controller.UpdateUserSetting | api-router.go:95 | user.go:1104 | model.GetUserById |
| POST | /api/user/stripe/amount | user | User | controller.RequestStripeAmount | api-router.go:91 | topup_stripe.go:128 | - |
| POST | /api/user/stripe/pay | user | User | controller.RequestStripePay | api-router.go:90 | topup_stripe.go:138 | - |
| GET | /api/user/token | user | User | controller.GenerateAccessToken | api-router.go:77 | user.go:289 | model.GetUserById |
| GET | /api/user/topup | user | Admin | controller.GetAllTopUps | api-router.go:117 | topup.go:420 | model.GetAllTopUps, model.SearchAllTopUps |
| POST | /api/user/topup | user | User | controller.TopUp | api-router.go:87 | user.go:1059 | model.Redeem |
| POST | /api/user/topup/complete | user | Admin | controller.AdminCompleteTopUp | api-router.go:118 | topup.go:449 | model.ManualCompleteTopUp |
| GET | /api/user/topup/info | user | User | controller.GetTopUpInfo | api-router.go:85 | topup.go:25 | - |
| GET | /api/user/topup/self | user | User | controller.GetUserTopUps | api-router.go:86 | topup.go:394 | model.GetUserTopUps, model.SearchUserTopUps |
| POST | /api/user/waffo/pay | user | User | controller.RequestWaffoPay | api-router.go:93 | topup_waffo.go:103 | model.GetUserById, model.GetUserGroup, service.GetCallbackAddress |
| GET | /api/user-agreement | user-agreement | Public | controller.GetUserAgreement | api-router.go:28 | misc.go:191 | - |
| GET | /api/vendors/ | vendors | Admin | controller.GetAllVendors | api-router.go:331 | vendor_meta.go:13 | model.GetAllVendors |
| POST | /api/vendors/ | vendors | Admin | controller.CreateVendorMeta | api-router.go:334 | vendor_meta.go:58 | model.IsVendorNameDuplicated |
| PUT | /api/vendors/ | vendors | Admin | controller.UpdateVendorMeta | api-router.go:335 | vendor_meta.go:85 | model.IsVendorNameDuplicated |
| DELETE | /api/vendors/:id | vendors | Admin | controller.DeleteVendorMeta | api-router.go:336 | vendor_meta.go:112 | - |
| GET | /api/vendors/:id | vendors | Admin | controller.GetVendorMeta | api-router.go:333 | vendor_meta.go:42 | model.GetVendorByID |
| GET | /api/vendors/search | vendors | Admin | controller.SearchVendors | api-router.go:332 | vendor_meta.go:28 | model.SearchVendors |
| GET | /api/verification | verification | Public | controller.SendEmailVerification | api-router.go:34 | misc.go:231 | model.IsEmailAlreadyTaken |
| POST | /api/verify | verify | User | controller.UniversalVerify | api-router.go:54 | secure_verification.go:37 | model.GetPasskeyByUserID, model.GetTwoFAByUserId, model.RecordLog |
| POST | /api/waffo/webhook | waffo | Public | controller.WaffoWebhook | api-router.go:51 | topup_waffo.go:289 | - |

## 6. Relay/Provider 兼容接口清单（81）

| Method | Path | 子域 | Auth | Handler | Router定义 | Controller定义 | 关键依赖(service/model/relay) |
|---|---|---|---|---|---|---|---|
| GET | /:mode/mj/image/:id | :mode | Public | relay.RelayMidjourneyImage | relay-router.go:204 | - | - |
| POST | /:mode/mj/insight-face/swap | :mode | Token | controller.RelayMidjourney | relay-router.go:221 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/action | :mode | Token | controller.RelayMidjourney | relay-router.go:207 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/blend | :mode | Token | controller.RelayMidjourney | relay-router.go:214 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/change | :mode | Token | controller.RelayMidjourney | relay-router.go:211 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/describe | :mode | Token | controller.RelayMidjourney | relay-router.go:213 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/edits | :mode | Token | controller.RelayMidjourney | relay-router.go:215 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/imagine | :mode | Token | controller.RelayMidjourney | relay-router.go:210 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/modal | :mode | Token | controller.RelayMidjourney | relay-router.go:209 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/shorten | :mode | Token | controller.RelayMidjourney | relay-router.go:208 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/simple-change | :mode | Token | controller.RelayMidjourney | relay-router.go:212 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/upload-discord-images | :mode | Token | controller.RelayMidjourney | relay-router.go:222 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/submit/video | :mode | Token | controller.RelayMidjourney | relay-router.go:216 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| GET | /:mode/mj/task/:id/fetch | :mode | Token | controller.RelayMidjourney | relay-router.go:218 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| GET | /:mode/mj/task/:id/image-seed | :mode | Token | controller.RelayMidjourney | relay-router.go:219 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /:mode/mj/task/list-by-condition | :mode | Token | controller.RelayMidjourney | relay-router.go:220 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /jimeng/ | jimeng | Token | controller.RelayTask | video-router.go:50 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| POST | /kling/v1/videos/image2video | kling | Token | controller.RelayTask | video-router.go:39 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| GET | /kling/v1/videos/image2video/:task_id | kling | Token | controller.RelayTaskFetch | video-router.go:41 | relay.go:464 | relay.RelayTaskFetch |
| POST | /kling/v1/videos/text2video | kling | Token | controller.RelayTask | video-router.go:38 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| GET | /kling/v1/videos/text2video/:task_id | kling | Token | controller.RelayTaskFetch | video-router.go:40 | relay.go:464 | relay.RelayTaskFetch |
| GET | /mj/image/:id | mj | Public | relay.RelayMidjourneyImage | relay-router.go:204 | - | - |
| POST | /mj/insight-face/swap | mj | Token | controller.RelayMidjourney | relay-router.go:221 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/action | mj | Token | controller.RelayMidjourney | relay-router.go:207 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/blend | mj | Token | controller.RelayMidjourney | relay-router.go:214 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/change | mj | Token | controller.RelayMidjourney | relay-router.go:211 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/describe | mj | Token | controller.RelayMidjourney | relay-router.go:213 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/edits | mj | Token | controller.RelayMidjourney | relay-router.go:215 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/imagine | mj | Token | controller.RelayMidjourney | relay-router.go:210 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/modal | mj | Token | controller.RelayMidjourney | relay-router.go:209 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/shorten | mj | Token | controller.RelayMidjourney | relay-router.go:208 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/simple-change | mj | Token | controller.RelayMidjourney | relay-router.go:212 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/upload-discord-images | mj | Token | controller.RelayMidjourney | relay-router.go:222 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/submit/video | mj | Token | controller.RelayMidjourney | relay-router.go:216 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| GET | /mj/task/:id/fetch | mj | Token | controller.RelayMidjourney | relay-router.go:218 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| GET | /mj/task/:id/image-seed | mj | Token | controller.RelayMidjourney | relay-router.go:219 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /mj/task/list-by-condition | mj | Token | controller.RelayMidjourney | relay-router.go:220 | relay.go:397 | relay.RelayMidjourneyNotify, relay.RelayMidjourneySubmit, relay.RelayMidjourneyTask, relay.RelayMidjourneyTaskImageSeed, relay.RelaySwapFace |
| POST | /pg/chat/completions | pg | User | controller.Playground | relay-router.go:67 | playground.go:15 | model.GetUserCache |
| POST | /suno/fetch | suno | Token | controller.RelayTaskFetch | relay-router.go:185 | relay.go:464 | relay.RelayTaskFetch |
| GET | /suno/fetch/:id | suno | Token | controller.RelayTaskFetch | relay-router.go:186 | relay.go:464 | relay.RelayTaskFetch |
| POST | /suno/submit/:action | suno | Token | controller.RelayTask | relay-router.go:184 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| POST | /v1/audio/speech | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIAudio) | relay-router.go:131 | - | - |
| POST | /v1/audio/transcriptions | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIAudio) | relay-router.go:125 | - | - |
| POST | /v1/audio/translations | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIAudio) | relay-router.go:128 | - | - |
| POST | /v1/chat/completions | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAI) | relay-router.go:96 | - | - |
| POST | /v1/completions | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAI) | relay-router.go:93 | - | - |
| POST | /v1/edits | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIImage) | relay-router.go:109 | - | - |
| POST | /v1/embeddings | v1 | Token | inline: controller.Relay(c, types.RelayFormatEmbedding) | relay-router.go:120 | - | - |
| POST | /v1/engines/:model/embeddings | v1 | Token | inline: controller.Relay(c, types.RelayFormatGemini) | relay-router.go:141 | - | - |
| GET | /v1/files | v1 | Token | controller.RelayNotImplemented | relay-router.go:155 | relay.go:440 | - |
| POST | /v1/files | v1 | Token | controller.RelayNotImplemented | relay-router.go:156 | relay.go:440 | - |
| DELETE | /v1/files/:id | v1 | Token | controller.RelayNotImplemented | relay-router.go:157 | relay.go:440 | - |
| GET | /v1/files/:id | v1 | Token | controller.RelayNotImplemented | relay-router.go:158 | relay.go:440 | - |
| GET | /v1/files/:id/content | v1 | Token | controller.RelayNotImplemented | relay-router.go:159 | relay.go:440 | - |
| GET | /v1/fine-tunes | v1 | Token | controller.RelayNotImplemented | relay-router.go:161 | relay.go:440 | - |
| POST | /v1/fine-tunes | v1 | Token | controller.RelayNotImplemented | relay-router.go:160 | relay.go:440 | - |
| GET | /v1/fine-tunes/:id | v1 | Token | controller.RelayNotImplemented | relay-router.go:162 | relay.go:440 | - |
| POST | /v1/fine-tunes/:id/cancel | v1 | Token | controller.RelayNotImplemented | relay-router.go:163 | relay.go:440 | - |
| GET | /v1/fine-tunes/:id/events | v1 | Token | controller.RelayNotImplemented | relay-router.go:164 | relay.go:440 | - |
| POST | /v1/images/edits | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIImage) | relay-router.go:115 | - | - |
| POST | /v1/images/generations | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIImage) | relay-router.go:112 | - | - |
| POST | /v1/images/variations | v1 | Token | controller.RelayNotImplemented | relay-router.go:154 | relay.go:440 | - |
| POST | /v1/messages | v1 | Token | inline: controller.Relay(c, types.RelayFormatClaude) | relay-router.go:88 | - | - |
| GET | /v1/models | v1 | Token | inline handler | relay-router.go:23 | - | - |
| POST | /v1/models/*path | v1 | Token | inline: controller.Relay(c, types.RelayFormatGemini) | relay-router.go:144 | - | - |
| DELETE | /v1/models/:model | v1 | Token | controller.RelayNotImplemented | relay-router.go:165 | relay.go:440 | - |
| GET | /v1/models/:model | v1 | Token | inline handler | relay-router.go:34 | - | - |
| POST | /v1/moderations | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAI) | relay-router.go:149 | - | - |
| GET | /v1/realtime | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIRealtime) | relay-router.go:78 | - | - |
| POST | /v1/rerank | v1 | Token | inline: controller.Relay(c, types.RelayFormatRerank) | relay-router.go:136 | - | - |
| POST | /v1/responses | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIResponses) | relay-router.go:101 | - | - |
| POST | /v1/responses/compact | v1 | Token | inline: controller.Relay(c, types.RelayFormatOpenAIResponsesCompaction) | relay-router.go:104 | - | - |
| POST | /v1/video/generations | v1 | Token | controller.RelayTask | video-router.go:23 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| GET | /v1/video/generations/:task_id | v1 | Token | controller.RelayTaskFetch | video-router.go:24 | relay.go:464 | relay.RelayTaskFetch |
| POST | /v1/videos | v1 | Token | controller.RelayTask | video-router.go:30 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| GET | /v1/videos/:task_id | v1 | Token | controller.RelayTaskFetch | video-router.go:31 | relay.go:464 | relay.RelayTaskFetch |
| GET | /v1/videos/:task_id/content | v1 | TokenOrUser | controller.VideoProxy | video-router.go:16 | video_proxy.go:33 | model.CacheGetChannel, model.GetByTaskId, service.GetHttpClientWithProxy |
| POST | /v1/videos/:video_id/remix | v1 | Token | controller.RelayTask | video-router.go:25 | relay.go:479 | model.InitTask, relay.RelayTaskSubmit, relay.ResolveOriginTask, service.LogTaskConsumption, service.SettleBilling, service.TaskErrorWrapperLocal |
| GET | /v1beta/models | v1beta | Token | inline: controller.ListModels(c, constant.ChannelTypeGemini) | relay-router.go:48 | - | - |
| POST | /v1beta/models/*path | v1beta | Token | inline: controller.Relay(c, types.RelayFormatGemini) | relay-router.go:197 | - | - |
| GET | /v1beta/openai/models | v1beta | Token | inline: controller.ListModels(c, constant.ChannelTypeOpenAI) | relay-router.go:57 | - | - |

## 7. 旧版 Dashboard 兼容接口（4）

| Method | Path | 子域 | Auth | Handler | Router定义 | Controller定义 | 关键依赖(service/model/relay) |
|---|---|---|---|---|---|---|---|
| GET | /dashboard/billing/subscription | dashboard | Token | controller.GetSubscription | dashboard.go:18 | billing.go:11 | model.GetTokenById, model.GetUserQuota, model.GetUserUsedQuota |
| GET | /dashboard/billing/usage | dashboard | Token | controller.GetUsage | dashboard.go:20 | billing.go:71 | model.GetTokenById, model.GetUserUsedQuota |
| GET | /v1/dashboard/billing/subscription | dashboard | Token | controller.GetSubscription | dashboard.go:19 | billing.go:11 | model.GetTokenById, model.GetUserQuota, model.GetUserUsedQuota |
| GET | /v1/dashboard/billing/usage | dashboard | Token | controller.GetUsage | dashboard.go:21 | billing.go:71 | model.GetTokenById, model.GetUserUsedQuota |

## 8. Web 回退路由（1）

| Method | Path | 子域 | Auth | Handler | Router定义 | Controller定义 | 关键依赖(service/model/relay) |
|---|---|---|---|---|---|---|---|
| NOROUTE |  | web | Public | inline: c.Set(middleware.RouteTagKey, "web") | web-router.go:21 | - | - |

## 9. 后续改造定位建议

1. 新增/改鉴权：优先改 `router/*.go` 的路由挂载中间件，再核对 `middleware/auth.go`。
2. 改计费或配额：先定位对应 `controller`，再查 `service/quota.go`、`service/billing*.go`、`model/log.go`。
3. 改模型路由选择：从 `middleware/distributor.go` 进入，联动 `service/channel_select.go`、`model/channel*.go`。
4. 改上游协议参数：从 `controller/relay.go` 与 `service/convert.go` 进入，避免破坏多供应商兼容。
5. 改数据库字段时：必须验证 SQLite/MySQL/PostgreSQL 三端兼容（参考 `model/main.go` 的兼容写法）。
6. 涉及 JSON 序列化：业务代码统一走 `common/json.go` 包装函数，不直接调用 `encoding/json`。
