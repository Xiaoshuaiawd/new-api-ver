# Four-Layer Prometheus Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mixed Grafana monitoring layout with four focused Chinese dashboards backed by host, application, middleware, and per-channel metrics.

**Architecture:** Keep new-api application metrics in the existing isolated Prometheus registry, add per-channel TTFT and normalized Token counters, and collect host/middleware metrics through dedicated exporters. Prometheus recording rules provide stable one-minute channel traffic indicators and five-minute latency/cache indicators. Grafana uses separate core and extended providers so the four daily-operation dashboards stay isolated from billing and task dashboards.

**Tech Stack:** Go 1.22+, prometheus/client_golang, Prometheus 3.5, Grafana 12.1, Docker Compose, Node Exporter, PostgreSQL Exporter, MySQL Exporter, Redis Exporter.

## Global Constraints

- Preserve all existing uncommitted work outside files listed in each task.
- Keep SQLite, MySQL, and PostgreSQL application compatibility.
- Use `channel_id` and fixed low-cardinality enums only; never add user, token, model, request ID, URL, Redis Key, or raw error labels.
- Channel real-time rates use a one-minute window; latency and cache ratios use a five-minute window.
- Channel cache hit ratio is cache-read Token divided by total input Token.
- Keep `newapi_stream_ttft_seconds{relay_format}` for compatibility and add a separate channel metric.
- Exporter credentials must come from runtime secrets or environment files and must never be committed.
- Do not delete Prometheus, Grafana, PostgreSQL, MySQL, or Redis data volumes.
- All Grafana user-facing copy is Chinese; metric identifiers, PromQL labels, UIDs, and protected project identity remain unchanged.

---

### Task 1: Add channel TTFT and normalized Token metrics

**Files:**
- Modify: `pkg/prometheus_metrics/channel.go`
- Modify: `pkg/prometheus_metrics/channel_test.go`
- Modify: `service/channel_runtime_metrics.go`
- Modify: `service/channel_runtime_metrics_test.go`
- Modify: `controller/relay.go`
- Modify: `relay/common/relay_info.go`
- Create: `relay/common/relay_info_test.go`
- Modify: `relay/channel/cloudflare/relay_cloudflare.go`
- Modify: `relay/channel/cohere/relay-cohere.go`
- Modify: `service/text_quota.go`
- Modify: `service/quota.go`
- Test: `controller/relay_metrics_test.go`
- Test: `service/text_quota_test.go`

**Interfaces:**
- Consumes: `ChannelAttemptMeta`, `ChannelAttemptOutcome`, normalized `dto.Usage`, and `relayInfo.ChannelId/ChannelType`.
- Produces: `newapi_channel_ttft_seconds{channel_id,channel_type}` and `newapi_channel_tokens_total{channel_id,channel_type,token_type}`.

- [ ] **Step 1: Write failing metric registration and validation tests**

Add deterministic tests that expect these exact observable contracts:

```go
func TestChannelAttemptRecordsSuccessfulStreamTTFT(t *testing.T) {
    runtime := activateMetricsTestRuntime(t)
    attempt := BeginChannelAttempt(ChannelAttemptMeta{ChannelID: 12, ChannelType: 5, Stream: true})
    attempt.Done(ChannelAttemptOutcome{Success: true, TTFT: 750 * time.Millisecond})

    metrics := scrapeMetrics(t, runtime)
    assert.Contains(t, metrics, `newapi_channel_ttft_seconds_count{channel_id="12",channel_type="5"} 1`)
}

func TestRecordChannelTokensUsesFixedTokenTypesAndIgnoresInvalidValues(t *testing.T) {
    runtime := activateMetricsTestRuntime(t)
    RecordChannelTokens(12, 5, ChannelTokenUsage{Input: 100, Output: 40, CacheRead: 25})
    RecordChannelTokens(0, 5, ChannelTokenUsage{Input: 999})
    RecordChannelTokens(12, 5, ChannelTokenUsage{Input: -1, Output: -1, CacheRead: -1})

    metrics := scrapeMetrics(t, runtime)
    assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="input"} 100`)
    assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="output"} 40`)
    assert.Contains(t, metrics, `newapi_channel_tokens_total{channel_id="12",channel_type="5",token_type="cache_read"} 25`)
}
```

