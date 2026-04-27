# new-api 项目架构说明

> 适用代码基线：`new-api-ver` 当前工作区源码
> 更新时间：2026-04-13

## 1. 架构总览

这是一个 **AI API 聚合网关**，将 40+ 上游模型服务统一成兼容 OpenAI/Claude/Gemini 等协议的入口。

核心目标：
- 统一 API 协议与鉴权方式
- 多渠道路由与可用性兜底
- 配额/账单/日志/订阅等平台能力
- 管理后台与用户控制台

系统采用分层架构：

`Router -> Middleware -> Controller -> Service -> Model -> DB/Cache/Upstream`

## 2. 目录分层职责

- `main.go`
  - 进程启动入口，初始化配置、数据库、Redis、i18n、OAuth、后台任务。
- `router/`
  - 路由注册与分组，定义接口边界与中间件链。
- `middleware/`
  - 鉴权、限流、渠道分发、性能保护、统计、日志等横切能力。
- `controller/`
  - HTTP 层参数解析、上下文读取、响应包装。
- `service/`
  - 核心业务逻辑：渠道选择、计费结算、任务调度、格式转换。
- `model/`
  - GORM 数据模型与 DB 访问、缓存同步。
- `relay/`
  - 上游协议适配与转发（OpenAI/Claude/Gemini/MJ/Suno/视频等）。
- `web/`
  - React + Vite 前端管理界面。

## 3. 路由域与入口

由 `router/main.go` 统一挂载五大域：

1. `SetApiRouter`：`/api/*`
   - 管理、用户、订阅、渠道、令牌、日志、模型、部署等控制面接口。
2. `SetDashboardRouter`：`/dashboard/*` 与 `/v1/dashboard/*`
   - 老版兼容账单接口。
3. `SetRelayRouter`：`/v1/*`、`/v1beta/*`、`/mj/*`、`/suno/*`、`/pg/*` 等
   - 数据面中继接口，负责请求转发与多渠道选择。
4. `SetVideoRouter`：`/v1/video*`、`/v1/videos*`、`/kling/v1/*`、`/jimeng`
   - 视频任务提交、查询、内容代理。
5. `SetWebRouter`：Web 静态资源与 SPA 回退。

## 4. 请求处理主链路

### 4.1 控制面（`/api`）

典型链路：
1. 全局中间件（请求 ID、i18n、日志、session）
2. API 组中间件（gzip、全局限流、body 清理）
3. 细分鉴权（`UserAuth/AdminAuth/RootAuth`）
4. Controller 入参校验
5. Service 执行业务（账单、订阅、渠道配置等）
6. Model 执行 DB 操作（GORM）
7. 返回统一 JSON 响应

### 4.2 数据面中继（`/v1`、`/v1beta`、`/mj` 等）

典型链路：
1. `TokenAuth`（支持 Bearer、多供应商 key 兼容）
2. `ModelRequestRateLimit` / 系统性能检查
3. `Distribute` 根据模型/分组/可用性选渠道
4. `controller.Relay` 识别请求格式（OpenAI/Claude/Gemini/...）
5. `relay/channel/*` 发起上游请求并回传流式或非流式结果
6. 记录 usage/log，进行计费与配额扣减

### 4.3 视频任务链路

1. 鉴权（`TokenAuth` 或 `TokenOrUserAuth`）
2. `Distribute` 选视频渠道
3. `RelayTask` 提交异步任务并落库
4. `RelayTaskFetch` 查询任务状态
5. `VideoProxy` 代理视频内容下载（按任务回查渠道）

### 4.4 Web 前端链路

1. 静态资源中间件
2. Web 全局限流 + 缓存
3. 非 API 路径走 `index.html`（SPA 回退）
4. API/Relay 路径不命中时返回中继风格错误

## 5. 中间件架构（核心）

### 5.1 鉴权层

- `UserAuth`：用户会话/访问令牌鉴权
- `AdminAuth`：管理员权限
- `RootAuth`：最高权限配置入口
- `TokenAuth`：数据面 token 鉴权（含多协议 key 适配）
- `TokenAuthReadOnly`：只读 token 鉴权（宽松）
- `TokenOrUserAuth`：视频内容代理兼容控制台与 API 客户端

### 5.2 流量治理

- `GlobalAPIRateLimit`、`GlobalWebRateLimit`
- `CriticalRateLimit`（登录、支付等关键操作）
- `SearchRateLimit`（按用户 ID）
- `ModelRequestRateLimit`（模型级请求节流）

### 5.3 路由与健康保护

- `Distribute`：核心路由决策，按模型/分组/token 约束选渠道
- `SystemPerformanceCheck`：系统资源超阈值时拒绝服务
- `StatsMiddleware`：活跃连接统计

## 6. 存储与状态架构

### 6.1 数据层

- 主 DB：SQLite / MySQL / PostgreSQL（三端兼容）
- ORM：GORM v2
- 主要实体：`User`、`Token`、`Channel`、`Log`、`TopUp`、`Subscription`、`Task` 等

