# 飞书告警 Webhook 转换服务设计

## 1. 背景

当前 Prometheus 已将告警发送给 Alertmanager，Alertmanager 的 warning 和 critical receiver 都使用通用 Webhook。服务器实际配置的地址仍是无效占位域名，因此告警只能在 Prometheus 和 Alertmanager 页面查看，无法送达外部通知渠道。

飞书群自定义机器人不接受 Alertmanager 原始 Webhook JSON。两者之间需要一个轻量转换服务，将 Alertmanager 通知转换为飞书交互式卡片，再发送给飞书机器人。

## 2. 目标

- 将 Alertmanager 的 firing 和 resolved 通知发送到飞书群自定义机器人。
- 使用中文彩色卡片展示告警级别、状态、摘要、详细说明、对象标签和时间。
- critical firing 使用红色卡片并 `@所有人`。
- warning firing 使用橙色卡片，不 `@所有人`。
- resolved 使用绿色卡片，不 `@所有人`。
- 通过飞书机器人的 IP 白名单限制调用来源，服务器出口 IP 为 `198.44.181.187`。
- 飞书 Webhook 地址只通过只读 secret 文件提供，不进入 Git、镜像、日志或进程参数。
- 飞书返回失败时向 Alertmanager 返回非 2xx，让 Alertmanager 按现有策略重试。
- 为转换服务提供健康检查、结构化日志和可抓取的 Prometheus 指标。

## 3. 非目标

- 不接入飞书企业自建应用、OAuth 或消息回调。
- 不实现飞书签名校验；本次使用 IP 白名单方式。
- 不修改现有告警表达式、阈值、分组周期和抑制语义。
- 不在转换服务中实现第二套告警聚合、静默或升级策略，这些职责继续由 Alertmanager 承担。
- 不在飞书消息中暴露 API Key、Token、数据库密码、Webhook 地址或请求正文。

## 4. 方案选择

采用项目内置的 Go 转换服务，作为独立容器运行：

```text
Prometheus
    │ firing / resolved
    ▼
Alertmanager
    │ Alertmanager Webhook v4 JSON
    ▼
feishu-alert-webhook（Docker 内网）
    │ 飞书交互式卡片 JSON
    ▼
飞书群自定义机器人
```

没有采用第三方转换镜像，原因是告警内容、密钥处理、镜像版本和维护周期不可控。没有采用 Alertmanager 直连飞书，因为飞书机器人与 Alertmanager 的请求协议不兼容。

## 5. 组件边界

### 5.1 Alertmanager

Alertmanager 继续负责：

- 告警分组、等待、重复通知和恢复通知。
- warning/critical 路由。
- critical 对 warning 的抑制。
- silence 静默。

两个 receiver 都改为调用 Docker 内网地址：

```text
http://feishu-alert-webhook:8080/api/v1/alerts
```

warning 和 critical 仍保留独立 receiver 名称，便于在 Alertmanager 页面和日志中区分路由结果。消息颜色和 `@所有人` 行为以 Alertmanager 传入的 `severity` 和 `status` 为准，不依赖 receiver 名称。

### 5.2 飞书转换服务

转换服务只负责：

- 校验 Alertmanager Webhook 请求。
- 将一组告警转换为一张飞书卡片。
- 调用飞书机器人 Webhook。
- 校验 HTTP 状态码和飞书业务返回码。
- 输出不包含秘密信息的结构化日志与 Prometheus 指标。

转换服务不暴露宿主机端口，只加入 `monitoring` Docker 网络。外部客户端不能直接访问其告警入口。

### 5.3 飞书机器人

飞书群自定义机器人负责把卡片投递到目标群。机器人安全设置使用 IP 白名单，加入服务器出口 IP `198.44.181.187`，不启用签名密钥。

## 6. 输入契约

转换服务接受 Alertmanager Webhook v4 请求，使用以下字段：

- 顶层：`version`、`status`、`receiver`、`groupLabels`、`commonLabels`、`commonAnnotations`、`externalURL`、`alerts`。
- 单条告警：`status`、`labels`、`annotations`、`startsAt`、`endsAt`、`generatorURL`、`fingerprint`。

请求约束：

- 仅接受 `POST /api/v1/alerts`。
- `Content-Type` 必须为 JSON。
- 请求体最大 1 MiB。
- `version` 必须为 `4`。
- `alerts` 必须至少包含一条告警。
- 缺少 `severity` 时按 warning 处理，并在日志中记录输入异常。
- 无法解析的请求返回 400，不向飞书发送消息。

## 7. 飞书卡片设计

### 7.1 卡片头部

| 告警状态 | 卡片颜色 | 标题 | 提醒策略 |
| --- | --- | --- | --- |
| critical firing | red | `严重告警 · <告警名称>` | `@所有人` |
| warning firing | orange | `警告 · <告警名称>` | 不提醒所有人 |
| resolved | green | `告警恢复 · <告警名称>` | 不提醒所有人 |

