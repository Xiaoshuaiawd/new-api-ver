#!/bin/sh

set -eu

monitoring_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$monitoring_dir/../.." && pwd)
promtool_bin=${PROMTOOL_BIN:-promtool}
amtool_bin=${AMTOOL_BIN:-amtool}

required_files="
$repo_dir/docker-compose.monitoring.yml
$monitoring_dir/prometheus.yml
$monitoring_dir/recording-rules.yml
$monitoring_dir/recording-rules.test.yml
$monitoring_dir/alert-rules.yml
$monitoring_dir/alert-rules.test.yml
$monitoring_dir/relay-latency-thresholds.yml
$monitoring_dir/relay-concurrency-thresholds.yml
$monitoring_dir/channel-latency-thresholds.yml
$monitoring_dir/channel-concurrency-thresholds.yml
$monitoring_dir/alertmanager.yml.example
$monitoring_dir/feishu-webhook/Dockerfile
$monitoring_dir/targets/postgres-exporter.yml
$monitoring_dir/targets/mysql-exporter.yml
$monitoring_dir/grafana/provisioning/datasources/prometheus.yml
$monitoring_dir/grafana/provisioning/dashboards/default.yml
$monitoring_dir/grafana/dashboards/core/host-overview.json
$monitoring_dir/grafana/dashboards/core/application-overview.json
$monitoring_dir/grafana/dashboards/core/middleware-overview.json
$monitoring_dir/grafana/dashboards/core/channel-overview.json
$monitoring_dir/grafana/dashboards/extended/billing-overview.json
$monitoring_dir/grafana/dashboards/extended/task-overview.json
$monitoring_dir/secrets/.gitignore
$monitoring_dir/secrets/feishu-webhook-url.example
$monitoring_dir/secrets/postgres-exporter-password.example
$monitoring_dir/secrets/redis-exporter-password.example
$monitoring_dir/secrets/mysql-exporter.cnf.example
$repo_dir/docs/prometheus-monitoring.md
"

for required_file in $required_files; do
	if [ ! -s "$required_file" ]; then
		echo "missing required monitoring artifact: $required_file" >&2
		exit 1
	fi
done

ruby -e '
  require "json"

  passwords = JSON.parse(File.read(ARGV.fetch(0)))
  valid = passwords.is_a?(Hash) && !passwords.empty? && passwords.all? do |address, password|
    (address.start_with?("redis://") || address.start_with?("rediss://")) && password.is_a?(String)
  end
  abort "Redis exporter password file example must be a non-empty redis URI to password JSON object" unless valid
' "$monitoring_dir/secrets/redis-exporter-password.example"

ruby -e '
  require "uri"

  value = File.read(ARGV.fetch(0)).strip
  uri = URI.parse(value)
  valid = uri.scheme == "https" &&
    uri.host == "open.feishu.cn" &&
    uri.port == 443 &&
    uri.userinfo.nil? &&
    uri.query.nil? &&
    uri.fragment.nil? &&
    uri.path.match?(%r{\A/open-apis/bot/v2/hook/[^/]+\z})
  abort "Feishu webhook example must be a complete official custom-bot URL" unless valid
' "$monitoring_dir/secrets/feishu-webhook-url.example"

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
	PROMETHEUS_BEARER_TOKEN_FILE="$monitoring_dir/secrets/new-api-bearer-token.example" \
	GRAFANA_ADMIN_PASSWORD_FILE="$monitoring_dir/secrets/grafana-admin-password.example" \
	FEISHU_WEBHOOK_URL_FILE="$monitoring_dir/secrets/feishu-webhook-url.example" \
	POSTGRES_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/postgres-exporter-password.example" \
	REDIS_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/redis-exporter-password.example" \
	MYSQL_EXPORTER_CONFIG_FILE="$monitoring_dir/secrets/mysql-exporter.cnf.example" \
	POSTGRES_EXPORTER_URI='postgres:5432/new-api?sslmode=disable' \
	POSTGRES_EXPORTER_USER=example \
	REDIS_EXPORTER_ADDR=redis://redis:6379 \
	NEW_API_DOCKER_NETWORK=example \
		docker compose -f "$repo_dir/docker-compose.monitoring.yml" --profile postgres config --quiet

	PROMETHEUS_BEARER_TOKEN_FILE="$monitoring_dir/secrets/new-api-bearer-token.example" \
	GRAFANA_ADMIN_PASSWORD_FILE="$monitoring_dir/secrets/grafana-admin-password.example" \
	FEISHU_WEBHOOK_URL_FILE="$monitoring_dir/secrets/feishu-webhook-url.example" \
	POSTGRES_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/postgres-exporter-password.example" \
	REDIS_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/redis-exporter-password.example" \
	MYSQL_EXPORTER_CONFIG_FILE="$monitoring_dir/secrets/mysql-exporter.cnf.example" \
	POSTGRES_EXPORTER_URI='postgres:5432/new-api?sslmode=disable' \
	POSTGRES_EXPORTER_USER=example \
	REDIS_EXPORTER_ADDR=redis://redis:6379 \
	NEW_API_DOCKER_NETWORK=example \
		docker compose -f "$repo_dir/docker-compose.monitoring.yml" --profile mysql config --quiet
