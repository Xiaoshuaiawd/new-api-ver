# Prometheus Relay Latency Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable, default-dormant P95/P99 Relay latency alerts whose thresholds are maintained in a dedicated Prometheus rule file per cluster, job, and relay format.

**Architecture:** Keep application metrics unchanged and add a default-empty Prometheus threshold rule file that emits `newapi_relay_latency_threshold_seconds` only after operators calibrate a format. Recording rules provide per-format request volume, alert rules join observed percentiles to configured thresholds, Alertmanager suppresses P95 warning when matching P99 critical is active, and Grafana overlays the configured thresholds on the existing latency panel.

**Tech Stack:** Prometheus 3.5 rule files and PromQL, promtool rule tests, Alertmanager 0.28 inhibition rules, Grafana 12 dashboard JSON, Docker Compose, POSIX shell, Ruby YAML validation.

## Global Constraints

- Do not modify Relay, billing, refunds, retries, routing, or client response behavior.
- Do not add application metrics; reuse `newapi_relay_duration_seconds` and `newapi_relay_requests_total`.
- Threshold labels are exactly `cluster,job,relay_format,quantile`; never add model, channel, user, Token, IP, Request ID, or error text.
- `quantile` is limited to `p95|p99`; `relay_format` is limited to `openai|claude|gemini|openai_responses|openai_responses_compaction|openai_alpha_search|openai_audio|openai_image|openai_realtime|rerank|embedding|task|mj_proxy|other`.
- Every threshold is a finite positive number of seconds expressed as `vector(<number>)`.
- The checked-in threshold file is empty, so latency alerts are dormant by default.
- P95 warning requires at least 50 final Relay requests in 5 minutes and remains true for 10 minutes.
- P99 critical requires at least 100 final Relay requests in 5 minutes and remains true for 10 minutes.
- Production baseline calibration stays unchecked in the TODO until real production evidence exists.
- Each behavior change follows RED→GREEN and updates `docs/prometheus-monitoring-todolist.md` only after reproducible verification.

---

### Task 1: Default-empty latency threshold rule file and deployment validation

**Files:**
- Create: `deploy/monitoring/relay-latency-thresholds.yml`
- Modify: `deploy/monitoring/prometheus.yml`
- Modify: `docker-compose.monitoring.yml`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Produces: optional metric `newapi_relay_latency_threshold_seconds{cluster,job,relay_format,quantile}`
- Produces: a checked-in zero-threshold rule file that passes `promtool check rules`
- Produces: validation that rejects malformed, duplicate, unknown-format, non-positive, or non-constant thresholds

- [ ] **Step 1: Extend the validation contract before creating the threshold file**

Add the threshold file to `required_files`, add its container-path replacement to the temporary Prometheus config, and include it in `promtool check rules`:

```sh
$monitoring_dir/relay-latency-thresholds.yml
```

```sh
-e "s#/etc/prometheus/rules/relay-latency-thresholds.yml#$monitoring_dir/relay-latency-thresholds.yml#" \
```

```sh
"$promtool_bin" check rules --lint=all --lint-fatal \
  "$monitoring_dir/recording-rules.yml" \
  "$monitoring_dir/alert-rules.yml" \
  "$monitoring_dir/relay-latency-thresholds.yml"
```

Add an always-run Ruby contract validator after `promtool check rules`:

```ruby
require "yaml"

allowed_formats = %w[
  openai claude gemini openai_responses openai_responses_compaction
  openai_alpha_search openai_audio openai_image openai_realtime rerank
  embedding task mj_proxy other
]
allowed_quantiles = %w[p95 p99]
document = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
rules = document.fetch("groups").flat_map { |group| group.fetch("rules") }
seen = {}

rules.each do |rule|
  abort "latency threshold record name is invalid" unless rule.fetch("record") == "newapi_relay_latency_threshold_seconds"
  labels = rule.fetch("labels")
  expected_keys = %w[cluster job quantile relay_format]
  abort "latency threshold labels are invalid" unless labels.keys.sort == expected_keys.sort
  abort "latency threshold cluster/job must be non-empty" if labels.fetch("cluster").to_s.empty? || labels.fetch("job").to_s.empty?
  abort "latency threshold relay_format is invalid" unless allowed_formats.include?(labels.fetch("relay_format"))
  abort "latency threshold quantile is invalid" unless allowed_quantiles.include?(labels.fetch("quantile"))

  match = /\Avector\(([^()]+)\)\z/.match(rule.fetch("expr").to_s.strip)
  abort "latency threshold must use vector(<positive seconds>)" unless match
  seconds = Float(match[1], exception: false)
  abort "latency threshold must be finite and positive" unless seconds&.finite? && seconds.positive?

  key = expected_keys.map { |name| labels.fetch(name) }
  abort "duplicate latency threshold #{key.join("/")}" if seen[key]
  seen[key] = true
end
```

- [ ] **Step 2: Run validation to verify RED**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: FAIL with `missing required monitoring artifact: .../relay-latency-thresholds.yml`.

- [ ] **Step 3: Create the default-empty threshold file and wire it into Prometheus**

Create:

```yaml
groups:
  - name: newapi-relay-latency-thresholds
    rules: []
```

Add to `deploy/monitoring/prometheus.yml`:

```yaml
rule_files:
  - /etc/prometheus/rules/recording-rules.yml
  - /etc/prometheus/rules/alert-rules.yml
  - /etc/prometheus/rules/relay-latency-thresholds.yml
```

Add to the Prometheus volumes in `docker-compose.monitoring.yml`:

```yaml
- ./deploy/monitoring/relay-latency-thresholds.yml:/etc/prometheus/rules/relay-latency-thresholds.yml:ro
```

Add a static mount assertion to `validate.sh`:

```sh
rg -q 'relay-latency-thresholds.yml:/etc/prometheus/rules/relay-latency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
```

- [ ] **Step 4: Run validation to verify GREEN**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: PASS; promtool reports `0 rules found` for the threshold file and the existing base rule counts remain unchanged.

- [ ] **Step 5: Prove invalid threshold contracts are rejected**

Temporarily replace the empty rule list in a copied fixture with each invalid case and run the same Ruby validator: unknown `relay_format`, `quantile: p90`, `vector(0)`, `vector(-1)`, `vector(NaN)`, extra label, and duplicate key. Keep these fixtures in a temporary directory created by `validate.sh`; do not commit invalid YAML files.

Expected: each fixture exits non-zero with the matching contract error, while the checked-in empty file passes.

- [ ] **Step 6: Commit Task 1**

```bash
git add deploy/monitoring/relay-latency-thresholds.yml deploy/monitoring/prometheus.yml docker-compose.monitoring.yml deploy/monitoring/validate.sh
git commit -m "监控：增加Relay延迟阈值配置"
```

---

### Task 2: Per-format request volume and P95/P99 alert rules

**Files:**
- Modify: `deploy/monitoring/recording-rules.test.yml`
- Modify: `deploy/monitoring/recording-rules.yml`
- Modify: `deploy/monitoring/alert-rules.test.yml`
- Modify: `deploy/monitoring/alert-rules.yml`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Consumes: `newapi_relay_requests_total{cluster,job,instance,relay_format,...}`
- Consumes: existing `newapi:relay_duration_seconds:p95_5m` and `newapi:relay_duration_seconds:p99_5m`
- Consumes: optional `newapi_relay_latency_threshold_seconds`
- Produces: `newapi:relay_request_increase_by_format:5m{cluster,job,relay_format}`
- Produces: `NewAPIRelayP95LatencyHigh` warning and `NewAPIRelayP99LatencyHigh` critical

- [ ] **Step 1: Write the failing per-format request-volume test**

Add a recording-rule test with two instances and two formats:

```yaml
- name: relay request volume preserves relay format
  interval: 1m
  input_series:
    - series: 'newapi_relay_requests_total{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false",result="success",error_type="none"}'
      values: '0+12x6'
    - series: 'newapi_relay_requests_total{cluster="test",job="new-api",instance="app-2",relay_format="openai",stream="true",result="failure",error_type="timeout"}'
      values: '0+8x6'
    - series: 'newapi_relay_requests_total{cluster="test",job="new-api",instance="app-1",relay_format="openai_image",stream="false",result="success",error_type="none"}'
      values: '0+4x6'
  promql_expr_test:
    - expr: newapi:relay_request_increase_by_format:5m
      eval_time: 6m
      exp_samples:
        - labels: '{__name__="newapi:relay_request_increase_by_format:5m",cluster="test",job="new-api",relay_format="openai"}'
          value: 100
        - labels: '{__name__="newapi:relay_request_increase_by_format:5m",cluster="test",job="new-api",relay_format="openai_image"}'
          value: 20
```

- [ ] **Step 2: Write failing alert tests**

Add fixed-input tests covering four independent behaviors:

```yaml
- name: configured latency thresholds require sufficient per-format traffic
  interval: 1m
  input_series:
    - series: 'newapi_relay_duration_seconds_bucket{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false",result="success",le="5"}'
      values: '0+10x20'
    - series: 'newapi_relay_duration_seconds_bucket{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false",result="success",le="10"}'
      values: '0+100x20'
    - series: 'newapi_relay_duration_seconds_bucket{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false",result="success",le="+Inf"}'
      values: '0+100x20'
    - series: 'newapi_relay_requests_total{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false",result="success",error_type="none"}'
      values: '0+20x20'
    - series: 'newapi_relay_latency_threshold_seconds{cluster="test",job="new-api",relay_format="openai",quantile="p95"}'
      values: '8x21'
  alert_rule_test:
    - eval_time: 16m
      alertname: NewAPIRelayP95LatencyHigh
      exp_alerts:
        - exp_labels:
            cluster: test
            job: new-api
            relay_format: openai
            severity: warning
```

Add equivalent P99 data for `openai_image` with `quantile="p99"`, threshold `9.5`, at least 100 requests in 5 minutes, and expected critical. Add separate cases proving no alert when the threshold series is absent, request volume is below the floor, or the threshold belongs to another cluster/job/format.

- [ ] **Step 3: Run rule tests to verify RED**

Run:

```bash
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/recording-rules.test.yml
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/alert-rules.test.yml
```

Expected: FAIL because the new recording rule and alert names do not exist.

- [ ] **Step 4: Implement the recording rule and alerts**

Add to `newapi-traffic`:

```yaml
- record: newapi:relay_request_increase_by_format:5m
  expr: sum by (cluster, job, relay_format) (increase(newapi_relay_requests_total[5m]))
```

Add to `newapi-service-alerts`:

```yaml
- alert: NewAPIRelayP95LatencyHigh
  expr: (newapi:relay_duration_seconds:p95_5m > on (cluster, job, relay_format) newapi_relay_latency_threshold_seconds{quantile="p95"}) and on (cluster, job, relay_format) (newapi:relay_request_increase_by_format:5m >= 50)
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: new-api Relay P95 latency exceeds the configured threshold
    description: Relay format {{ $labels.relay_format }} P95 latency is above its calibrated threshold in cluster {{ $labels.cluster }} job {{ $labels.job }}. Check upstream latency, retries, and format-specific load.

- alert: NewAPIRelayP99LatencyHigh
  expr: (newapi:relay_duration_seconds:p99_5m > on (cluster, job, relay_format) newapi_relay_latency_threshold_seconds{quantile="p99"}) and on (cluster, job, relay_format) (newapi:relay_request_increase_by_format:5m >= 100)
  for: 10m
  labels:
    severity: critical
  annotations:
    summary: new-api Relay P99 latency exceeds the configured threshold
    description: Relay format {{ $labels.relay_format }} P99 latency is above its calibrated threshold in cluster {{ $labels.cluster }} job {{ $labels.job }}. Investigate upstream saturation, network latency, and long-running requests.
```

Update exact base counts in `validate.sh` from 33/24 to 34/26. The default-empty threshold file does not add to the base count.