- [ ] **Step 2: Run focused tests and verify they fail for missing APIs**

Run:

```bash
go test ./pkg/prometheus_metrics ./service ./controller -run 'Test(ChannelAttemptRecordsSuccessfulStreamTTFT|RecordChannelTokensUsesFixedTokenTypesAndIgnoresInvalidValues)' -count=1
```

Expected: FAIL because the new outcome type, histogram, CounterVec, or recording API does not yet exist.

- [ ] **Step 3: Extend the channel metric lifecycle**

Implement the following stable domain types in `pkg/prometheus_metrics/channel.go`:

```go
const (
    ChannelTokenTypeInput     = "input"
    ChannelTokenTypeOutput    = "output"
    ChannelTokenTypeCacheRead = "cache_read"
)

type ChannelAttemptOutcome struct {
    Success bool
    Error   ErrorDetails
    TTFT    time.Duration
}

type ChannelTokenUsage struct {
    Input     int
    Output    int
    CacheRead int
}
```

Add `ttft *prometheus.HistogramVec` and `tokens *prometheus.CounterVec` to `channelMetrics`. Register:

```go
prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name: "newapi_channel_ttft_seconds",
    Help: "Successful streaming channel time to first response in seconds.",
    Buckets: ttftBuckets,
}, []string{"channel_id", "channel_type"})

prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "newapi_channel_tokens_total",
    Help: "Normalized channel token usage by fixed token type.",
}, []string{"channel_id", "channel_type", "token_type"})
```

Change `ChannelAttempt.Done` to accept one `ChannelAttemptOutcome`. Record TTFT only when `meta.Stream`, `outcome.Success`, and `outcome.TTFT > 0`. Add `RecordChannelTokens(channelID, channelType int, usage ChannelTokenUsage)` that no-ops for missing runtime, invalid channel ID, and negative values, and increments only positive counters.

- [ ] **Step 4: Thread TTFT through the unified attempt boundary**

Change the service alias and wrapper:

```go
type ChannelAttemptOutcome = prometheusmetrics.ChannelAttemptOutcome
```

Extend `RelayInfo` with current-attempt timing state and methods with these contracts:

- `BeginChannelAttempt()` resets the current attempt start time and clears only the current attempt first-response time.
- `SetFirstResponseTime()` keeps the existing Relay-global first response behavior, and also sets the current attempt first response once.
- `ChannelAttemptTTFT()` returns a positive duration only when the current attempt has both a start and first response.

Call `relayInfo.BeginChannelAttempt()` immediately before invoking the selected channel handler inside each retry callback. Do not reset the Relay-global `FirstResponseTime`, because existing logs and `newapi_stream_ttft_seconds{relay_format}` depend on it.

Replace the direct `FirstResponseTime = time.Now()` assignments in the Cloudflare and Cohere stream paths with `SetFirstResponseTime()` so all channel implementations update both clocks consistently.

Add deterministic `relay/common` tests proving that:

1. the first Relay-global response time is preserved across retries;
2. current-attempt timing resets for each retry;
3. the second attempt TTFT does not include the first attempt duration.

In `TrackChannelAttempt`, pass the complete outcome once. In `controller/relay.go`, populate `ChannelAttemptOutcome.TTFT` from `relayInfo.ChannelAttemptTTFT()` inside the retry attempt callback. Keep `relayMetricsOutcome` on the original Relay-global timing so the existing Relay-level TTFT metric remains backward compatible.

- [ ] **Step 5: Record normalized Token usage once at settlement**

Add one service helper that derives counters from the already normalized effective billing usage:

```go
func recordChannelTokenMetrics(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) {
    if relayInfo == nil || usage == nil {
        return
    }
    input := usage.InputTokens
    if input <= 0 {
        input = usage.PromptTokens
    }
    output := usage.OutputTokens
    if output <= 0 {
        output = usage.CompletionTokens
    }
    cacheRead := usage.PromptTokensDetails.CachedTokens
    if cacheRead <= 0 && usage.InputTokensDetails != nil {
        cacheRead = usage.InputTokensDetails.CachedTokens
    }
    if cacheRead <= 0 {
        cacheRead = usage.PromptCacheHitTokens
    }
    prometheusmetrics.RecordChannelTokens(relayInfo.ChannelId, relayInfo.ChannelType, prometheusmetrics.ChannelTokenUsage{
        Input: input, Output: output, CacheRead: cacheRead,
    })
}
```

Call it from `PostTextConsumeQuota` using `billingUsage`, and from `PostAudioConsumeQuota` using its normalized Usage. Do not call it from pre-consume or error-log paths.

- [ ] **Step 6: Run focused and package tests**

Run:

```bash
go test ./pkg/prometheus_metrics ./service ./controller -count=1
```

Expected: PASS with no duplicate registry registration and exact channel Token/TTFT series.

- [ ] **Step 7: Commit Task 1**

```bash
git add pkg/prometheus_metrics/channel.go pkg/prometheus_metrics/channel_test.go service/channel_runtime_metrics.go service/channel_runtime_metrics_test.go controller/relay.go relay/common/relay_info.go relay/common/relay_info_test.go relay/channel/cloudflare/relay_cloudflare.go relay/channel/cohere/relay-cohere.go service/text_quota.go service/quota.go controller/relay_metrics_test.go service/text_quota_test.go
git commit -m '监控：增加渠道TTFT与Token指标'
```

---

### Task 2: Add channel recording rules and rule tests

**Files:**
- Modify: `deploy/monitoring/recording-rules.yml`
- Modify: `deploy/monitoring/recording-rules.test.yml`
- Modify: `deploy/monitoring/alert-rules.yml`
- Modify: `deploy/monitoring/alert-rules.test.yml`

**Interfaces:**
- Consumes: channel attempt, retry, duration, first-byte, TTFT, and Token series.
- Produces: the exact `newapi:channel_*` rules used by the channel dashboard.

- [ ] **Step 1: Add failing promtool fixtures for one-minute rates and five-minute ratios**

Add explicit input series for two channels and expected output for:

```text
newapi:channel_attempt_rpm:1m
newapi:channel_failure_rpm:1m
newapi:channel_retry_rpm:1m
newapi:channel_success_ratio:1m
newapi:channel_timeout_ratio:1m
newapi:channel_duration_seconds:p90_5m
newapi:channel_duration_seconds:p95_5m
newapi:channel_ttft_seconds:p95_5m
newapi:channel_first_byte_seconds:p95_5m
newapi:channel_cache_hit_ratio:5m
newapi:channel_tokens_per_minute:5m
```

Expected fixtures must retain `cluster`, `job`, and `channel_id` and must prove that channel 7 and channel 9 are not merged.

- [ ] **Step 2: Run promtool tests and verify missing-rule failure**

Run:

```bash
promtool test rules deploy/monitoring/recording-rules.test.yml
```

Expected: FAIL because the new record names do not exist.

- [ ] **Step 3: Implement the recording rules**

Use `sum by (cluster, job, channel_id)` for counters, `sum by (cluster, job, channel_id, le)` before `histogram_quantile`, and `clamp_min(..., 0.000001)` for denominators. Preserve `token_type` in `newapi:channel_tokens_per_minute:5m`.

The timeout ratio numerator must select `result="failure",error_type="timeout"`. Stream interruption panels use existing raw attempt labels and do not need a duplicate recording rule.

- [ ] **Step 4: Keep existing alert behavior compatible**

Update alert expressions only where renamed five-minute channel rules are consumed. Do not change thresholds or `for` durations. Add or update rule fixtures so existing channel alerts still pass.

- [ ] **Step 5: Validate all rules**

Run:

```bash
promtool check rules --lint=all --lint-fatal deploy/monitoring/recording-rules.yml deploy/monitoring/alert-rules.yml deploy/monitoring/relay-latency-thresholds.yml deploy/monitoring/relay-concurrency-thresholds.yml
promtool test rules deploy/monitoring/recording-rules.test.yml
promtool test rules deploy/monitoring/alert-rules.test.yml
```