同一通知组包含多个告警名称时，标题使用 `严重告警 · N 条告警`、`警告 · N 条告警` 或 `告警恢复 · N 条告警`。

### 7.2 卡片内容

卡片首先展示公共信息：

- 状态：触发中或已恢复。
- 级别：严重或警告。
- 集群、任务和接收器。
- 告警数量。
- 通知时间，按 `Asia/Shanghai` 显示。

随后逐条展示告警：

- 告警名称 `alertname`。
- 中文摘要 `summary`。
- 中文详细说明 `description`。
- 当前值 `value`，仅在 annotation 存在时展示。
- 与对象定位有关的固定标签：`channel_id`、`channel_type`、`relay_format`、`instance`、`database`、`platform`、`device`、`mountpoint`、`collector`、`error_type`。
- 开始时间；resolved 告警同时展示恢复时间和持续时长。
- `generatorURL` 和顶层 `externalURL` 仅在后续配置了浏览器可访问的 HTTPS 外部地址时才提供跳转按钮。本次部署的 Prometheus 和 Alertmanager 只监听服务器回环地址，因此首版卡片不展示不可访问的内部链接。

固定标签按上述白名单输出，避免把未来新增的高敏感或高基数字段直接带入飞书。空字段不展示。

### 7.3 多告警限制

- 一张卡片最多展示 10 条告警。
- 超过 10 条时，在卡片底部显示“另有 N 条告警未展开，请前往 Alertmanager 查看”。
- 单个摘要和详细说明按 Unicode 字符安全截断，卡片总内容控制在飞书接口限制以内。
- 不拆分成多张卡片，避免同一告警组产生通知风暴。

### 7.4 中文内容

卡片框架、状态、级别、标签名和错误信息全部使用中文。实施时同步把现有告警规则中仍为英文的 `summary` 和 `description` 翻译为中文，但不改变规则表达式、阈值、`for` 时间、alertname 或标签。

## 8. 飞书请求与响应

转换服务向 secret 文件中的完整飞书机器人 Webhook URL 发送交互式卡片请求：

- HTTP 方法：POST。
- Content-Type：`application/json; charset=utf-8`。
- 连接与完整请求超时：10 秒。
- 不自动跟随跨域重定向，避免把请求发送到非飞书域名。
- 启动时校验 URL 使用 HTTPS，且主机名属于飞书官方开放平台域名。

只有同时满足以下条件才视为投递成功：

- HTTP 状态码为 2xx。
- 飞书响应 JSON 中的业务返回码表示成功。

非 2xx、业务失败码、超时、DNS 错误或响应解析失败，都返回 502 给 Alertmanager。转换服务日志记录失败类型、HTTP 状态、飞书业务码和告警组信息，但不记录完整 Webhook URL 或响应中的潜在敏感字段。

## 9. 重试与幂等语义

转换服务本身不做后台重试，失败直接返回非 2xx，由 Alertmanager 负责重试。这样只有一套重试状态，不会在两个组件中形成叠加重试。

通知语义为至少一次。极少数情况下，飞书已经收取消息但响应在网络中丢失，Alertmanager 重试后可能产生重复卡片。本次不增加持久化去重数据库，避免为了低概率重复引入状态服务。卡片中保留告警名称、时间和对象标签，便于识别重复通知。

## 10. 安全设计

- 飞书 Webhook URL 保存在服务器文件 `/data/new-api-ver/runtime/secrets/feishu-webhook-url`。
- Compose 通过 `FEISHU_WEBHOOK_URL_FILE` 将该文件挂载到转换服务的 `/run/secrets/feishu_webhook_url`。
- secret 文件不加入 Git，部署文档要求限制为 root 和容器运行组可读。
- 转换服务以非 root 用户运行，文件系统设为只读，并启用 `no-new-privileges`。
- 容器只加入 `monitoring` 网络，不映射端口。
- Alertmanager 不再直接读取外部 Webhook secret，只请求内部转换服务。
- 日志、指标标签和错误响应都不得包含完整 Webhook URL。
- 飞书机器人 IP 白名单只允许服务器出口 IP `198.44.181.187`。

## 11. 可观测性

转换服务提供：

- `GET /healthz`：进程存活时返回 200，不依赖外部网络或飞书状态。
- `GET /readyz`：Webhook secret 已加载、配置合法且 HTTP 客户端可用时返回 200；不主动向飞书发送探测消息。
- `GET /metrics`：供 Prometheus 抓取。

指标使用固定低基数标签：

