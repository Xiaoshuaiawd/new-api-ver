# Prometheus Relay Concurrency Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable, default-dormant Relay inflight warning/critical alerts per cluster, job, and relay format without changing application request behavior.

**Architecture:** Keep application metrics unchanged and aggregate the existing `newapi_relay_inflight` Gauge with one Recording Rule. A dedicated default-empty Prometheus threshold file emits operator-calibrated warning/critical thresholds, alert rules join observed inflight to those thresholds, Alertmanager suppresses matching warnings when critical is active, and Grafana shows actual inflight with textual dashed threshold lines.

**Tech Stack:** Prometheus 3.5 rule files and PromQL, promtool rule tests, Alertmanager 0.28 inhibition rules, Grafana 12 dashboard JSON, Docker Compose, POSIX shell, Ruby YAML contract validation.

## Global Constraints

- Do not modify Relay, channel selection, retries, billing, refunds, routing, queues, concurrency rejection, or client response behavior.
- Reuse `newapi_relay_inflight`; do not add application metrics or Go recording points.
- Threshold labels are exactly `cluster,job,relay_format,severity`; never add instance, channel ID, model, user, Token, IP, Request ID, or error text.
- `severity` is limited to `warning|critical`; `relay_format` is limited to `openai|claude|gemini|openai_responses|openai_responses_compaction|openai_alpha_search|openai_audio|openai_image|openai_realtime|rerank|embedding|task|mj_proxy|other`.
- Every threshold is a positive integer request count expressed as `vector(<integer>)`.
- Warning and critical may be configured independently; when both exist for the same cluster/job/format, critical must be greater than warning.
- The checked-in threshold file is empty, so concurrency alerts are dormant by default.
- Warning remains true for 10 minutes; critical remains true for 5 minutes.
- Gauge aggregation is a scrape-time trend, not an exact simultaneous snapshot.
- Production threshold calibration stays unchecked in the TODO until real production evidence exists.
- Each behavior change follows RED→GREEN and updates `docs/prometheus-monitoring-todolist.md` only after reproducible verification.

---

### Task 1: Default-empty concurrency threshold rule file and strict validation

**Files:**
- Create: `deploy/monitoring/relay-concurrency-thresholds.yml`
- Modify: `deploy/monitoring/prometheus.yml`
- Modify: `docker-compose.monitoring.yml`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Produces: optional metric `newapi_relay_inflight_threshold{cluster,job,relay_format,severity}`
- Produces: a checked-in zero-threshold rule file that passes `promtool check rules`
- Produces: validation that rejects malformed, duplicate, unknown-format, non-integer, non-positive, or incorrectly ordered thresholds

- [ ] **Step 1: Extend the validation contract before creating the threshold file**

Add the file to `required_files`, Prometheus path replacement, and `promtool check rules`:

```sh
$monitoring_dir/relay-concurrency-thresholds.yml
```

```sh
-e "s#/etc/prometheus/rules/relay-concurrency-thresholds.yml#$monitoring_dir/relay-concurrency-thresholds.yml#" \
```

```sh
"$promtool_bin" check rules --lint=all --lint-fatal \
  "$monitoring_dir/recording-rules.yml" \
  "$monitoring_dir/alert-rules.yml" \
  "$monitoring_dir/relay-latency-thresholds.yml" \
  "$monitoring_dir/relay-concurrency-thresholds.yml"
```

Add an always-run Ruby validator with these exact rules:

```ruby
allowed_formats = %w[
  openai claude gemini openai_responses openai_responses_compaction
  openai_alpha_search openai_audio openai_image openai_realtime rerank
  embedding task mj_proxy other
].freeze
allowed_severities = %w[warning critical].freeze
expected_keys = %w[cluster job relay_format severity].freeze

validate = lambda do |document|
  rules = document.fetch("groups").flat_map { |group| group.fetch("rules") }
  seen = {}
  thresholds = Hash.new { |hash, key| hash[key] = {} }

  rules.each do |rule|
    raise ArgumentError, "concurrency threshold record name is invalid" unless rule.fetch("record") == "newapi_relay_inflight_threshold"
    labels = rule.fetch("labels")
    raise ArgumentError, "concurrency threshold labels are invalid" unless labels.keys.sort == expected_keys.sort
    if labels.fetch("cluster").to_s.empty? || labels.fetch("job").to_s.empty?
      raise ArgumentError, "concurrency threshold cluster/job must be non-empty"
    end
    raise ArgumentError, "concurrency threshold relay_format is invalid" unless allowed_formats.include?(labels.fetch("relay_format"))
    raise ArgumentError, "concurrency threshold severity is invalid" unless allowed_severities.include?(labels.fetch("severity"))

    match = /\Avector\(([1-9][0-9]*)\)\z/.match(rule.fetch("expr").to_s.strip)
    raise ArgumentError, "concurrency threshold must use vector(<positive integer>)" unless match
    value = Integer(match[1], 10)

    key = expected_keys.map { |name| labels.fetch(name) }
    raise ArgumentError, "duplicate concurrency threshold #{key.join("/")}" if seen[key]
    seen[key] = true
    format_key = %w[cluster job relay_format].map { |name| labels.fetch(name) }
    thresholds[format_key][labels.fetch("severity")] = value
  end

  thresholds.each do |key, values|
    next unless values.key?("warning") && values.key?("critical")
    unless values.fetch("critical") > values.fetch("warning")
      raise ArgumentError, "critical concurrency threshold must exceed warning for #{key.join("/")}"
    end
  end
end
```

Use temporary YAML fixtures generated inside the Ruby process to assert rejection of unknown format, unknown severity, `vector(0)`, `vector(-1)`, `vector(1.5)`, `vector(NaN)`, `scalar(1)`, an extra label, a duplicate key, equal warning/critical, and critical below warning. Also assert warning-only and critical-only fixtures pass.

- [ ] **Step 2: Run validation to verify RED**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: FAIL with `missing required monitoring artifact: .../relay-concurrency-thresholds.yml`.

- [ ] **Step 3: Create the default-empty file and wire it into deployment**

Create:

```yaml
groups:
  - name: newapi-relay-concurrency-thresholds
    rules: []
```

Append to `deploy/monitoring/prometheus.yml` `rule_files`:

```yaml
- /etc/prometheus/rules/relay-concurrency-thresholds.yml
```

Append to the Prometheus volumes in `docker-compose.monitoring.yml`:

```yaml
- ./deploy/monitoring/relay-concurrency-thresholds.yml:/etc/prometheus/rules/relay-concurrency-thresholds.yml:ro
```

Add a static mount assertion:

```sh
rg -q 'relay-concurrency-thresholds.yml:/etc/prometheus/rules/relay-concurrency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
```

- [ ] **Step 4: Run validation to verify GREEN**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: PASS; Prometheus finds four rule files, the concurrency threshold file reports `0 rules found`, and existing base counts remain 34 Recording Rules, 26 alerts, and 72 dashboard queries.

- [ ] **Step 5: Commit Task 1**

```bash
git add deploy/monitoring/relay-concurrency-thresholds.yml deploy/monitoring/prometheus.yml docker-compose.monitoring.yml deploy/monitoring/validate.sh
git commit -m "监控：增加Relay并发阈值配置"
```

---

### Task 2: Per-format inflight aggregation and warning/critical alerts

**Files:**
- Modify: `deploy/monitoring/recording-rules.test.yml`
- Modify: `deploy/monitoring/recording-rules.yml`
- Modify: `deploy/monitoring/alert-rules.test.yml`
- Modify: `deploy/monitoring/alert-rules.yml`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Consumes: `newapi_relay_inflight{cluster,job,instance,relay_format,stream}`
- Consumes: optional `newapi_relay_inflight_threshold`
- Produces: `newapi:relay_inflight_by_format{cluster,job,relay_format}`
- Produces: `NewAPIRelayInflightHigh` warning and `NewAPIRelayInflightCritical` critical

- [ ] **Step 1: Write the failing aggregation test**

Append to `recording-rules.test.yml`:

```yaml
- name: relay inflight aggregation preserves relay format
  interval: 1m
  input_series:
    - series: 'newapi_relay_inflight{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false"}'
      values: '20x3'
    - series: 'newapi_relay_inflight{cluster="test",job="new-api",instance="app-2",relay_format="openai",stream="true"}'
      values: '15x3'
    - series: 'newapi_relay_inflight{cluster="test",job="new-api",instance="app-1",relay_format="openai_image",stream="false"}'
      values: '5x3'
  promql_expr_test:
    - expr: newapi:relay_inflight_by_format
      eval_time: 1m
      exp_samples:
        - labels: '{__name__="newapi:relay_inflight_by_format",cluster="test",job="new-api",relay_format="openai"}'
          value: 35
        - labels: '{__name__="newapi:relay_inflight_by_format",cluster="test",job="new-api",relay_format="openai_image"}'
          value: 5
```

- [ ] **Step 2: Write failing alert tests**

Append fixed-input tests to `alert-rules.test.yml`:

```yaml
- name: configured relay inflight warning and critical trigger after their durations
  interval: 1m
  input_series:
    - series: 'newapi_relay_inflight{cluster="test",job="new-api",instance="app-1",relay_format="openai",stream="false"}'
      values: '20x16'
    - series: 'newapi_relay_inflight{cluster="test",job="new-api",instance="app-2",relay_format="openai",stream="true"}'
      values: '15x16'
    - series: 'newapi_relay_inflight_threshold{cluster="test",job="new-api",relay_format="openai",severity="warning"}'
      values: '30x16'
    - series: 'newapi_relay_inflight_threshold{cluster="test",job="new-api",relay_format="openai",severity="critical"}'
      values: '32x16'
  alert_rule_test:
    - eval_time: 6m
      alertname: NewAPIRelayInflightCritical
      exp_alerts:
        - exp_labels:
            cluster: test
            job: new-api
            relay_format: openai
            severity: critical
          exp_annotations:
            summary: new-api Relay inflight exceeds the calibrated critical threshold
            description: Relay format openai inflight is above its calibrated critical threshold in cluster test job new-api. Check long-lived streams, upstream latency, request accumulation, instance distribution, and scaling needs; this threshold is not an application hard limit.
    - eval_time: 11m
      alertname: NewAPIRelayInflightHigh
      exp_alerts:
        - exp_labels:
            cluster: test
            job: new-api
            relay_format: openai
            severity: warning
          exp_annotations:
            summary: new-api Relay inflight exceeds the calibrated warning threshold
            description: Relay format openai inflight is above its calibrated warning threshold in cluster test job new-api. Check long-lived streams, upstream latency, request accumulation, instance distribution, and scaling needs; this threshold is not an application hard limit.
```

Add independent cases that expect no alerts when inflight equals the threshold, threshold series are absent, or thresholds belong to another cluster/job/format. Add a transient case with inflight above warning for only five minutes and below it afterward; evaluate at 11 minutes and expect no warning.

- [ ] **Step 3: Run rule tests to verify RED**

Run:

```bash
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/recording-rules.test.yml
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/alert-rules.test.yml
```

Expected: FAIL because the new Recording Rule and alert names do not exist.

- [ ] **Step 4: Implement the minimal rules**

Add to `newapi-traffic`:

```yaml
- record: newapi:relay_inflight_by_format
  expr: sum by (cluster, job, relay_format) (newapi_relay_inflight)
```

Add to `newapi-service-alerts`:

```yaml
- alert: NewAPIRelayInflightHigh
  expr: newapi:relay_inflight_by_format > on (cluster, job, relay_format) newapi_relay_inflight_threshold{severity="warning"}
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: new-api Relay inflight exceeds the calibrated warning threshold
    description: Relay format {{ $labels.relay_format }} inflight is above its calibrated warning threshold in cluster {{ $labels.cluster }} job {{ $labels.job }}. Check long-lived streams, upstream latency, request accumulation, instance distribution, and scaling needs; this threshold is not an application hard limit.

- alert: NewAPIRelayInflightCritical
  expr: newapi:relay_inflight_by_format > on (cluster, job, relay_format) newapi_relay_inflight_threshold{severity="critical"}
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: new-api Relay inflight exceeds the calibrated critical threshold
    description: Relay format {{ $labels.relay_format }} inflight is above its calibrated critical threshold in cluster {{ $labels.cluster }} job {{ $labels.job }}. Check long-lived streams, upstream latency, request accumulation, instance distribution, and scaling needs; this threshold is not an application hard limit.
```