else
	echo "warning: docker compose unavailable; skipped monitoring Compose validation" >&2
fi

if ! command -v "$promtool_bin" >/dev/null 2>&1 && [ ! -x "$promtool_bin" ]; then
	echo "promtool is required; set PROMTOOL_BIN to its executable path" >&2
	exit 1
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

sed \
	-e "s#/etc/prometheus/rules/recording-rules.yml#$monitoring_dir/recording-rules.yml#" \
	-e "s#/etc/prometheus/rules/alert-rules.yml#$monitoring_dir/alert-rules.yml#" \
	-e "s#/etc/prometheus/rules/relay-latency-thresholds.yml#$monitoring_dir/relay-latency-thresholds.yml#" \
	-e "s#/etc/prometheus/rules/relay-concurrency-thresholds.yml#$monitoring_dir/relay-concurrency-thresholds.yml#" \
	-e "s#/etc/prometheus/rules/channel-latency-thresholds.yml#$monitoring_dir/channel-latency-thresholds.yml#" \
	-e "s#/etc/prometheus/rules/channel-concurrency-thresholds.yml#$monitoring_dir/channel-concurrency-thresholds.yml#" \
	-e "s#/etc/prometheus/secrets/new-api-bearer-token#$monitoring_dir/secrets/new-api-bearer-token.example#" \
	-e "s#/etc/prometheus/targets/postgres-exporter.yml#$monitoring_dir/targets/postgres-exporter.yml#" \
	-e "s#/etc/prometheus/targets/mysql-exporter.yml#$monitoring_dir/targets/mysql-exporter.yml#" \
	"$monitoring_dir/prometheus.yml" >"$tmp_dir/prometheus.yml"

"$promtool_bin" check config "$tmp_dir/prometheus.yml"
"$promtool_bin" check rules --lint=all --lint-fatal \
	"$monitoring_dir/recording-rules.yml" \
	"$monitoring_dir/alert-rules.yml" \
	"$monitoring_dir/relay-latency-thresholds.yml" \
	"$monitoring_dir/relay-concurrency-thresholds.yml" \
	"$monitoring_dir/channel-latency-thresholds.yml" \
	"$monitoring_dir/channel-concurrency-thresholds.yml"
