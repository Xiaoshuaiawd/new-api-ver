# 飞书告警 Webhook 转换服务实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为现有 Prometheus/Alertmanager 监控栈增加一个项目自带的 Go 转换服务，把 firing/resolved 告警转换为中文飞书彩色卡片并通过 IP 白名单机器人发送。

**Architecture:** Alertmanager v0.28.1 继续负责分组、抑制、静默、重复和恢复通知，并把 Webhook v4 JSON 发送到 Docker 内网的 `feishu-alert-webhook`。转换服务校验输入、构造不超过飞书 20 KB 限制的交互式卡片、调用飞书机器人、校验 `code == 0`，失败时返回非 2xx 让 Alertmanager 重试。飞书 URL 只从只读 secret 文件加载。

**Tech Stack:** Go 1.25.1 module、Go 1.26.1 build image、标准库 `net/http`/`log/slog`、项目 `common` JSON 包装、`prometheus/client_golang` v1.22.0、Docker Compose、Alertmanager v0.28.1、Prometheus v3.5.0。

## Global Constraints

- 飞书机器人使用 IP 白名单，服务器出口 IP 固定为 `198.44.181.187`，不实现签名模式。
- critical firing 使用红色卡片并 `@所有人`；warning firing 使用橙色；resolved 使用绿色且不提醒所有人。
- 一张卡片最多展示 10 条告警，请求 JSON 必须小于 20 KB。
- 仅输出固定标签白名单，不把任意 Prometheus 标签复制到飞书。
- Webhook URL 不进入 Git、镜像、环境变量值、日志、指标标签或错误响应。
- 所有 JSON 编解码通过 `common.Marshal`、`common.Unmarshal` 或 `common.DecodeJson`。
- 新 Go 测试使用 `testify/require` 和 `testify/assert`。
- 不修改告警表达式、阈值、`for` 时间、alertname、分组和抑制语义。
- 保留现有未提交文件 `pkg/prometheus_metrics/channel_state_collector.go`、`pkg/prometheus_metrics/registry_test.go`、`pkg/prometheus_metrics/task_queue_collector.go` 和 `.superpowers/`，不得暂存或覆盖。

---

## 文件结构

- `pkg/feishualert/message.go`：Alertmanager 输入模型、飞书卡片模型、中文卡片构造与安全截断。
- `pkg/feishualert/message_test.go`：卡片颜色、`@所有人`、恢复、多告警、标签白名单和 20 KB 边界测试。
- `pkg/feishualert/service.go`：URL 校验、HTTP 路由、飞书投递、返回码校验、健康端点和 Prometheus 指标。
- `pkg/feishualert/service_test.go`：请求校验、飞书成功/失败/超时、指标和秘密不泄露测试。
- `cmd/feishu-alert-webhook/main.go`：secret 加载、时区配置、HTTP server、优雅退出和健康检查子命令。
- `cmd/feishu-alert-webhook/main_test.go`：配置加载、空 secret、非法时区和健康检查测试。
- `deploy/monitoring/feishu-webhook/Dockerfile`：独立静态二进制的多阶段镜像。
- `docker-compose.monitoring.yml`：新增内部转换服务和飞书 secret，Alertmanager 改用内部 receiver。
- `deploy/monitoring/alertmanager.yml.example`：warning/critical receiver 指向转换服务。
- `deploy/monitoring/prometheus.yml`：抓取转换服务 `/metrics`。
- `deploy/monitoring/secrets/feishu-webhook-url.example`：合法格式示例。
- `deploy/monitoring/validate.sh`：Compose、secret、Alertmanager、Prometheus 和容器安全约束校验。
- `deploy/monitoring/alert-rules.yml`：仅把仍为英文的告警注释翻译成中文。
- `docs/prometheus-monitoring.md`：飞书机器人、IP 白名单、secret、部署和验收说明。
- `docs/prometheus-monitoring-todolist.md`：记录飞书通知链路实施与生产验收状态。

---

### Task 1: 构造中文飞书告警卡片