- `newapi_feishu_alert_requests_total{result}`：接收请求数量，`result` 为 `accepted` 或 `rejected`。
- `newapi_feishu_alert_deliveries_total{result,severity,status}`：投递结果，`result` 为 `success`、`http_error`、`feishu_error`、`timeout` 或 `network_error`。
- `newapi_feishu_alert_delivery_duration_seconds`：飞书请求耗时直方图。
- `newapi_feishu_alerts_total{severity,status}`：转换的实际告警条数。
- `newapi_feishu_alert_webhook_configured`：Webhook secret 是否已成功加载，值为 0 或 1。

不使用 alertname、channel_id、instance 或错误文本作为转换服务指标标签，避免基数增长。

Prometheus 增加内部 scrape job。监控页面可以展示转换服务存活和投递失败数。转换服务故障时，Alertmanager 自身无法通过该服务向飞书报告故障，因此服务器日志和 Prometheus 页面仍是必要的兜底观察入口。

## 12. 容器与部署

新增独立 Dockerfile，以当前项目 Go 工具链构建静态二进制，运行镜像不包含编译器和包管理器。二进制同时支持服务启动和健康检查子命令，使只读最小镜像也能执行 Compose healthcheck。

Compose 新增 `feishu-alert-webhook` 服务：

- `restart: unless-stopped`。
- 只连接 `monitoring` 网络。
- 不配置 `ports`。
- 挂载 `feishu_webhook_url` secret。
- `read_only: true`。
- `security_opt: no-new-privileges:true`。
- 健康检查调用本地 `/readyz`，确保 Alertmanager 开始投递前转换服务已经加载有效配置。

Alertmanager 增加对转换服务 healthy 的依赖。Prometheus 增加对转换服务 `/metrics` 的抓取目标。

部署顺序：

1. 在飞书群创建自定义机器人并启用 IP 白名单。
2. 将 `198.44.181.187` 加入机器人白名单。
3. 把完整机器人 Webhook URL 写入服务器 secret 文件并设置权限。
4. 拉取包含转换服务的代码。
5. 构建并启动转换服务、Alertmanager 和 Prometheus。
6. 检查三个容器健康状态和 Prometheus targets。
7. 发送受控测试 firing 告警，确认红色 critical 卡片和 `@所有人`。
8. 恢复测试告警，确认绿色恢复卡片且不 `@所有人`。

## 13. 测试策略

### 13.1 Go 单元测试

- Alertmanager v4 JSON 正确解析。
- critical firing 生成红色卡片并包含 `@所有人`。
- warning firing 生成橙色卡片且不包含 `@所有人`。
- resolved 生成绿色卡片且不包含 `@所有人`。
- 多告警标题、10 条截断和剩余数量正确。
- 标签白名单过滤生效，未知标签和敏感字段不进入卡片。
- 中文时间、持续时长和空字段处理正确。
- 非法 JSON、错误版本、空 alerts 和超大请求返回 400。
- 飞书 2xx 且业务成功时返回 2xx。
- 飞书非 2xx、业务失败、超时和非法响应时返回 502。
- 日志与错误消息不包含 Webhook URL。

### 13.2 配置静态校验

- `go test ./... -count=1`。
- Docker Compose 两个数据库 profile 均能通过 config 校验。
- Alertmanager 配置通过 `amtool check-config`。
- Prometheus 配置和规则通过 `promtool`。
- 校验脚本确认转换服务没有宿主机端口、挂载了 secret、启用了健康检查和只读文件系统。

### 13.3 服务器验收

- 转换服务、Alertmanager、Prometheus 均为 healthy。
- Prometheus 能抓取转换服务指标。
- 受控 critical firing 告警在飞书显示红色卡片并 `@所有人`。
- 受控 warning firing 告警显示橙色卡片且不 `@所有人`。
- resolved 告警显示绿色卡片且不 `@所有人`。
- 飞书投递成功指标增加，Alertmanager 通知失败日志为 0。
- 暂时替换为不可达测试地址时，转换服务返回失败且 Alertmanager 进入重试；恢复配置后通知可正常送达。

## 14. 回滚方案

- 保留实施前的 Alertmanager 配置和 Compose 版本。
- 如果转换服务异常，回滚代码和 Compose 后重新启动 Alertmanager、Prometheus。
- Prometheus 指标和告警规则不依赖飞书转换服务才能计算，回滚通知链路不会影响监控数据采集。
- 飞书 Webhook secret 独立存放，回滚时不删除，便于修复后重新启用。

## 15. 完成标准

以下条件全部满足才视为功能完成：

- 代码、配置、文档和测试进入同一版本。
- 全量 Go 测试和所有监控静态校验通过。
- 服务器部署的是包含最新 18 条 Alertmanager 抑制规则的配置。
- 飞书 firing、resolved 实际消息均完成验收。
- critical firing 会 `@所有人`，warning 和 resolved 不会。
- Alertmanager 和转换服务日志中没有持续投递失败。
- Git 和容器检查确认未泄露飞书 Webhook URL。