ruby -e '
  require "tmpdir"
  require "yaml"

  allowed_formats = %w[
    openai claude gemini openai_responses openai_responses_compaction
    openai_alpha_search openai_audio openai_image openai_realtime rerank
    embedding task mj_proxy other
  ].freeze
  allowed_quantiles = %w[p95 p99].freeze
  expected_keys = %w[cluster job quantile relay_format].freeze

  validate = lambda do |document|
    rules = document.fetch("groups").flat_map { |group| group.fetch("rules") }
    seen = {}

    rules.each do |rule|
      raise ArgumentError, "latency threshold record name is invalid" unless rule.fetch("record") == "newapi_relay_latency_threshold_seconds"
      labels = rule.fetch("labels")
      raise ArgumentError, "latency threshold labels are invalid" unless labels.keys.sort == expected_keys.sort
      if labels.fetch("cluster").to_s.empty? || labels.fetch("job").to_s.empty?
        raise ArgumentError, "latency threshold cluster/job must be non-empty"
      end
      raise ArgumentError, "latency threshold relay_format is invalid" unless allowed_formats.include?(labels.fetch("relay_format"))
      raise ArgumentError, "latency threshold quantile is invalid" unless allowed_quantiles.include?(labels.fetch("quantile"))

      match = /\Avector\(([^()]+)\)\z/.match(rule.fetch("expr").to_s.strip)
      raise ArgumentError, "latency threshold must use vector(<positive seconds>)" unless match
      seconds = Float(match[1], exception: false)
      raise ArgumentError, "latency threshold must be finite and positive" unless seconds&.finite? && seconds.positive?

      key = expected_keys.map { |name| labels.fetch(name) }
      raise ArgumentError, "duplicate latency threshold #{key.join("/")}" if seen[key]
      seen[key] = true
    end
  end

  document = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  begin
    validate.call(document)
  rescue KeyError, ArgumentError => error
    abort error.message
  end

  base_rule = {
    "record" => "newapi_relay_latency_threshold_seconds",
    "expr" => "vector(10)",
    "labels" => {
      "cluster" => "default",
      "job" => "new-api",
      "relay_format" => "openai",
      "quantile" => "p95"
    }
  }
  fixture = lambda do |rules|
    { "groups" => [{ "name" => "test", "rules" => rules }] }
  end
  changed_rule = lambda do |path, value|
    rule = Marshal.load(Marshal.dump(base_rule))
    target = path.reduce(rule) { |current, key| current.fetch(key) }
    target.replace(value) if target.is_a?(String)
    rule
  end
  invalid_cases = {
    "unknown-format" => ["latency threshold relay_format is invalid", changed_rule.call(%w[labels relay_format], "unknown")],
    "unknown-quantile" => ["latency threshold quantile is invalid", changed_rule.call(%w[labels quantile], "p90")],
    "zero" => ["latency threshold must be finite and positive", changed_rule.call(%w[expr], "vector(0)")],
    "negative" => ["latency threshold must be finite and positive", changed_rule.call(%w[expr], "vector(-1)")],
    "nan" => ["latency threshold must be finite and positive", changed_rule.call(%w[expr], "vector(NaN)")],
    "non-constant" => ["latency threshold must use vector(<positive seconds>)", changed_rule.call(%w[expr], "scalar(1)")]
  }
  extra_label = Marshal.load(Marshal.dump(base_rule))
  extra_label.fetch("labels")["instance"] = "app-1"
  invalid_cases["extra-label"] = ["latency threshold labels are invalid", extra_label]
  invalid_cases["duplicate"] = ["duplicate latency threshold default/new-api/p95/openai", [base_rule, base_rule]]

  Dir.mktmpdir("newapi-relay-latency-thresholds") do |directory|
    invalid_cases.each do |name, (expected_error, rule_or_rules)|
      rules = rule_or_rules.is_a?(Array) ? rule_or_rules : [rule_or_rules]
      path = File.join(directory, "#{name}.yml")
      File.write(path, YAML.dump(fixture.call(rules)))
      begin
        validate.call(YAML.safe_load(File.read(path), aliases: true))
        abort "invalid latency threshold fixture #{name} was accepted"
      rescue KeyError, ArgumentError => error
        unless error.message == expected_error
          abort "invalid latency threshold fixture #{name} returned #{error.message.inspect}, expected #{expected_error.inspect}"
        end
      end
    end
  end