**Files:**
- Create: `pkg/feishualert/message.go`
- Create: `pkg/feishualert/message_test.go`

**Interfaces:**
- Consumes: Alertmanager Webhook v4 JSON 中的 `version`、`status`、`receiver`、`groupLabels`、`commonLabels`、`commonAnnotations`、`externalURL`、`alerts`、`truncatedAlerts`。
- Produces: `func BuildCard(message WebhookMessage, now time.Time, location *time.Location) (CardMessage, CardMeta, error)`。
- Produces: `CardMeta{Severity string, Status string, AlertCount int}`，供投递指标使用。

- [ ] **Step 1: 写 critical firing 的失败测试**

测试固定输入包含 `severity=critical`、`channel_id=12`、一个未知标签 `token=secret-value`，断言：

```go
card, meta, err := BuildCard(message, now, shanghai)
require.NoError(t, err)
assert.Equal(t, "critical", meta.Severity)
assert.Equal(t, "firing", meta.Status)
assert.Equal(t, "red", card.Card.Header.Template)
assert.Contains(t, card.Card.Body.Elements[0].Content, "<at id=all></at>")
assert.Contains(t, card.Card.Body.Elements[0].Content, "渠道 ID：12")
assert.NotContains(t, card.Card.Body.Elements[0].Content, "secret-value")
```

- [ ] **Step 2: 运行测试并确认 RED**

Run: `go test ./pkg/feishualert -run TestBuildCardCriticalFiring -count=1 -v`

Expected: FAIL，因为 `BuildCard` 和消息类型尚未定义。

- [ ] **Step 3: 定义输入输出类型并实现最小 critical 卡片**

在 `message.go` 定义：

```go
type WebhookMessage struct {
    Version           string            `json:"version"`
    Status            string            `json:"status"`
    Receiver          string            `json:"receiver"`
    GroupLabels       map[string]string `json:"groupLabels"`
    CommonLabels      map[string]string `json:"commonLabels"`
    CommonAnnotations map[string]string `json:"commonAnnotations"`
    ExternalURL       string            `json:"externalURL"`
    Alerts            []Alert           `json:"alerts"`
    TruncatedAlerts   int               `json:"truncatedAlerts"`
}

type Alert struct {
    Status       string            `json:"status"`
    Labels       map[string]string `json:"labels"`
    Annotations  map[string]string `json:"annotations"`
    StartsAt     time.Time         `json:"startsAt"`
    EndsAt       time.Time         `json:"endsAt"`
    GeneratorURL string            `json:"generatorURL"`
    Fingerprint  string            `json:"fingerprint"`
}

type CardMessage struct {
    MsgType string `json:"msg_type"`
    Card    Card   `json:"card"`
}

type Card struct {
    Schema string     `json:"schema"`
    Config CardConfig `json:"config"`
    Body   CardBody   `json:"body"`
    Header CardHeader `json:"header"`
}

type CardConfig struct {
    UpdateMulti bool `json:"update_multi"`
}

type CardBody struct {
    Direction string        `json:"direction"`
    Padding   string        `json:"padding"`
    Elements  []CardElement `json:"elements"`
}

type CardElement struct {
    Tag       string `json:"tag"`
    Content   string `json:"content"`
    TextAlign string `json:"text_align"`
    TextSize  string `json:"text_size"`
}

type CardHeader struct {
    Title    CardText `json:"title"`
    Template string   `json:"template"`
    Padding  string   `json:"padding"`
}

type CardText struct {
    Tag     string `json:"tag"`
    Content string `json:"content"`
}

type CardMeta struct {
    Severity   string
    Status     string
    AlertCount int
}
```

`BuildCard` 对 critical firing 生成 `schema: "2.0"`、`msg_type: "interactive"`、red header 和包含 `<at id=all></at>` 的 markdown element。只读取标签白名单：`channel_id`、`channel_type`、`relay_format`、`instance`、`database`、`platform`、`device`、`mountpoint`、`collector`、`error_type`。

- [ ] **Step 4: 运行 critical 测试并确认 GREEN**