### 6.2 缓存层

- Redis：分布式缓存与限流状态
- 内存缓存：渠道缓存、选路缓存、速率控制等
- 启动后会执行周期同步（如渠道缓存、配置选项）

### 6.3 后台任务

- 自动渠道测试与余额更新
- Codex credential 自动刷新
- 订阅额度重置任务
- 模型上游差异检测与更新任务
- Midjourney/Task 批量更新任务（条件启用）

## 7. 控制面与数据面分离

### 控制面（Control Plane）

负责“配置与管理”：
- 用户/角色/令牌
- 渠道配置与模型能力
- 订阅计划/支付回调
- 系统选项与性能管理

### 数据面（Data Plane）

负责“高频推理流量”：
- 请求鉴权与模型解析
- 动态选路与转发
- 流式响应透传
- 使用量计费与日志沉淀

这种分离使得后台管理改动不直接影响核心推理链路吞吐。

## 8. 扩展点设计

### 新增上游渠道

入口通常在：
- `relay/channel/<provider>/`
- `constant/` 渠道类型定义
- `router/relay-router.go`（如需新增协议路由）
- `service/convert.go` / 对应 DTO 映射

### 新增管理接口

入口通常在：
- `router/api-router.go` 挂路由
- `controller/*.go` 入参 + 响应
- `service/*.go` 业务实现
- `model/*.go` 数据落库

## 9. 建议的改造切入顺序

1. 先改路由与中间件（边界）
2. 再改 controller/service（行为）
3. 最后改 model/schema（持久化）
4. 涉及 relay 的改动需验证流式与非流式两条链
5. 涉及 DB 的改动要验证 SQLite/MySQL/PostgreSQL 三端

## 10. AI 调用接口分析（客户端调用入口）

本节聚焦“实际用于调用 AI 能力”的数据面接口（`relay/video/task`），不含后台管理接口。

### 10.1 统计结论（源码扫描）

- AI 调用相关路由：`81` 条
- 已实现：`69` 条
- 显式未实现（`501`）：`12` 条
- 鉴权分布：`Token 77`、`Public 2`、`User 1`、`TokenOrUser 1`

高频处理入口：
- `controller.Relay`：文本、音频、图像、向量、重排、实时
- `controller.RelayTask / RelayTaskFetch`：视频、Suno、Kling、Jimeng 异步任务
- `controller.RelayMidjourney`：Midjourney 动作与任务查询

### 10.2 同步推理接口（OpenAI/Claude/Gemini 兼容）

| 能力 | 方法 | 路径 | RelayFormat/模式 | 处理函数 | 鉴权 |
|---|---|---|---|---|---|
| Chat | POST | `/v1/chat/completions` | `RelayFormatOpenAI` | `controller.Relay` | Token |
| Legacy Completion | POST | `/v1/completions` | `RelayFormatOpenAI` | `controller.Relay` | Token |
| Claude Messages | POST | `/v1/messages` | `RelayFormatClaude` | `controller.Relay` | Token |
| Responses API | POST | `/v1/responses` | `RelayFormatOpenAIResponses` | `controller.Relay` | Token |
| Responses Compact | POST | `/v1/responses/compact` | `RelayFormatOpenAIResponsesCompaction` | `controller.Relay` | Token |
| Moderation | POST | `/v1/moderations` | `RelayFormatOpenAI` | `controller.Relay` | Token |
| Playground Chat | POST | `/pg/chat/completions` | `RelayFormatOpenAI` | `controller.Playground -> Relay` | User |

### 10.3 模型列表与模型查询接口

| 方法 | 路径 | 说明 | 鉴权 |
|---|---|---|---|
| GET | `/v1/models` | 模型列表（按请求头自动兼容 OpenAI/Anthropic/Gemini 风格） | Token |
| GET | `/v1/models/:model` | 单模型详情 | Token |
| GET | `/v1beta/models` | Gemini 风格模型列表 | Token |
| GET | `/v1beta/openai/models` | OpenAI 兼容模型列表 | Token |

### 10.4 多模态与向量能力接口

| 能力 | 方法 | 路径 | RelayFormat/模式 | 鉴权 |
|---|---|---|---|---|
| 图像生成 | POST | `/v1/images/generations` | `RelayFormatOpenAIImage` | Token |
| 图像编辑 | POST | `/v1/images/edits` | `RelayFormatOpenAIImage` | Token |
| 兼容编辑接口 | POST | `/v1/edits` | `RelayFormatOpenAIImage` | Token |
| 音频转写 | POST | `/v1/audio/transcriptions` | `RelayFormatOpenAIAudio` | Token |
| 音频翻译 | POST | `/v1/audio/translations` | `RelayFormatOpenAIAudio` | Token |
| 文本转语音 | POST | `/v1/audio/speech` | `RelayFormatOpenAIAudio` | Token |
| Embedding | POST | `/v1/embeddings` | `RelayFormatEmbedding` | Token |
| Gemini Embedding 兼容 | POST | `/v1/engines/:model/embeddings` | `RelayFormatGemini` | Token |
| Gemini 通用调用 | POST | `/v1/models/*path` | `RelayFormatGemini` | Token |
| Gemini v1beta 调用 | POST | `/v1beta/models/*path` | `RelayFormatGemini` | Token |
| Rerank | POST | `/v1/rerank` | `RelayFormatRerank` | Token |