' "$monitoring_dir/relay-latency-thresholds.yml"
ruby -e '
  require "tmpdir"
  require "yaml"

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

  document = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  begin
    validate.call(document)
  rescue KeyError, ArgumentError => error
    abort error.message
  end

  base_rule = {
    "record" => "newapi_relay_inflight_threshold",
    "expr" => "vector(10)",
    "labels" => {
      "cluster" => "default",
      "job" => "new-api",
      "relay_format" => "openai",
      "severity" => "warning"
    }
  }
  fixture = lambda do |rules|
    { "groups" => [{ "name" => "test", "rules" => rules }] }
  end
  changed_rule = lambda do |path, value|
    rule = Marshal.load(Marshal.dump(base_rule))
    target = path.reduce(rule) { |current, key| current.fetch(key) }
    target.replace(value)
    rule
  end
  invalid_cases = {
    "unknown-format" => ["concurrency threshold relay_format is invalid", changed_rule.call(%w[labels relay_format], "unknown")],
    "unknown-severity" => ["concurrency threshold severity is invalid", changed_rule.call(%w[labels severity], "notice")],
    "zero" => ["concurrency threshold must use vector(<positive integer>)", changed_rule.call(%w[expr], "vector(0)")],
    "negative" => ["concurrency threshold must use vector(<positive integer>)", changed_rule.call(%w[expr], "vector(-1)")],
    "decimal" => ["concurrency threshold must use vector(<positive integer>)", changed_rule.call(%w[expr], "vector(1.5)")],
    "nan" => ["concurrency threshold must use vector(<positive integer>)", changed_rule.call(%w[expr], "vector(NaN)")],
    "non-constant" => ["concurrency threshold must use vector(<positive integer>)", changed_rule.call(%w[expr], "scalar(1)")]
  }
  extra_label = Marshal.load(Marshal.dump(base_rule))
  extra_label.fetch("labels")["instance"] = "app-1"
  invalid_cases["extra-label"] = ["concurrency threshold labels are invalid", extra_label]
  invalid_cases["duplicate"] = ["duplicate concurrency threshold default/new-api/openai/warning", [base_rule, base_rule]]

  warning_rule = Marshal.load(Marshal.dump(base_rule))
  critical_rule = Marshal.load(Marshal.dump(base_rule))
  critical_rule.fetch("labels")["severity"] = "critical"
  invalid_cases["equal"] = [
    "critical concurrency threshold must exceed warning for default/new-api/openai",
    [warning_rule, critical_rule]
  ]
  critical_below_warning = Marshal.load(Marshal.dump(critical_rule))
  critical_below_warning["expr"] = "vector(9)"
  invalid_cases["critical-below-warning"] = [
    "critical concurrency threshold must exceed warning for default/new-api/openai",
    [warning_rule, critical_below_warning]
  ]

  Dir.mktmpdir("newapi-relay-concurrency-thresholds") do |directory|
    invalid_cases.each do |name, (expected_error, rule_or_rules)|
      rules = rule_or_rules.is_a?(Array) ? rule_or_rules : [rule_or_rules]
      path = File.join(directory, "#{name}.yml")
      File.write(path, YAML.dump(fixture.call(rules)))
      begin
        validate.call(YAML.safe_load(File.read(path), aliases: true))
        abort "invalid concurrency threshold fixture #{name} was accepted"
      rescue KeyError, ArgumentError => error
        unless error.message == expected_error
          abort "invalid concurrency threshold fixture #{name} returned #{error.message.inspect}, expected #{expected_error.inspect}"
        end
      end
    end

    warning_only_path = File.join(directory, "warning-only.yml")
    File.write(warning_only_path, YAML.dump(fixture.call([warning_rule])))
    validate.call(YAML.safe_load(File.read(warning_only_path), aliases: true))

    critical_only_path = File.join(directory, "critical-only.yml")
    File.write(critical_only_path, YAML.dump(fixture.call([critical_rule])))
    validate.call(YAML.safe_load(File.read(critical_only_path), aliases: true))
  end