Run: `go test ./pkg/feishualert -run TestBuildCardCriticalFiring -count=1 -v`

Expected: PASS。

- [ ] **Step 5: 写 warning、resolved、多告警和边界失败测试**

增加表驱动测试，精确断言：

```go
tests := []struct {
    name          string
    status        string
    severity      string
    template      string
    mentionAll    bool
}{
    {name: "warning firing", status: "firing", severity: "warning", template: "orange"},
    {name: "critical firing", status: "firing", severity: "critical", template: "red", mentionAll: true},
    {name: "critical resolved", status: "resolved", severity: "critical", template: "green"},
}
```

另加测试确认：空 alerts 返回错误；缺少 severity 回退为 warning；超过 10 条只展示 10 条并显示剩余数量；summary 超过 200 rune、description 超过 500 rune 时安全截断；注释中的 `<at id=all></at>` 被转义，只有程序生成的 critical mention 能生效；`common.Marshal(card)` 后长度小于 20 KiB。

- [ ] **Step 6: 运行新增测试并确认 RED**

Run: `go test ./pkg/feishualert -count=1 -v`

Expected: FAIL，缺少状态映射、截断和内容净化。

- [ ] **Step 7: 实现状态映射、中文字段和安全截断**

实现常量：

```go
const (
    maxVisibleAlerts    = 10
    maxSummaryRunes     = 200
    maxDescriptionRunes = 500
    maxFeishuBodyBytes  = 20 * 1024
)
```

实现 `truncateRunes`、`sanitizeCardText`、`severityOf`、`statusOf`。卡片时间使用传入的 `location`，格式固定为 `2006-01-02 15:04:05 MST`。resolved 使用 `EndsAt`，有效时显示持续时长。最终使用 `common.Marshal` 检查消息体长度，超过限制返回明确错误，不发送不完整卡片。

- [ ] **Step 8: 运行包测试并提交 Task 1**

Run: `go test ./pkg/feishualert -count=1`

Expected: PASS。

Commit:

```bash
git add pkg/feishualert/message.go pkg/feishualert/message_test.go
git commit -m "监控：生成飞书告警卡片"
```

---

### Task 2: 实现投递服务、健康端点和指标

**Files:**
- Create: `pkg/feishualert/service.go`
- Create: `pkg/feishualert/service_test.go`

**Interfaces:**
- Consumes: Task 1 的 `WebhookMessage`、`CardMessage`、`CardMeta` 和 `BuildCard`。
- Produces: `func NewService(config ServiceConfig) (*Service, error)`。
- Produces: `func (service *Service) Handler() http.Handler`，包含 `/api/v1/alerts`、`/healthz`、`/readyz`、`/metrics`。

- [ ] **Step 1: 写 URL 校验和成功投递失败测试**

定义测试期望：

```go
service, err := NewService(ServiceConfig{
    WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/example",
    HTTPClient: &http.Client{Transport: transport},
    Registry:   prometheus.NewRegistry(),
    Logger:     slog.New(slog.NewJSONHandler(&logs, nil)),
    Now:        func() time.Time { return now },
    Location:   shanghai,
})
require.NoError(t, err)
```

生产 URL 只允许 `https://open.feishu.cn/open-apis/bot/v2/hook/` 路径。测试使用自定义 `roundTripFunc` 返回受控响应，不给生产配置增加 HTTP 或任意域名绕过开关。成功上游返回 `{"code":0,"msg":"success"}`，handler 必须返回 204。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `go test ./pkg/feishualert -run 'TestNewServiceValidatesWebhookURL|TestServiceDeliversAlert' -count=1 -v`

Expected: FAIL，因为 Service 尚未实现。

- [ ] **Step 3: 实现 ServiceConfig、路由和成功投递**

定义：

```go
type ServiceConfig struct {
    WebhookURL string
    HTTPClient *http.Client
    Registry   *prometheus.Registry
    Logger     *slog.Logger
    Now        func() time.Time
    Location   *time.Location
}

type Service struct {
    webhookURL string
    client     *http.Client
    logger     *slog.Logger
    now        func() time.Time
    location   *time.Location
    handler    http.Handler
    ready      atomic.Bool
    metrics    serviceMetrics
}
```