Expected: all checks and unit tests PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add deploy/monitoring/recording-rules.yml deploy/monitoring/recording-rules.test.yml deploy/monitoring/alert-rules.yml deploy/monitoring/alert-rules.test.yml
git commit -m '监控：增加渠道实时聚合规则'
```

---

### Task 3: Add host and middleware exporters

**Files:**
- Modify: `docker-compose.monitoring.yml`
- Modify: `deploy/monitoring/prometheus.yml`
- Create: `deploy/monitoring/targets/postgres-exporter.yml`
- Create: `deploy/monitoring/targets/mysql-exporter.yml`
- Modify: `deploy/monitoring/validate.sh`
- Modify: `deploy/monitoring/secrets/.gitignore`
- Create: `deploy/monitoring/secrets/postgres-exporter-password.example`
- Create: `deploy/monitoring/secrets/redis-exporter-password.example`
- Create: `deploy/monitoring/secrets/mysql-exporter.cnf.example`

**Interfaces:**
- Consumes: an external application Docker network and runtime credentials.
- Produces: `node_*`, `pg_*`, `mysql_*`, and `redis_*` scrape series.

- [ ] **Step 1: Add Compose services with pinned images**

Add:

```yaml
node-exporter:
  image: prom/node-exporter:v1.9.1
  command:
    - --path.procfs=/host/proc
    - --path.sysfs=/host/sys
    - --path.rootfs=/rootfs
    - --collector.filesystem.mount-points-exclude=^/(dev|proc|sys|var/lib/docker/.+|var/lib/containers/storage/.+)($|/)
  volumes:
    - /proc:/host/proc:ro
    - /sys:/host/sys:ro
    - /:/rootfs:ro,rslave
  networks: [monitoring]

postgres-exporter:
  image: prometheuscommunity/postgres-exporter:v0.17.1
  profiles: [postgres]
  environment:
    DATA_SOURCE_URI: ${POSTGRES_EXPORTER_URI:?set POSTGRES_EXPORTER_URI}
    DATA_SOURCE_USER: ${POSTGRES_EXPORTER_USER:?set POSTGRES_EXPORTER_USER}
    DATA_SOURCE_PASS_FILE: /run/secrets/postgres_exporter_password
  secrets: [postgres_exporter_password]
  networks: [monitoring, application]

mysqld-exporter:
  image: prom/mysqld-exporter:v0.17.2
  profiles: [mysql]
  command: [--config.my-cnf=/run/secrets/mysql_exporter_cnf]
  secrets: [mysql_exporter_cnf]
  networks: [monitoring, application]