' "$monitoring_dir/relay-concurrency-thresholds.yml"
ruby -e '
  require "yaml"

  latency = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  concurrency = YAML.safe_load(File.read(ARGV.fetch(1)), aliases: true)
  allowed_metrics = %w[duration_p95 ttft_p95 first_byte_p95].freeze
  allowed_severities = %w[warning critical].freeze

  validate_rules = lambda do |document, expected_record, expected_keys, value_pattern|
    seen = {}
    document.fetch("groups").flat_map { |group| group.fetch("rules") }.each do |rule|
      raise ArgumentError, "channel threshold record name is invalid" unless rule.fetch("record") == expected_record
      labels = rule.fetch("labels")
      raise ArgumentError, "channel threshold labels are invalid" unless labels.keys.sort == expected_keys.sort
      raise ArgumentError, "channel threshold cluster/job must be non-empty" if labels.fetch("cluster").to_s.empty? || labels.fetch("job").to_s.empty?
      raise ArgumentError, "channel threshold channel_id is invalid" unless /\A[1-9][0-9]*\z/.match?(labels.fetch("channel_id").to_s)
      raise ArgumentError, "channel threshold severity is invalid" unless allowed_severities.include?(labels.fetch("severity"))
      if labels.key?("metric")
        raise ArgumentError, "channel latency metric is invalid" unless allowed_metrics.include?(labels.fetch("metric"))
      end
      match = value_pattern.match(rule.fetch("expr").to_s.strip)
      raise ArgumentError, "channel threshold must use vector(<positive value>)" unless match
      value = Float(match[1], exception: false)
      raise ArgumentError, "channel threshold must be finite and positive" unless value&.finite? && value.positive?
      key = expected_keys.map { |name| labels.fetch(name) }
      raise ArgumentError, "duplicate channel threshold #{key.join("/")}" if seen[key]
      seen[key] = true
    end
  end

  validate_rules.call(
    latency,
    "newapi_channel_latency_threshold_seconds",
    %w[channel_id cluster job metric severity],
    /\Avector\(([^()]+)\)\z/
  )
  validate_rules.call(
    concurrency,
    "newapi_channel_inflight_threshold",
    %w[channel_id cluster job severity],
    /\Avector\(([1-9][0-9]*)\)\z/
  )

  [latency, concurrency].each do |document|
    thresholds = Hash.new { |hash, key| hash[key] = {} }
    document.fetch("groups").flat_map { |group| group.fetch("rules") }.each do |rule|
      labels = rule.fetch("labels")
      key = labels.reject { |name, _| name == "severity" }.sort
      value = Float(/\Avector\(([^()]+)\)\z/.match(rule.fetch("expr").to_s.strip)[1])
      thresholds[key][labels.fetch("severity")] = value
    end
    thresholds.each do |key, values|
      next unless values.key?("warning") && values.key?("critical")
      unless values.fetch("critical") > values.fetch("warning")
        raise ArgumentError, "critical channel threshold must exceed warning for #{key.inspect}"
      end
    end
  end
' "$monitoring_dir/channel-latency-thresholds.yml" "$monitoring_dir/channel-concurrency-thresholds.yml"
"$promtool_bin" test rules "$monitoring_dir/recording-rules.test.yml"
"$promtool_bin" test rules "$monitoring_dir/alert-rules.test.yml"
ruby -e '
  require "yaml"

  recording = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  alerts = YAML.safe_load(File.read(ARGV.fetch(1)), aliases: true)
  recording_count = recording.fetch("groups").sum { |group| group.fetch("rules").length }
  alert_count = alerts.fetch("groups").sum { |group| group.fetch("rules").length }
  abort "expected 47 recording rules, got #{recording_count}" unless recording_count == 47
  abort "expected 72 alert rules, got #{alert_count}" unless alert_count == 72
' "$monitoring_dir/recording-rules.yml" "$monitoring_dir/alert-rules.yml"
if command -v "$amtool_bin" >/dev/null 2>&1 || [ -x "$amtool_bin" ]; then
	"$amtool_bin" check-config "$monitoring_dir/alertmanager.yml.example"
else
	if ! command -v ruby >/dev/null 2>&1; then
		echo "amtool or ruby is required to validate Alertmanager YAML" >&2
		exit 1
	fi
	ruby -e 'require "yaml"; YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)' \
		"$monitoring_dir/alertmanager.yml.example"
	echo "warning: amtool unavailable; Alertmanager received YAML and contract validation only" >&2
fi