`/api/v1/alerts` 仅接受 POST JSON；使用 `http.MaxBytesReader` 限制 1 MiB；使用 `common.DecodeJson` 解码，拒绝 version 非 `4` 和空 alerts；使用 `common.Marshal` 发送飞书卡片。成功只接受 HTTP 2xx 且响应 `code == 0`，返回 204。

- [ ] **Step 4: 写失败分类、健康和指标测试**

覆盖以下精确行为：

- 非 POST 返回 405。
- 非 JSON Content-Type 返回 415。
- 非法 JSON、version 非 4、空 alerts 返回 400，飞书上游调用数保持 0。
- 请求体超过 1 MiB 返回 413。
- 飞书 HTTP 429/500 返回 502。
- 飞书 HTTP 200 但 `code=19022` 返回 502。
- 飞书超时返回 502。
- `/healthz` 返回 200；配置完成后 `/readyz` 返回 200。
- 指标只出现固定 `result`、`severity`、`status` 标签，不出现 alertname、channel_id 或错误文本。
- 日志和 HTTP 响应不包含完整 Webhook URL。

- [ ] **Step 5: 运行失败测试并确认 RED**

Run: `go test ./pkg/feishualert -run 'TestService|TestMetrics' -count=1 -v`

Expected: FAIL，缺少错误分类和指标。

- [ ] **Step 6: 实现失败处理与固定低基数指标**

注册：

```go
newapi_feishu_alert_requests_total{result}
newapi_feishu_alert_deliveries_total{result,severity,status}
newapi_feishu_alert_delivery_duration_seconds
newapi_feishu_alerts_total{severity,status}
newapi_feishu_alert_webhook_configured
```

`result` 只允许设计文档中的固定枚举。HTTP client 禁止重定向，飞书响应体最多读取 64 KiB。超时、网络、HTTP、飞书业务码和响应解析错误都记录不含 URL 的结构化字段，并向 Alertmanager 返回 502。

- [ ] **Step 7: 运行包测试和 race 测试并提交 Task 2**

Run:

```bash
go test ./pkg/feishualert -count=1
go test -race ./pkg/feishualert -count=1
```

Expected: PASS。

Commit:

```bash
git add pkg/feishualert/service.go pkg/feishualert/service_test.go
git commit -m "监控：实现飞书告警投递服务"
```

---

### Task 3: 增加独立服务入口和容器镜像

**Files:**
- Create: `cmd/feishu-alert-webhook/main.go`
- Create: `cmd/feishu-alert-webhook/main_test.go`
- Create: `deploy/monitoring/feishu-webhook/Dockerfile`

**Interfaces:**
- Consumes: Task 2 的 `NewService` 和 `Service.Handler()`。
- Produces: `/feishu-alert-webhook` 服务二进制；默认监听 `:8080`；`healthcheck` 子命令访问本地 `/readyz`。

- [ ] **Step 1: 写配置加载失败测试**

定义内部配置：

```go
type appConfig struct {
    ListenAddress string
    WebhookURL     string
    Location       *time.Location
}
```

测试默认读取 `/run/secrets/feishu_webhook_url`，默认监听 `:8080`，默认时区 `Asia/Shanghai`。空文件、只含空白、文件不存在和非法时区必须返回错误，错误中只能包含 secret 文件路径，不能包含文件内容。

- [ ] **Step 2: 运行测试并确认 RED**

Run: `go test ./cmd/feishu-alert-webhook -run TestLoadConfig -count=1 -v`

Expected: FAIL，因为入口和配置函数尚未定义。

- [ ] **Step 3: 实现配置加载、服务启动和优雅退出**

环境变量只保存非秘密配置：

```text
FEISHU_WEBHOOK_URL_FILE=/run/secrets/feishu_webhook_url
FEISHU_ALERT_LISTEN_ADDR=:8080
FEISHU_ALERT_TIMEZONE=Asia/Shanghai
```