- [ ] **Step 5: Run rule tests and static validation to verify GREEN**

Run:

```bash
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/recording-rules.test.yml
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/alert-rules.test.yml
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: all pass with 34 base Recording Rules and 26 alerts; latency alerts remain absent when no threshold series exists.

- [ ] **Step 6: Commit Task 2**

```bash
git add deploy/monitoring/recording-rules.yml deploy/monitoring/recording-rules.test.yml deploy/monitoring/alert-rules.yml deploy/monitoring/alert-rules.test.yml deploy/monitoring/validate.sh
git commit -m "监控：增加Relay延迟告警规则"
```

---

### Task 3: Alertmanager inhibition and Grafana threshold overlays

**Files:**
- Modify: `deploy/monitoring/alertmanager.yml.example`
- Modify: `deploy/monitoring/grafana/dashboards/system-overview.json`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Consumes: `NewAPIRelayP95LatencyHigh` and `NewAPIRelayP99LatencyHigh`
- Consumes: `newapi_relay_latency_threshold_seconds`
- Produces: P99→P95 inhibition for the same `cluster/job/relay_format`
- Produces: P95/P99 threshold lines in panel ID 6

- [ ] **Step 1: Tighten validation before changing Alertmanager or Grafana**

Require `relay_format` in Alertmanager `group_by`, require an inhibition rule with source `NewAPIRelayP99LatencyHigh`, target `NewAPIRelayP95LatencyHigh`, and equal labels `cluster/job/relay_format`, and update the dashboard PromQL count from 70 to 72.

Use an always-run Ruby contract assertion:

```ruby
config = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
abort "relay_format missing from Alertmanager group_by" unless config.fetch("route").fetch("group_by").include?("relay_format")
matched = config.fetch("inhibit_rules").any? do |rule|
  rule.fetch("source_matchers").include?('alertname="NewAPIRelayP99LatencyHigh"') &&
    rule.fetch("target_matchers").include?('alertname="NewAPIRelayP95LatencyHigh"') &&
    rule.fetch("equal").sort == %w[cluster job relay_format].sort
end
abort "missing Relay latency inhibition" unless matched
```

- [ ] **Step 2: Run validation to verify RED**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: FAIL because `relay_format` grouping, the inhibition rule, and two dashboard queries are not present.

- [ ] **Step 3: Add Alertmanager grouping and inhibition**

Add `relay_format` to `route.group_by`, then append:

```yaml
- source_matchers:
    - alertname="NewAPIRelayP99LatencyHigh"
    - severity="critical"
  target_matchers:
    - alertname="NewAPIRelayP95LatencyHigh"
    - severity="warning"
  equal:
    - cluster
    - job
    - relay_format