ruby -e '
  require "yaml"

  config = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  unless config.fetch("route").fetch("group_by").include?("relay_format")
    abort "relay_format missing from Alertmanager group_by"
  end
  expected_receiver_names = %w[new-api-webhook-warning new-api-webhook-critical]
  receivers = config.fetch("receivers").select { |receiver| expected_receiver_names.include?(receiver.fetch("name")) }
  abort "missing Feishu Alertmanager receivers" unless receivers.length == expected_receiver_names.length
  receivers.each do |receiver|
    webhooks = receiver.fetch("webhook_configs")
    abort "Feishu receiver must contain exactly one webhook" unless webhooks.length == 1
    webhook = webhooks.first
    unless webhook.fetch("url") == "http://feishu-alert-webhook:8080/api/v1/alerts" &&
        webhook.fetch("send_resolved") == true &&
        webhook.fetch("timeout") == "15s"
      abort "Feishu receiver webhook contract is invalid"
    end
  end
  matched = config.fetch("inhibit_rules").any? do |rule|
    rule.fetch("source_matchers").include?(%q{alertname="NewAPIRelayP99LatencyHigh"}) &&
      rule.fetch("target_matchers").include?(%q{alertname="NewAPIRelayP95LatencyHigh"}) &&
      rule.fetch("equal").sort == %w[cluster job relay_format].sort
  end
  abort "missing Relay latency inhibition" unless matched
  matched = config.fetch("inhibit_rules").any? do |rule|
    rule.fetch("source_matchers").include?(%q{alertname="NewAPIRelayInflightCritical"}) &&
      rule.fetch("target_matchers").include?(%q{alertname="NewAPIRelayInflightHigh"}) &&
      rule.fetch("equal").sort == %w[cluster job relay_format].sort
  end
  abort "missing Relay inflight inhibition" unless matched
  matched = config.fetch("inhibit_rules").any? do |rule|
    rule.fetch("source_matchers").include?(%q{alertname="NewAPIChannelFirstTokenWaitingCritical"}) &&
      rule.fetch("target_matchers").include?(%q{alertname="NewAPIChannelFirstTokenWaitingHigh"}) &&
      rule.fetch("equal").sort == %w[channel_id cluster job].sort
  end
  abort "missing channel first-token inhibition" unless matched
' "$monitoring_dir/alertmanager.yml.example"

rg -q '^  scrape_interval: 15s$' "$monitoring_dir/prometheus.yml"
rg -q '^  scrape_timeout: 10s$' "$monitoring_dir/prometheus.yml"
rg -q '^    cluster: default$' "$monitoring_dir/prometheus.yml"
rg -q '^  - job_name: new-api$' "$monitoring_dir/prometheus.yml"
rg -q '^  - job_name: feishu-alert-webhook$' "$monitoring_dir/prometheus.yml"
rg -q '^          - feishu-alert-webhook:8080$' "$monitoring_dir/prometheus.yml"
rg -q '^      credentials_file: /etc/prometheus/secrets/new-api-bearer-token$' "$monitoring_dir/prometheus.yml"
rg -q 'relay-latency-thresholds.yml:/etc/prometheus/rules/relay-latency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
rg -q 'relay-concurrency-thresholds.yml:/etc/prometheus/rules/relay-concurrency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
rg -q 'channel-latency-thresholds.yml:/etc/prometheus/rules/channel-latency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
rg -q 'channel-concurrency-thresholds.yml:/etc/prometheus/rules/channel-concurrency-thresholds.yml:ro' "$repo_dir/docker-compose.monitoring.yml"
rg -q '^inhibit_rules:$' "$monitoring_dir/alertmanager.yml.example"
rg -q '^        send_resolved: true$' "$monitoring_dir/alertmanager.yml.example"
rg -q '^      - url: http://feishu-alert-webhook:8080/api/v1/alerts$' "$monitoring_dir/alertmanager.yml.example"
if rg -q 'url_file: /run/secrets/alertmanager_webhook_url' "$monitoring_dir/alertmanager.yml.example"; then
	echo "Alertmanager must send to the internal Feishu alert bridge" >&2
	exit 1
fi
rg -q '^!\*\.example$' "$monitoring_dir/secrets/.gitignore"

ruby -e '
  require "yaml"

  config = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
  cluster = config.fetch("global").fetch("external_labels").fetch("cluster")
  jobs = config.fetch("scrape_configs")
  monitoring_dir = ARGV.fetch(1)

  jobs.each do |job|
    job.fetch("static_configs", []).each do |static_config|
      labels = static_config.fetch("labels")
      unless labels.fetch("cluster", nil) == cluster
        abort "scrape target #{job.fetch("job_name")} must set cluster=#{cluster}; external_labels are not added to local query series"
      end
    end
    job.fetch("file_sd_configs", []).each do |file_sd|
      file_sd.fetch("files").each do |configured_path|
        local_path = File.join(monitoring_dir, "targets", File.basename(configured_path))
        YAML.safe_load(File.read(local_path), aliases: true).each do |static_config|
          labels = static_config.fetch("labels")
          unless labels.fetch("cluster", nil) == cluster
            abort "file_sd target #{job.fetch("job_name")} must set cluster=#{cluster}"
          end
        end
      end
    end
  end