redis-exporter:
  image: oliver006/redis_exporter:v1.67.0
  environment:
    REDIS_ADDR: ${REDIS_EXPORTER_ADDR:-redis://redis:6379}
    REDIS_PASSWORD_FILE: /run/secrets/redis_exporter_password
  secrets: [redis_exporter_password]
  networks: [monitoring, application]
```

Define external network `application` with name `${NEW_API_DOCKER_NETWORK:?set NEW_API_DOCKER_NETWORK}`. Do not publish exporter ports.

- [ ] **Step 2: Add Prometheus scrape jobs and optional database target files**

Mount `deploy/monitoring/targets` into Prometheus. Add jobs for `node-exporter`, `postgres-exporter`, `mysql-exporter`, and `redis-exporter`. PostgreSQL targets contain `postgres-exporter:9187`; MySQL targets are an empty YAML list until the MySQL profile is enabled.

Every active target must receive fixed `cluster: default` and a readable `instance` label.

- [ ] **Step 3: Extend validation**

Require the target files and exporter secret examples in `validate.sh`. Add Compose validation commands for the PostgreSQL and MySQL profiles using non-secret example paths and dummy non-production values.

- [ ] **Step 4: Validate Compose and Prometheus configuration**

Run:

```bash
PROMETHEUS_BEARER_TOKEN_FILE=deploy/monitoring/secrets/new-api-bearer-token.example \
GRAFANA_ADMIN_PASSWORD_FILE=deploy/monitoring/secrets/grafana-admin-password.example \
ALERTMANAGER_WEBHOOK_URL_FILE=deploy/monitoring/secrets/alertmanager-webhook-url.example \
POSTGRES_EXPORTER_PASSWORD_FILE=deploy/monitoring/secrets/postgres-exporter-password.example \
REDIS_EXPORTER_PASSWORD_FILE=deploy/monitoring/secrets/redis-exporter-password.example \
MYSQL_EXPORTER_CONFIG_FILE=deploy/monitoring/secrets/mysql-exporter.cnf.example \
POSTGRES_EXPORTER_URI=postgres:5432/new-api?sslmode=disable \
POSTGRES_EXPORTER_USER=example \
NEW_API_DOCKER_NETWORK=example \
docker compose -f docker-compose.monitoring.yml --profile postgres config --quiet
```

Repeat with `--profile mysql`. Run `deploy/monitoring/validate.sh` with a real `PROMTOOL_BIN`.

- [ ] **Step 5: Commit Task 3**

```bash
git add docker-compose.monitoring.yml deploy/monitoring/prometheus.yml deploy/monitoring/targets deploy/monitoring/validate.sh deploy/monitoring/secrets
git commit -m '监控：接入主机与中间件Exporter'
```

---

### Task 4: Rebuild Grafana provisioning and dashboards

**Files:**
- Modify: `deploy/monitoring/grafana/provisioning/dashboards/default.yml`
- Create: `deploy/monitoring/grafana/dashboards/core/host-overview.json`
- Create: `deploy/monitoring/grafana/dashboards/core/application-overview.json`
- Create: `deploy/monitoring/grafana/dashboards/core/middleware-overview.json`
- Move/Modify: `deploy/monitoring/grafana/dashboards/channel-overview.json` → `deploy/monitoring/grafana/dashboards/core/channel-overview.json`
- Move: `deploy/monitoring/grafana/dashboards/billing-overview.json` → `deploy/monitoring/grafana/dashboards/extended/billing-overview.json`
- Move: `deploy/monitoring/grafana/dashboards/task-overview.json` → `deploy/monitoring/grafana/dashboards/extended/task-overview.json`
- Delete: `deploy/monitoring/grafana/dashboards/system-overview.json`

**Interfaces:**
- Consumes: standard exporter metrics and Task 2 recording rules.
- Produces: four core Chinese dashboards and two extended Chinese dashboards.

- [ ] **Step 1: Split provisioning into two providers**

Use two providers with `disableDeletion: false`, `editable: false`, and `updateIntervalSeconds: 30`:

```yaml
providers:
  - name: new-api-core-monitoring
    folder: new-api 监控
    options:
      path: /var/lib/grafana/dashboards/core
  - name: new-api-extended-monitoring
    folder: new-api 扩展监控
    options:
      path: /var/lib/grafana/dashboards/extended
```

- [ ] **Step 2: Build the host dashboard**

Create UID `newapi-host-overview` with Chinese variables and panels for CPU usage/modes/load, memory/Swap, filesystem usage, disk read/write bytes, IOPS, and I/O time. Exclude pseudo filesystems with the same regex used by Node Exporter.

- [ ] **Step 3: Build the application dashboard**

Create UID `newapi-application-overview` with an instance table and three focused trend areas:

```promql
go_memstats_heap_alloc_bytes{cluster="$cluster",job="new-api",instance=~"$instance"}
go_goroutines{cluster="$cluster",job="new-api",instance=~"$instance"}
rate(process_cpu_seconds_total{cluster="$cluster",job="new-api",instance=~"$instance"}[5m])
```

Also show the existing 30-minute heap and goroutine growth recording rules. Do not include Relay, database, Redis, billing, or task panels.

- [ ] **Step 4: Build the middleware dashboard**

Create UID `newapi-middleware-overview` with collapsible PostgreSQL, MySQL, and Redis rows. Use exporter `up` plus service metrics for connections, throughput, cache hit ratio, locks/errors, memory, clients, evictions, expirations, and capacity. Missing optional exporters display “未启用/暂无数据”, not 0.

- [ ] **Step 5: Rebuild the channel dashboard from the approved layout**

Preserve UID `newapi-channel-overview`. Add one channel-per-row table with real-time RPM, failure RPM, retry RPM, success ratio, timeout ratio, inflight, P90, P95, TTFT P95, cache hit ratio, and first-byte P95. Add the four approved rows: traffic/stability, performance/cache, error/retry diagnostics, and Token throughput.

All PromQL must group by `channel_id`; table queries must use instant mode and trends must use range mode. Preserve literal template labels such as `{{channel_id}}` while translating surrounding copy.

- [ ] **Step 6: Move extended dashboards and retire the old mixed system dashboard**

Move billing/task JSON without changing UIDs or PromQL. Remove the old system overview file after host/application/middleware replacements exist.

- [ ] **Step 7: Validate dashboard JSON and query invariants**

Run:

```bash
jq empty deploy/monitoring/grafana/dashboards/core/*.json deploy/monitoring/grafana/dashboards/extended/*.json
rg -n 'No data|Overview|Cluster|Instance|Success|Failure|Retry|Channel|Billing|Task' deploy/monitoring/grafana/dashboards
git diff --check
```

Extract every `.targets[].expr` and verify channel dashboard expressions that aggregate channel metrics retain `channel_id`.

- [ ] **Step 8: Commit Task 4**

```bash
git add deploy/monitoring/grafana
git commit -m '监控：重构四层中文仪表盘'
```

---

### Task 5: Update documentation and deploy to the current server

**Files:**
- Modify: `docs/prometheus-monitoring.md`
- Modify: `docs/prometheus-monitoring-todolist.md`
- Modify: `README.md` only if an existing monitoring link requires a path correction; protected identity text must remain untouched.

**Interfaces:**
- Consumes: completed application/config/dashboard artifacts.
- Produces: reproducible deployment steps and a verified running server.

- [ ] **Step 1: Document the four dashboards and exporter setup**

Document exact metric formulas, dashboard folder structure, Docker network variable, PostgreSQL default profile, MySQL switch procedure, secret files, and the difference between channel TTFT, first-byte latency, and application cache lookup rate.

- [ ] **Step 2: Run the full local verification suite**

Run:

```bash
go test ./pkg/prometheus_metrics ./service ./controller -count=1
deploy/monitoring/validate.sh
jq empty deploy/monitoring/grafana/dashboards/core/*.json deploy/monitoring/grafana/dashboards/extended/*.json
git diff --check
```

Expected: all commands PASS.

- [ ] **Step 3: Commit documentation**

```bash
git add docs/prometheus-monitoring.md docs/prometheus-monitoring-todolist.md
git commit -m '文档：更新四层监控部署说明'
```

- [ ] **Step 4: Prepare runtime-only exporter configuration on `198.44.181.187`**

Read the existing Docker network name and current PostgreSQL/Redis runtime environment without printing secrets. Create only the required runtime secret files under `/data/new-api-ver/runtime/secrets`, with mode `0600`. Do not add `runtime/` or credentials to Git.

- [ ] **Step 5: Synchronize code and start exporters**

Pull or copy the committed branch into `/data/new-api-ver`, then start the monitoring stack with the PostgreSQL profile and existing runtime environment. Do not recreate PostgreSQL, Redis, Prometheus, Grafana, or application data volumes.

- [ ] **Step 6: Verify live targets and dashboards**

Verify:

```text
new-api target: UP
node-exporter target: UP
postgres-exporter target: UP
redis-exporter target: UP
Grafana: running healthy
```

Use Grafana API to confirm:

```text
new-api 监控: 主机总览、程序总览、中间件总览、渠道总览
new-api 扩展监控: 计费总览、任务总览
```

Send real traffic through at least two channels and confirm their `channel_id` values appear separately in RPM, P95, TTFT, cache ratio, and Token series.

- [ ] **Step 7: Report the deployment result**

Report dashboard URLs, exporter health, tests run, commits created, and any metric that remains “暂无数据” solely because the corresponding provider has not returned Usage or no real request sample exists.