使用 `os.ReadFile` 读取 secret，trim 后交给 `NewService` 校验。HTTP server 设置 `ReadHeaderTimeout=5s`、`ReadTimeout=15s`、`WriteTimeout=15s`、`IdleTimeout=60s`；接收 SIGINT/SIGTERM 后使用 10 秒 context shutdown。日志使用 `slog.NewJSONHandler(os.Stdout, nil)`。

- [ ] **Step 4: 写并实现 healthcheck 子命令测试**

`feishu-alert-webhook healthcheck` 请求 `http://127.0.0.1:<port>/readyz`，3 秒超时；200 退出 0，其他状态或网络错误退出非 0。测试使用 `httptest.Server` 验证两条路径。

- [ ] **Step 5: 运行入口测试并构建二进制**

Run:

```bash
go test ./cmd/feishu-alert-webhook -count=1
go build -o /tmp/newapi-feishu-alert-webhook ./cmd/feishu-alert-webhook
```

Expected: PASS，构建成功。

- [ ] **Step 6: 编写最小非 root Dockerfile**

Dockerfile 使用项目现有固定 Go builder 和 Debian runtime digest：

```dockerfile
FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder
ENV CGO_ENABLED=0
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/feishu-alert-webhook ./cmd/feishu-alert-webhook

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/feishu-alert-webhook /feishu-alert-webhook
USER 65532:65532
ENTRYPOINT ["/feishu-alert-webhook"]
```

- [ ] **Step 7: 构建镜像并提交 Task 3**

Run: `docker build -f deploy/monitoring/feishu-webhook/Dockerfile -t new-api-feishu-alert-webhook:test .`

Expected: image build exit 0，`docker image inspect new-api-feishu-alert-webhook:test` 显示运行用户 `65532:65532`。

Commit:

```bash
git add cmd/feishu-alert-webhook deploy/monitoring/feishu-webhook/Dockerfile
git commit -m "监控：增加飞书告警服务镜像"
```

---

### Task 4: 接入 Compose、Alertmanager 和 Prometheus

**Files:**
- Modify: `docker-compose.monitoring.yml`
- Modify: `deploy/monitoring/alertmanager.yml.example`
- Modify: `deploy/monitoring/prometheus.yml`
- Create: `deploy/monitoring/secrets/feishu-webhook-url.example`
- Delete: `deploy/monitoring/secrets/alertmanager-webhook-url.example`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Consumes: Task 3 的容器镜像和 `/api/v1/alerts`、`/readyz`、`/metrics`。
- Produces: 完整 Docker 内部告警投递链路。

- [ ] **Step 1: 先扩展静态校验并确认失败**

在 `validate.sh` 加入断言：

- 必须存在 `feishu-webhook-url.example`。
- Compose 必须存在 `feishu-alert-webhook`，不得配置 `ports`，必须 `read_only: true`、健康检查、`monitoring` 网络和 `feishu_webhook_url` secret。
- Alertmanager receiver URL 必须是 `http://feishu-alert-webhook:8080/api/v1/alerts`，不得再读取外部 Webhook secret。
- Prometheus 必须有 `job_name: feishu-alert-webhook`，target 为 `feishu-alert-webhook:8080` 且带 `cluster: default`。
- 所有 Compose 校验环境使用 `FEISHU_WEBHOOK_URL_FILE`，不再使用 `ALERTMANAGER_WEBHOOK_URL_FILE`。

Run: `PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool bash deploy/monitoring/validate.sh`

Expected: FAIL，因为 Compose 和配置尚未接入。

- [ ] **Step 2: 修改 Compose 服务和 secret**

新增服务核心配置：