' "$monitoring_dir/prometheus.yml" "$monitoring_dir"

PROMETHEUS_BEARER_TOKEN_FILE="$monitoring_dir/secrets/new-api-bearer-token.example" \
GRAFANA_ADMIN_PASSWORD_FILE="$monitoring_dir/secrets/grafana-admin-password.example" \
FEISHU_WEBHOOK_URL_FILE="$monitoring_dir/secrets/feishu-webhook-url.example" \
	POSTGRES_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/postgres-exporter-password.example" \
	REDIS_EXPORTER_PASSWORD_FILE="$monitoring_dir/secrets/redis-exporter-password.example" \
	MYSQL_EXPORTER_CONFIG_FILE="$monitoring_dir/secrets/mysql-exporter.cnf.example" \
	POSTGRES_EXPORTER_URI='postgres:5432/new-api?sslmode=disable' \
	POSTGRES_EXPORTER_USER=example \
	REDIS_EXPORTER_ADDR=redis://redis:6379 \
	NEW_API_DOCKER_NETWORK=example \
		docker compose -f "$repo_dir/docker-compose.monitoring.yml" config --format json >"$tmp_dir/compose.json"
jq -e '
  (.services | keys | sort) == ["alertmanager", "feishu-alert-webhook", "grafana", "node-exporter", "prometheus", "redis-exporter"] and
  ([.services[].image | endswith(":latest")] | any | not) and
  ([.services.prometheus, .services.grafana, .services.alertmanager, .services["feishu-alert-webhook"]] | map(has("healthcheck")) | all) and
  (.services["feishu-alert-webhook"] | has("ports") | not) and
  (.services["feishu-alert-webhook"].read_only == true) and
  (.services["feishu-alert-webhook"].networks | has("monitoring")) and
  ([.services["feishu-alert-webhook"].secrets[].source] | index("feishu_webhook_url") != null) and
  (.services.alertmanager.depends_on["feishu-alert-webhook"].condition == "service_healthy") and
  (.secrets | has("feishu_webhook_url")) and
  (.secrets | has("alertmanager_webhook_url") | not) and
  (.services["node-exporter"] | has("ports") | not) and
  (.services["redis-exporter"] | has("ports") | not) and
  (.services["redis-exporter"].networks | has("application")) and
  (.services["node-exporter"].networks | has("monitoring"))
' "$tmp_dir/compose.json" >/dev/null

jq -e '
  .apiVersion == 1 and
  (.datasources | length) == 1 and
  .datasources[0].type == "prometheus" and
  .datasources[0].uid == "prometheus" and
  .datasources[0].isDefault == true
' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml" >/dev/null 2>&1 || {
	# jq cannot parse YAML; validate the required datasource contract textually.
	rg -q '^apiVersion: 1$' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml"
	rg -q '^  - name: Prometheus$' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml"
	rg -q '^    type: prometheus$' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml"
	rg -q '^    uid: prometheus$' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml"
	rg -q '^    isDefault: true$' "$monitoring_dir/grafana/provisioning/datasources/prometheus.yml"
}

rg -q '^apiVersion: 1$' "$monitoring_dir/grafana/provisioning/dashboards/default.yml"
rg -q '^    folder: new-api 监控$' "$monitoring_dir/grafana/provisioning/dashboards/default.yml"
rg -q '^    folder: new-api 扩展监控$' "$monitoring_dir/grafana/provisioning/dashboards/default.yml"
rg -q '^      path: /var/lib/grafana/dashboards/core$' "$monitoring_dir/grafana/provisioning/dashboards/default.yml"
rg -q '^      path: /var/lib/grafana/dashboards/extended$' "$monitoring_dir/grafana/provisioning/dashboards/default.yml"