Update exact base counts in `validate.sh` from 34/26 to 35/28. The default-empty threshold files do not add to base counts.

- [ ] **Step 5: Run tests and static validation to verify GREEN**

Run:

```bash
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/recording-rules.test.yml
/tmp/newapi-promtool-bin/promtool test rules deploy/monitoring/alert-rules.test.yml
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: PASS with 35 base Recording Rules, 28 alerts, and zero checked-in concurrency thresholds.

- [ ] **Step 6: Commit Task 2**

```bash
git add deploy/monitoring/recording-rules.yml deploy/monitoring/recording-rules.test.yml deploy/monitoring/alert-rules.yml deploy/monitoring/alert-rules.test.yml deploy/monitoring/validate.sh
git commit -m "监控：增加Relay并发告警规则"
```

---

### Task 3: Alertmanager inhibition and Grafana concurrency panel

**Files:**
- Modify: `deploy/monitoring/alertmanager.yml.example`
- Modify: `deploy/monitoring/grafana/dashboards/system-overview.json`
- Modify: `deploy/monitoring/validate.sh`

**Interfaces:**
- Consumes: `NewAPIRelayInflightHigh` and `NewAPIRelayInflightCritical`
- Consumes: `newapi:relay_inflight_by_format` and `newapi_relay_inflight_threshold`
- Produces: critical→warning inhibition for the same `cluster/job/relay_format`
- Produces: System Overview panel ID 26 with actual, warning, and critical series

- [ ] **Step 1: Tighten static validation before changing configuration**

Extend the existing Alertmanager Ruby assertion with a second contract:

```ruby
matched = config.fetch("inhibit_rules").any? do |rule|
  rule.fetch("source_matchers").include?(%q{alertname="NewAPIRelayInflightCritical"}) &&
    rule.fetch("target_matchers").include?(%q{alertname="NewAPIRelayInflightHigh"}) &&
    rule.fetch("equal").sort == %w[cluster job relay_format].sort
end
abort "missing Relay inflight inhibition" unless matched
```

Require panel ID 26 to contain exactly one actual query, one warning threshold query, one critical threshold query, no `$instance`, distinct text legends, and separate regex overrides for `/warning threshold/` and `/critical threshold/`. Update expected dashboard PromQL count from 72 to 75 and System Overview minimum panel count from 12 to 13.

- [ ] **Step 2: Run validation to verify RED**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: FAIL because the inhibition rule and panel ID 26 do not exist.

- [ ] **Step 3: Add Alertmanager inhibition**

Append:

```yaml
- source_matchers:
    - alertname="NewAPIRelayInflightCritical"
    - severity="critical"
  target_matchers:
    - alertname="NewAPIRelayInflightHigh"
    - severity="warning"
  equal:
    - cluster
    - job
    - relay_format