### 10.5 实时与异步任务接口

| 类型 | 方法 | 路径 | 处理函数 | 鉴权 |
|---|---|---|---|---|
| Realtime WebSocket | GET | `/v1/realtime` | `controller.Relay (OpenAIRealtime)` | Token |
| 视频生成任务提交 | POST | `/v1/video/generations` | `controller.RelayTask` | Token |
| 视频生成任务查询 | GET | `/v1/video/generations/:task_id` | `controller.RelayTaskFetch` | Token |
| OpenAI 兼容视频提交 | POST | `/v1/videos` | `controller.RelayTask` | Token |
| OpenAI 兼容视频查询 | GET | `/v1/videos/:task_id` | `controller.RelayTaskFetch` | Token |
| 视频 Remix | POST | `/v1/videos/:video_id/remix` | `controller.RelayTask` | Token |
| 视频内容代理 | GET | `/v1/videos/:task_id/content` | `controller.VideoProxy` | TokenOrUser |
| Suno 提交 | POST | `/suno/submit/:action` | `controller.RelayTask` | Token |
| Suno 查询 | POST/GET | `/suno/fetch`, `/suno/fetch/:id` | `controller.RelayTaskFetch` | Token |
| Kling 提交 | POST | `/kling/v1/videos/text2video`, `/kling/v1/videos/image2video` | `controller.RelayTask` | Token |
| Kling 查询 | GET | `/kling/v1/videos/text2video/:task_id`, `/kling/v1/videos/image2video/:task_id` | `controller.RelayTaskFetch` | Token |
| Jimeng 官方格式 | POST | `/jimeng/` | `controller.RelayTask` | Token |

### 10.6 Midjourney 接口族

提供两套等价前缀：`/mj/*` 与 `/:mode/mj/*`（后者用于多模式路由）。

- 公开接口（无需 token）：
  - `GET /mj/image/:id`
  - `GET /:mode/mj/image/:id`
- 需 token 的主要动作：
  - `POST /mj/submit/imagine`
  - `POST /mj/submit/change`
  - `POST /mj/submit/simple-change`
  - `POST /mj/submit/blend`
  - `POST /mj/submit/describe`
  - `POST /mj/submit/modal`
  - `POST /mj/submit/shorten`
  - `POST /mj/submit/action`
  - `POST /mj/submit/video`
  - `POST /mj/submit/edits`
  - `POST /mj/submit/upload-discord-images`
  - `POST /mj/insight-face/swap`
  - `GET /mj/task/:id/fetch`
  - `GET /mj/task/:id/image-seed`
  - `POST /mj/task/list-by-condition`
  - 同名接口在 `/:mode/mj/*` 下各有一套对应路由

### 10.7 当前未实现接口（固定返回 501）

- `POST /v1/images/variations`
- `GET /v1/files`
- `POST /v1/files`
- `DELETE /v1/files/:id`
- `GET /v1/files/:id`
- `GET /v1/files/:id/content`
- `POST /v1/fine-tunes`
- `GET /v1/fine-tunes`
- `GET /v1/fine-tunes/:id`
- `POST /v1/fine-tunes/:id/cancel`
- `GET /v1/fine-tunes/:id/events`
- `DELETE /v1/models/:model`

### 10.8 鉴权与请求头兼容要点

`TokenAuth` 对多协议 Key 有兼容处理：

- 默认：`Authorization: Bearer sk-...`
- Claude 兼容：`x-api-key`（`/v1/messages`、`/v1/models*`）
- Gemini 兼容：`x-goog-api-key` 或 query `key`（`/v1beta/models*`、`/v1/models/:model`）
- Realtime：`Sec-WebSocket-Protocol` 中支持 `openai-insecure-api-key.sk-...`
- Midjourney 兼容：支持 `mj-api-secret`

补充说明：
- 数据面主链路默认挂载 `SystemPerformanceCheck + TokenAuth + ModelRequestRateLimit + Distribute`（部分任务路由按场景精简）。
- 实际完整路由表见 `docs/codebase-interface-analysis.md` 的“Relay/Provider 兼容接口清单”。

---

如果你后面要做具体功能改造，我建议按这个顺序定位：

- 找接口：先查 `docs/codebase-interface-analysis.md`
- 找架构关系：再看本文件
- 开始改代码：从对应 `router -> controller -> service -> model` 路径逐层下钻