```yaml
  feishu-alert-webhook:
    build:
      context: .
      dockerfile: deploy/monitoring/feishu-webhook/Dockerfile
    image: new-api-feishu-alert-webhook:local
    restart: unless-stopped
    read_only: true
    environment:
      FEISHU_WEBHOOK_URL_FILE: /run/secrets/feishu_webhook_url
      FEISHU_ALERT_TIMEZONE: Asia/Shanghai
    secrets:
      - feishu_webhook_url
    networks:
      - monitoring
    security_opt:
      - no-new-privileges:true
    healthcheck:
      test: ["CMD", "/feishu-alert-webhook", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

Alertmanager `depends_on` 该服务 healthy，并删除 `alertmanager_webhook_url` secret。顶层 secret 改为：

```yaml
  feishu_webhook_url:
    file: "${FEISHU_WEBHOOK_URL_FILE:?set FEISHU_WEBHOOK_URL_FILE}"
```

- [ ] **Step 3: 修改 Alertmanager receiver 和 Prometheus target**

两个 receiver 使用：

```yaml
webhook_configs:
  - url: http://feishu-alert-webhook:8080/api/v1/alerts
    send_resolved: true
    timeout: 15s
```

Prometheus 新增：

```yaml
  - job_name: feishu-alert-webhook
    static_configs:
      - targets:
          - feishu-alert-webhook:8080
        labels:
          cluster: default
```

示例 secret 内容固定为 `https://open.feishu.cn/open-apis/bot/v2/hook/example`，格式合法但不可用于生产；校验脚本只验证 scheme、host 和 path，不输出内容。

- [ ] **Step 4: 运行 Compose、Prometheus 和 Alertmanager 校验**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool bash deploy/monitoring/validate.sh
docker run --rm --entrypoint amtool -v "$PWD/deploy/monitoring/alertmanager.yml.example:/etc/alertmanager/alertmanager.yml:ro" prom/alertmanager:v0.28.1 check-config /etc/alertmanager/alertmanager.yml
```

Expected: 两个命令均 exit 0。

- [ ] **Step 5: 提交 Task 4**

```bash
git add docker-compose.monitoring.yml deploy/monitoring/alertmanager.yml.example deploy/monitoring/prometheus.yml deploy/monitoring/secrets deploy/monitoring/validate.sh
git commit -m "监控：接入飞书告警通知链路"
```

---

### Task 5: 中文化告警注释并完善部署文档

**Files:**
- Modify: `deploy/monitoring/alert-rules.yml`
- Modify: `docs/prometheus-monitoring.md`
- Modify: `docs/prometheus-monitoring-todolist.md`

**Interfaces:**
- Consumes: 现有 72 条告警规则和 Task 4 的环境变量/服务名称。
- Produces: 飞书卡片可直接展示的中文 summary/description 与可执行部署手册。

- [ ] **Step 1: 建立注释不变量并确认当前失败**

在 `validate.sh` 的 Alertmanager/规则契约检查中加入：每条 alert 必须有非空 `summary`、`description`，且不得改动现有 alertname 列表和规则总数 72。保存实施前的 alertname/expr/for/labels 规范化摘要，翻译后比较这些字段完全一致。

Run: `rg -n '^[[:space:]]+(summary|description): [A-Za-z]' deploy/monitoring/alert-rules.yml`

Expected: 列出当前服务、渠道、数据库、Runtime、Redis、Billing 等英文注释。

- [ ] **Step 2: 只翻译 summary 和 description**

翻译原则：

- `new-api`、Relay、P95、P99、TTFT、HTTP 状态码、error_type、channel_id 等技术标识保留。
- 动态模板表达式 `{{ $labels.* }}` 原样保留。
- summary 使用简短中文结果，例如 `new-api 服务成功率低于 95%`。
- description 使用动作导向中文，例如 `请检查集群 {{ $labels.cluster }}、任务 {{ $labels.job }} 的上游健康状态和 error_type 指标。`
- 不修改 `expr`、`for`、labels 或 alertname。

- [ ] **Step 3: 更新部署文档**

文档必须给出：

```bash
export FEISHU_WEBHOOK_URL_FILE=/data/new-api-ver/runtime/secrets/feishu-webhook-url
install -o root -g 65532 -m 0640 /dev/null "$FEISHU_WEBHOOK_URL_FILE"
read -rsp '飞书 Webhook URL: ' FEISHU_WEBHOOK_URL; printf '\n'
printf '%s\n' "$FEISHU_WEBHOOK_URL" >"$FEISHU_WEBHOOK_URL_FILE"
unset FEISHU_WEBHOOK_URL
export NEW_API_DOCKER_NETWORK=new-api-ver_new-api-network
docker compose -f docker-compose.monitoring.yml build feishu-alert-webhook
docker compose -f docker-compose.monitoring.yml up -d feishu-alert-webhook alertmanager prometheus
```

同时说明在飞书机器人 IP 白名单加入 `198.44.181.187`，secret 文件写入完整 URL，容器不得使用仓库 `.example` 文件。验收命令包含容器健康、Prometheus target、转换服务指标、Alertmanager 日志和 firing/resolved 实际卡片。

- [ ] **Step 4: 运行规则与文档校验**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool bash deploy/monitoring/validate.sh
git diff --check
```