```

- [ ] **Step 4: Add Grafana panel ID 26**

Append a full-width panel at `gridPos: { "h": 7, "w": 24, "x": 0, "y": 75 }` with title `Relay inflight by format`, unit `short`, step-after lines, visible table legend, and three targets:

```json
{
  "expr": "newapi:relay_inflight_by_format{cluster=\"$cluster\",job=\"new-api\",relay_format=~\"$relay_format\"}",
  "legendFormat": "{{relay_format}} actual inflight",
  "refId": "A"
},
{
  "expr": "max by (relay_format) (newapi_relay_inflight_threshold{cluster=\"$cluster\",job=\"new-api\",relay_format=~\"$relay_format\",severity=\"warning\"})",
  "legendFormat": "{{relay_format}} warning threshold",
  "refId": "B"
},
{
  "expr": "max by (relay_format) (newapi_relay_inflight_threshold{cluster=\"$cluster\",job=\"new-api\",relay_format=~\"$relay_format\",severity=\"critical\"})",
  "legendFormat": "{{relay_format}} critical threshold",
  "refId": "C"
}
```

Use dashed/no-fill overrides for both threshold legend regexes, with different dash arrays so warning and critical remain distinguishable without color. The description must state that thresholds are calibrated, absent by default, ignore instance filtering, and use 10-minute warning/5-minute critical durations; multi-instance Gauge sums are scrape-time trends.

- [ ] **Step 5: Run validation to verify GREEN**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: PASS with the inhibition contract, 13+ System Overview panels, and exactly 75 dashboard PromQL expressions.

- [ ] **Step 6: Commit Task 3**

```bash
git add deploy/monitoring/alertmanager.yml.example deploy/monitoring/grafana/dashboards/system-overview.json deploy/monitoring/validate.sh
git commit -m "监控：展示并抑制Relay并发告警"
```

---

### Task 4: Deployment documentation, D10 TODO evidence, and full verification

**Files:**
- Modify: `docs/prometheus-monitoring.md`
- Modify: `docs/prometheus-monitoring-todolist.md`
- Verify: all monitoring artifacts and the complete Go workspace

**Interfaces:**
- Consumes: completed threshold file, aggregation, alerts, inhibition, and panel
- Produces: operator instructions and D10 evidence without falsely marking production calibration complete

- [ ] **Step 1: Document concurrency threshold operation**

Add to `docs/prometheus-monitoring.md`:

- The default file is empty and concurrency alerts are dormant.
- A complete warning/critical example for one `cluster/job/relay_format`.
- Allowed formats, severities, positive-integer syntax, uniqueness, and critical-greater-than-warning rule.
- Warning `for: 10m` and critical `for: 5m`.
- Validation and reload commands:

```bash
PROMTOOL_BIN=/path/to/promtool deploy/monitoring/validate.sh
curl -fsS -X POST http://localhost:9090/-/reload
```

- PromQL for actual inflight and thresholds together.
- Troubleshooting for absent thresholds, wrong labels, equal-to-threshold values, short spikes, and invalid warning/critical ordering.
- A warning that threshold changes do not add a hard limit, reject requests, modify routing, or disable channels.

- [ ] **Step 2: Update the monitoring TODO with D10**

Mark as completed only after evidence exists:

- Default-empty concurrency threshold file and strict contract validation.
- `newapi:relay_inflight_by_format` aggregation.
- Warning/critical alerts and their durations.
- Missing-threshold, equality, label-isolation, and transient-spike tests.
- Alertmanager critical→warning inhibition.
- Grafana actual/threshold panel and No data semantics.
- Static counts: 35 base Recording Rules, 28 alerts, 75 dashboard PromQL, zero checked-in concurrency thresholds.
- Full validation commands and confirmation that application behavior was untouched.

Keep this item unchecked:

```markdown
- [ ] Production Relay concurrency thresholds have been calibrated from real per-format baselines and entered in `relay-concurrency-thresholds.yml`; the checked-in file intentionally remains empty.
```

Replace the old generic unchecked concurrency-alert item with a checked infrastructure statement plus the separate unchecked production-calibration item.

- [ ] **Step 3: Run fresh monitoring verification**

Run:

```bash
PROMTOOL_BIN=/tmp/newapi-promtool-bin/promtool deploy/monitoring/validate.sh
```

Expected: Prometheus config valid, 35 base Recording Rules, 28 alerts, zero checked-in latency and concurrency threshold rules, both rule test suites pass, Alertmanager contracts pass, and 75 dashboard queries pass promtool parsing.

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
  deploy/monitoring/relay-concurrency-thresholds.yml \
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

Expected: no whitespace errors. Review `git status --short` and confirm only planned D10 files plus pre-existing user changes are present.

- [ ] **Step 6: Review requirements against the approved design**

Confirm default dormancy, per-format thresholds, explicit cluster/job boundaries, positive integer validation, warning/critical ordering, sustained durations, inhibition, accessible Dashboard legends/styles, no high-cardinality labels, no application behavior change, and production calibration still unchecked.

- [ ] **Step 7: Commit Task 4**

```bash
git add docs/prometheus-monitoring.md docs/prometheus-monitoring-todolist.md
git commit -m "文档：补充Relay并发告警部署说明"
```