```

- [ ] **Step 4: Add P95/P99 threshold targets to Grafana panel 6**

Update the panel description to state that thresholds are format-specific, absent by default, and use 50/100-request floors. Append:

```json
{
  "datasource": { "type": "prometheus", "uid": "prometheus" },
  "editorMode": "code",
  "expr": "max by (relay_format) (newapi_relay_latency_threshold_seconds{cluster=\"$cluster\",job=\"new-api\",relay_format=~\"$relay_format\",quantile=\"p95\"})",
  "legendFormat": "{{relay_format}} P95 threshold",
  "range": true,
  "refId": "D"
},
{
  "datasource": { "type": "prometheus", "uid": "prometheus" },
  "editorMode": "code",
  "expr": "max by (relay_format) (newapi_relay_latency_threshold_seconds{cluster=\"$cluster\",job=\"new-api\",relay_format=~\"$relay_format\",quantile=\"p99\"})",
  "legendFormat": "{{relay_format}} P99 threshold",
  "range": true,
  "refId": "E"
}
```

Add field overrides matching `/threshold/` so threshold series use dashed lines and no fill while preserving accessible text legends.

- [ ] **Step 5: Run validation to verify GREEN**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: PASS with the Alertmanager inhibition contract and exactly 72 dashboard PromQL expressions.

- [ ] **Step 6: Commit Task 3**

```bash
git add deploy/monitoring/alertmanager.yml.example deploy/monitoring/grafana/dashboards/system-overview.json deploy/monitoring/validate.sh
git commit -m "监控：展示并抑制Relay延迟告警"
```

---

### Task 4: Deployment documentation, TODO evidence, and full verification

**Files:**
- Modify: `docs/prometheus-monitoring.md`
- Modify: `docs/prometheus-monitoring-todolist.md`
- Verify: all monitoring artifacts and the complete Go workspace

**Interfaces:**
- Consumes: completed threshold file, rules, alerts, inhibition, and dashboard overlay
- Produces: operator instructions and checked TODO evidence without falsely marking production calibration complete

- [ ] **Step 1: Document threshold configuration and safe activation**

Add to `docs/prometheus-monitoring.md`:

- The default file is empty and latency alerts are dormant.
- A complete P95/P99 example for one `cluster/job/relay_format`.
- Allowed relay formats and quantiles.
- P95/P99 minimum request counts and `for: 10m`.
- Validation command before reload.
- Reload command:

```bash
curl -fsS -X POST http://localhost:9090/-/reload
```

- PromQL to inspect observed percentile, threshold, and request volume together.
- Troubleshooting for absent thresholds, wrong cluster/job/format labels, insufficient traffic, and duplicate rules.
- A warning that threshold changes are operational configuration, not automatic routing or channel disabling.

- [ ] **Step 2: Update the monitoring TODO with a D9 batch**

Mark as completed only after evidence exists:

- Default-empty threshold file and strict contract validation.
- Per-format request-volume Recording Rule.
- P95 warning and P99 critical with sample floors.
- Threshold-absent, low-volume, and label-isolation tests.
- Alertmanager P99→P95 inhibition.
- Grafana threshold overlays and No data semantics.
- Static counts: 34 base Recording Rules, 26 alerts, 72 dashboard PromQL.
- Full validation commands.

Keep this production item unchecked and rewrite it so the distinction is explicit:

```markdown
- [ ] Production P95/P99 thresholds have been calibrated from real per-format baselines and entered in `relay-latency-thresholds.yml`; the checked-in file intentionally remains empty.
```

- [ ] **Step 3: Run fresh monitoring verification**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: Prometheus config valid, 34 base Recording Rules, 26 alerts, zero checked-in threshold rules, both rule test suites pass, Alertmanager contract passes, and 72 dashboard queries pass promtool parsing.

- [ ] **Step 4: Run the full Go regression suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS; configuration-only monitoring changes do not alter application packages.

- [ ] **Step 5: Run final artifact and whitespace checks**

Run:

```bash
git diff --check
if rg -n '[[:blank:]]+$' \
  deploy/monitoring/relay-latency-thresholds.yml \
  deploy/monitoring/recording-rules.yml \
  deploy/monitoring/recording-rules.test.yml \
  deploy/monitoring/alert-rules.yml \
  deploy/monitoring/alert-rules.test.yml \
  deploy/monitoring/alertmanager.yml.example \
  deploy/monitoring/grafana/dashboards/system-overview.json \
  deploy/monitoring/prometheus.yml \
  deploy/monitoring/validate.sh \
  docker-compose.monitoring.yml \
  docs/prometheus-monitoring.md \
  docs/prometheus-monitoring-todolist.md; then
  exit 1
fi
```

Expected: no whitespace errors. Review `git status --short` and confirm only planned monitoring files plus pre-existing user changes are present.

- [ ] **Step 6: Review requirements against the approved design**

Confirm default dormancy, per-format thresholds, explicit cluster/job boundaries, positive constant validation, P95/P99 traffic floors, inhibition, dashboard threshold visibility, no high-cardinality labels, no application behavior change, and production calibration still unchecked.

- [ ] **Step 7: Commit Task 4**

```bash
git add docs/prometheus-monitoring.md docs/prometheus-monitoring-todolist.md
git commit -m "文档：补充Relay延迟告警部署说明"
```