validate_dashboard() {
	dashboard_file=$1
	expected_uid=$2
	minimum_panels=$3
	shift 3

	jq -e --arg expected_uid "$expected_uid" --argjson minimum_panels "$minimum_panels" '
    .uid == $expected_uid and
    (.title | length > 0) and
    .schemaVersion >= 39 and
    .refresh == "15s" and
    .timezone == "browser" and
    (.time.from == "now-6h") and
    (.panels | length >= $minimum_panels) and
    ([.panels[].id] | length == (unique | length)) and
    ([.panels[] | select((.title // "") == "")] | length == 0) and
    ([.panels[] | select(.type != "row" and (.description // "") == "")] | length == 0) and
    ([.panels[] | select(.type != "row") | .targets[]? | select(.datasource.uid != "prometheus")] | length == 0) and
    ([.panels[] | select(.type != "row") | .targets[]? | select((.expr // "") == "")] | length == 0)
  ' "$dashboard_file" >/dev/null

	for variable_name in "$@"; do
		jq -e --arg variable_name "$variable_name" '
      [.templating.list[] | select(.name == $variable_name)] | length == 1
    ' "$dashboard_file" >/dev/null
	done
}

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/core/host-overview.json" \
	"newapi-host-overview" \
	12 \
	cluster instance

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/core/application-overview.json" \
	"newapi-application-overview" \
	7 \
	cluster instance

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/core/middleware-overview.json" \
	"newapi-middleware-overview" \
	18 \
	cluster

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/core/channel-overview.json" \
	"newapi-channel-overview" \
	18 \
	cluster channel_id

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/extended/billing-overview.json" \
	"newapi-billing-overview" \
	6 \
	cluster instance billing_source

validate_dashboard \
	"$monitoring_dir/grafana/dashboards/extended/task-overview.json" \
	"newapi-task-overview" \
	9 \
	cluster instance platform

jq -e '
  ([.panels[] | select(.id == 2 and .type == "table")] | length) == 1 and
  ([.panels[] | select(.id == 2) | .targets[] | select(.instant != true or .format != "table")] | length == 0) and
  ([.panels[].targets[]? | select((.expr | test("newapi(_|:)channel")) and (.expr | contains("channel_id") | not))] | length == 0) and
  ([.panels[].title] | index("上游缓存命中率") != null) and
  ([.panels[].title] | index("TTFT 与上游首字节 P95") != null) and
  ([.panels[].title] | index("流式中断与客户端取消") != null)
' "$monitoring_dir/grafana/dashboards/core/channel-overview.json" >/dev/null

jq -s '
  [
    .[].panels[].targets[]?
    | select((.expr // "") != "")
    | .expr
    | gsub("\\$cluster"; "default")
    | gsub("\\$instance"; ".*")
    | gsub("\\$relay_format"; ".*")
    | gsub("\\$channel_id"; ".*")
    | gsub("\\$billing_source"; ".*")
    | gsub("\\$platform"; ".*")
  ]
  | to_entries
  | {
      groups: [
        {
          name: "newapi-grafana-queries",
          rules: [
            .[]
            | {
                record: ("newapi_dashboard_query_" + (.key | tostring)),
                expr: .value
              }
          ]
        }
      ]
    }
' \
	"$monitoring_dir/grafana/dashboards/core/host-overview.json" \
	"$monitoring_dir/grafana/dashboards/core/application-overview.json" \
	"$monitoring_dir/grafana/dashboards/core/middleware-overview.json" \
	"$monitoring_dir/grafana/dashboards/core/channel-overview.json" \
	"$monitoring_dir/grafana/dashboards/extended/billing-overview.json" \
	"$monitoring_dir/grafana/dashboards/extended/task-overview.json" \
	>"$tmp_dir/dashboard-rules.json"

dashboard_query_count=$(jq '.groups[0].rules | length' "$tmp_dir/dashboard-rules.json")
if [ "$dashboard_query_count" -lt 100 ]; then
	echo "expected at least 100 dashboard PromQL expressions, got $dashboard_query_count" >&2
	exit 1
fi

"$promtool_bin" check rules --lint=all --lint-fatal "$tmp_dir/dashboard-rules.json"

echo "monitoring static validation passed"