Expected: PASS；72 条告警数量不变，非注释字段摘要不变。

- [ ] **Step 5: 提交 Task 5**

```bash
git add deploy/monitoring/alert-rules.yml deploy/monitoring/validate.sh docs/prometheus-monitoring.md docs/prometheus-monitoring-todolist.md
git commit -m "监控：中文化飞书告警内容"
```

---

### Task 6: 全量验证和服务器部署准备

**Files:**
- Modify only if verification exposes a defect in files owned by Tasks 1-5.

**Interfaces:**
- Consumes: Tasks 1-5 的完整通知链路。
- Produces: 可部署 commit、验证证据和服务器操作清单。

- [ ] **Step 1: 运行定向与 race 测试**

Run:

```bash
go test ./pkg/feishualert ./cmd/feishu-alert-webhook -count=1
go test -race ./pkg/feishualert ./cmd/feishu-alert-webhook -count=1
```

Expected: PASS，无 race。

- [ ] **Step 2: 运行全量 Go 测试**

Run: `go test ./... -count=1`

Expected: PASS。

- [ ] **Step 3: 运行监控静态校验和镜像检查**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool bash deploy/monitoring/validate.sh
docker build -f deploy/monitoring/feishu-webhook/Dockerfile -t new-api-feishu-alert-webhook:test .
docker image inspect new-api-feishu-alert-webhook:test
git diff --check
```

Expected: 全部 exit 0，镜像用户为 `65532:65532`。

- [ ] **Step 4: 做本地端到端测试**

用本地 fake Feishu server 启动转换服务，向 `/api/v1/alerts` 发送一条 critical firing 和对应 resolved。精确验证：两次飞书请求；第一张 red 且包含 `<at id=all></at>`；第二张 green 且不包含 mention；`newapi_feishu_alert_deliveries_total{result="success"}` 增加 2。

- [ ] **Step 5: 做秘密和暂存区审计**

Run:

```bash
git grep -n 'open-apis/bot/v2/hook/' -- ':!deploy/monitoring/secrets/*.example' ':!docs/**'
git status --short
git diff --cached --name-only
```

Expected: 代码和配置中没有真实飞书 token；受保护用户改动仍未暂存。

- [ ] **Step 6: 准备服务器部署**

在服务器确认 `curl -4 https://api.ipify.org` 返回 `198.44.181.187`。获取用户提供的真实飞书 Webhook 后，以不回显方式写入 `/data/new-api-ver/runtime/secrets/feishu-webhook-url`，设置 root:65532 和 0640，再拉取代码、build/up 服务。没有真实 Webhook URL 时停止在此步骤，不用示例 URL冒充生产验收。

- [ ] **Step 7: 生产验收**

触发受控 critical firing、warning firing 和 resolved，确认飞书颜色及提醒策略；检查 `docker logs` 无持续投递失败，Prometheus target 为 up，Alertmanager 使用最新 18 条抑制规则。验收通过后创建最终中文提交或部署记录提交。
