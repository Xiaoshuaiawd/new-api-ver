# Prometheus 监控部署与运维

本文说明如何启用 new-api 的 `/metrics`、启动独立 Prometheus/Grafana/Alertmanager 栈，并解释多实例聚合、安全策略、常用查询和故障排查。

## 组件与职责

| 组件 | 默认地址 | 职责 | 数据是否可用于财务对账 |
| --- | --- | --- | --- |
| new-api `/metrics` | `http://应用地址/metrics` | 导出运行时、HTTP、Relay、渠道、计费、异步任务、Redis/缓存和数据库连接池指标 | 否 |
| Prometheus | `http://localhost:9090` | 抓取、保存时间序列、执行 Recording/告警规则 | 否 |
| Node Exporter | Docker 内网 `node-exporter:9100` | 导出整台主机 CPU、内存、文件系统和磁盘 I/O | 否 |
| PostgreSQL Exporter | Docker 内网 `postgres-exporter:9187` | 导出 PostgreSQL 连接、事务、缓存、锁和容量 | 否 |
| MySQL Exporter | Docker 内网 `mysqld-exporter:9104` | 可选导出 MySQL 连接、查询、InnoDB 和容量 | 否 |
| Redis Exporter | Docker 内网 `redis-exporter:9121` | 导出 Redis 内存、客户端、命令、Keyspace 和 Key 生命周期 | 否 |
| Grafana | `http://localhost:3001` | 展示主机、程序、中间件、渠道及扩展监控面板 | 否 |
| Alertmanager | `http://localhost:9093` | 告警分组、通知、恢复通知、静默和抑制 | 否 |

精确用量、账务、用户/Token/IP 明细仍以数据库消费日志和审计日志为准。Prometheus Counter 用于趋势和告警，不替代账单。

## 版本与目录

独立部署栈固定使用：

- Prometheus `v3.5.0`
- Alertmanager `v0.28.1`
- Grafana `12.1.0`
- Node Exporter `v1.9.1`
- PostgreSQL Exporter `v0.17.1`
- MySQL Exporter `v0.17.2`
- Redis Exporter `v1.67.0`

主要文件：

```text
docker-compose.monitoring.yml
deploy/monitoring/
  prometheus.yml
  recording-rules.yml
  alert-rules.yml
  relay-latency-thresholds.yml
  relay-concurrency-thresholds.yml
  alertmanager.yml.example
  targets/
    postgres-exporter.yml
    mysql-exporter.yml
  validate.sh
  grafana/
    provisioning/
    dashboards/
      core/
        host-overview.json
        application-overview.json
        middleware-overview.json
        channel-overview.json
      extended/
        billing-overview.json
        task-overview.json
  secrets/
    *.example
```

监控栈独立于默认业务 Compose，不会自动启动 new-api、数据库或 Redis。

## 一、启用应用指标

### 安全决策表

| 场景 | 应用配置 | Prometheus 配置 | 建议 |
| --- | --- | --- | --- |
| Bearer Token | `PROMETHEUS_ENABLED=true`、`PROMETHEUS_BEARER_TOKEN=<随机值>` | `authorization.credentials_file` 保存相同值 | 默认推荐 |
| IP/CIDR 白名单 | `PROMETHEUS_ENABLED=true`、`PROMETHEUS_ALLOWED_IPS=<IP/CIDR>` | Prometheus 来源地址必须在白名单内 | 固定内网适用 |
| Token + IP | 同时设置以上两项 | 满足任意一种即可 | OR 语义，不是 AND |
| Public | `PROMETHEUS_ENABLED=true`、`PROMETHEUS_ALLOW_PUBLIC=true` | 可移除 authorization | 仅隔离测试环境使用 |
| 关闭 | `PROMETHEUS_ENABLED=false` 或不设置 | target 会返回 404 | 默认状态 |

生产环境启用 `/metrics` 时必须至少配置 Bearer、IP/CIDR 白名单或显式 Public。若只设置 `PROMETHEUS_ENABLED=true` 而没有任何保护方式，应用会拒绝启动。

Bearer Token 应由部署平台的 secret 管理能力注入应用环境变量，不要写入仓库、普通系统设置或命令行参数。Prometheus 侧使用文件读取同一个值：

```yaml
authorization:
  type: Bearer
  credentials_file: /etc/prometheus/secrets/new-api-bearer-token
```

其他配置：

- `PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true`：关闭渠道 attempt 总耗时、渠道 TTFT 和 transport 首字节 Histogram，保留 attempt、retry、inflight 和 Token Counter。渠道数 `N >= 300` 或预算试算超过 `45,000` 条自定义序列时必须启用。
- `NODE_NAME`：用于应用日志识别节点；Prometheus 的 `instance` 标签由 target 配置决定，两者不会自动映射，多实例部署时都应保持稳定且唯一。

## 二、准备 Secret

仓库中的 `.example` 文件只是格式示例，不能直接用于生产。请在仓库外创建以下仅部署用户可读的文件：

| 环境变量 | 文件内容 |
| --- | --- |
| `PROMETHEUS_BEARER_TOKEN_FILE` | 与应用 `PROMETHEUS_BEARER_TOKEN` 完全相同的随机值 |
| `GRAFANA_ADMIN_PASSWORD_FILE` | Grafana 管理员强密码 |
| `ALERTMANAGER_WEBHOOK_URL_FILE` | 接收 Alertmanager webhook 的完整 HTTPS URL |
| `POSTGRES_EXPORTER_PASSWORD_FILE` | PostgreSQL Exporter 只读监控账号密码 |
| `REDIS_EXPORTER_PASSWORD_FILE` | Redis 地址到密码的 JSON 映射；键必须与 `REDIS_EXPORTER_ADDR` 一致并包含 `redis://` 或 `rediss://`，无密码时值使用空字符串 |
| `MYSQL_EXPORTER_CONFIG_FILE` | MySQL Exporter 的 `[client]` 配置；仅启用 MySQL profile 时使用 |

示例环境变量只保存文件路径，不保存 secret 本身：

```bash
export PROMETHEUS_BEARER_TOKEN_FILE=/opt/new-api-monitoring/secrets/new-api-bearer-token
export GRAFANA_ADMIN_PASSWORD_FILE=/opt/new-api-monitoring/secrets/grafana-admin-password
export ALERTMANAGER_WEBHOOK_URL_FILE=/opt/new-api-monitoring/secrets/alertmanager-webhook-url
export POSTGRES_EXPORTER_PASSWORD_FILE=/opt/new-api-monitoring/secrets/postgres-exporter-password
export REDIS_EXPORTER_PASSWORD_FILE=/opt/new-api-monitoring/secrets/redis-exporter-password
export MYSQL_EXPORTER_CONFIG_FILE=/opt/new-api-monitoring/secrets/mysql-exporter.cnf
```

Redis Exporter 密码文件示例：

```json
{
  "redis://redis:6379": "replace-with-the-redis-password"
}
```

建议 Secret 使用 `0600` 权限，并确保固定镜像的运行用户能读取。当前 PostgreSQL Exporter `v0.17.1` 使用 UID/GID `65534:65534`，Redis Exporter `v1.67.0` 使用 `59000:59000`；若宿主机 Secret 保持 root 所有且 `0600`，Exporter 会因 `permission denied` 退出。轮换 Bearer Token 时应先同步更新应用和 Prometheus 文件，再重载两端，避免产生抓取空窗。

## 三、配置抓取目标

### 业务网络与 Exporter

监控 Compose 通过外部网络连接 PostgreSQL、MySQL 和 Redis。先确认业务容器所在网络：

```bash
docker network ls
export NEW_API_DOCKER_NETWORK=new-api-ver_new-api-network
```

不要填写容器临时 IP。`NEW_API_DOCKER_NETWORK` 必须是实际 Docker 网络名，Exporter 本身不发布宿主机端口。

当前 PostgreSQL 部署需要：

```bash
export POSTGRES_EXPORTER_URI='postgres:5432/new-api?sslmode=disable'
export POSTGRES_EXPORTER_USER=exporter
export REDIS_EXPORTER_ADDR='redis://redis:6379'
```

PostgreSQL target 位于 `deploy/monitoring/targets/postgres-exporter.yml`。MySQL target 文件默认是空列表，因此未启用 MySQL 时 Prometheus 不会产生一个永久 DOWN 的假 target。

MySQL 切换步骤：

1. 为 MySQL 创建最小权限监控账号并填写运行时 `MYSQL_EXPORTER_CONFIG_FILE`。
2. 将 `deploy/monitoring/targets/mysql-exporter.yml` 改为包含 `mysqld-exporter:9104`、固定 `cluster` 和可读 `instance` 标签。
3. 使用 `--profile mysql` 启动；PostgreSQL 部署使用 `--profile postgres`，两者不要求同时启用。

### 单实例

默认 `deploy/monitoring/prometheus.yml` 从容器内抓取宿主机的 `host.docker.internal:3000`，并显式设置稳定标签：

```yaml
- job_name: new-api
  metrics_path: /metrics
  static_configs:
    - targets:
        - host.docker.internal:3000
      labels:
        cluster: default
        instance: new-api-1
```

Compose 为 Linux 添加了 `host.docker.internal:host-gateway` 映射。若应用不在宿主机的 3000 端口，请修改 target。

### 多实例

每个实例使用独立的 `static_configs` 项，保证 `instance` 唯一：

```yaml
- job_name: new-api
  metrics_path: /metrics
  scheme: http
  authorization:
    type: Bearer
    credentials_file: /etc/prometheus/secrets/new-api-bearer-token
  static_configs:
    - targets: ["new-api-1:3000"]
      labels:
        cluster: default
        instance: new-api-1
    - targets: ["new-api-2:3000"]
      labels:
        cluster: default
        instance: new-api-2
```

若应用在另一个 Compose 网络中，应创建或复用一个外部网络，把 Prometheus 与应用实例接入同一网络，再使用稳定的服务名作为 target。不要依赖容器临时 IP。

### 多集群

默认外部标签为：

```yaml
external_labels:
  cluster: default
```

每个环境或集群必须使用不同的 `cluster`。`external_labels` 只用于 Prometheus 对外发送数据和告警时补充标签，不会自动写入本地抓取序列；因此每个抓取 target 还必须在 `static_configs.labels`（或等价的 `relabel_configs`）中显式设置相同的 `cluster`。Recording Rules 始终保留 `cluster/job`，不会跨集群混算。

`NewAPIMasterCollectorAbsent` 和 `NewAPITaskQueueCollectorAbsent` 默认固定检查 `cluster="default"`。修改集群名时，必须同步修改 `deploy/monitoring/alert-rules.yml` 中的选择器和 `alert-rules.test.yml` 固定用例，然后重新运行规则测试。

## 四、静态校验

需要以下工具：

- `docker compose`
- `promtool`
- `amtool`（推荐；不可用时统一脚本使用 Ruby YAML 解析和契约检查作为降级校验）
- `jq`
- `rg`

运行统一校验：

```bash
PROMTOOL_BIN=/path/to/promtool \
AMTOOL_BIN=/path/to/amtool \
deploy/monitoring/validate.sh
```

校验内容包括：

- Prometheus 配置、47 条基础 Recording Rules、72 条告警规则，以及默认 0 条 Relay/渠道延迟与并发阈值规则。
- Recording/告警固定输入测试。
- Alertmanager route、恢复通知和 warning/critical 抑制配置。
- PostgreSQL/MySQL 两种 Compose profile、固定镜像版本、外部业务网络、Exporter 内网端口和运行时 Secret。
- Grafana 双文件夹 provisioning、6 个中文 dashboard JSON、变量、面板 ID、渠道维度保留和 108 条 PromQL 语法检查。

也可以单独执行：

```bash
promtool check config deploy/monitoring/prometheus.yml
promtool check rules --lint=all --lint-fatal \
  deploy/monitoring/recording-rules.yml \
  deploy/monitoring/alert-rules.yml \
  deploy/monitoring/relay-latency-thresholds.yml \
  deploy/monitoring/relay-concurrency-thresholds.yml
promtool test rules deploy/monitoring/recording-rules.test.yml
promtool test rules deploy/monitoring/alert-rules.test.yml
amtool check-config deploy/monitoring/alertmanager.yml.example
docker compose -f docker-compose.monitoring.yml --profile postgres config
docker compose -f docker-compose.monitoring.yml --profile mysql config
```

## 五、启动与检查

确认应用已启用 `/metrics`、Secret 路径、业务网络和 PostgreSQL/Redis 连接参数已设置后：

```bash
docker compose -f docker-compose.monitoring.yml --profile postgres up -d
docker compose -f docker-compose.monitoring.yml ps
```

健康检查：

```bash
curl -fsS http://localhost:9090/-/ready
curl -fsS http://localhost:3001/api/health
curl -fsS http://localhost:9093/-/ready
```

然后检查：

1. Prometheus `Status → Targets` 中 `new-api`、`node-exporter`、`postgres-exporter` 和 `redis-exporter` target 为 `UP`。
2. Prometheus `Status → Rules` 中 Recording/Alert Rules 加载成功。
3. Grafana 自动出现两个文件夹：
   - `new-api 监控`：`new-api / 主机总览`、`new-api / 程序总览`、`new-api / 中间件总览`、`new-api / 渠道总览`
   - `new-api 扩展监控`：`new-api / 计费总览`、`new-api / 任务总览`
4. Alertmanager `Status` 页面能看到当前配置和运行状态。

Grafana provisioning 设置为不可直接保存 UI 修改。需要改面板时修改仓库中的 JSON，等待最多 30 秒自动重新加载。

## 六、Dashboard 口径

### 主机总览

展示 Node Exporter 采集的整机 CPU 使用率、系统负载、内存/Swap、文件系统空间、磁盘读写吞吐和 I/O 耗时。这一页只反映宿主机资源，不把容器或 new-api 进程数据混入主机口径。

变量为 `cluster`、`instance` 和 `device`。文件系统面板排除 tmpfs、overlay 等临时文件系统；容量剩余 `0` 表示已采集且无剩余空间，`No data` 表示 Node Exporter target 或对应设备序列缺失。

### 程序总览

展示 new-api 实例健康、进程 CPU/常驻内存、Go 堆分配、Goroutine、GC 和运行时趋势，同时保留最终 Relay RPM、成功率、P50/P95/P99、按 Relay 格式配置的延迟阈值线、inflight/流式 inflight、并发阈值线、限流和固定错误类型。

Goroutine 和 Go heap 增长使用 `deriv(...[30m])` 按单实例计算每秒线性增长率。Goroutine warning 同时要求当前值 `>= 500`、增长率 `> 0.05/s`；heap warning 同时要求当前分配 `>= 512 MiB`、增长率 `> 128 KiB/s`。两者都要求进程运行至少 30 分钟且条件持续 15 分钟。这些是初始候选阈值，上线后应结合实例规格、业务负载和 Go profile 校准。

变量为 `cluster`、`instance` 和 `relay_format`。

### 中间件总览

展示 PostgreSQL、可选 MySQL 和 Redis 的状态与容量。PostgreSQL 包含连接、事务、缓存命中、锁和数据库容量；MySQL 包含连接、查询、InnoDB 和表容量；Redis 包含内存、客户端、命令、Keyspace、命中/过期和业务缓存/降级趋势。未启动的数据库 profile 对应区域显示“暂无数据”，不应解释为故障。

应用内部数据库等待使用 `sql.DB.Stats()` 的累计 `WaitCount` 和 `WaitDuration`。Recording Rules 先按 `cluster/job/instance/database` 计算 5 分钟增量，再用同一实例的等待时长除以等待次数。面板中 `0` 表示窗口内没有等待，`No data` 表示指标缺失。初始 warning 同时要求 5 分钟等待次数 `>= 20`、平均等待 `> 0.1s`，并持续 `10m`；需在生产观察期校准。

变量为 `cluster`、`instance`、`database` 和 `redis_instance`。

### 渠道总览

渠道实时口径使用 1 分钟窗口：attempt RPM、失败 RPM、重试 RPM、成功率和超时率。性能与缓存口径使用 5 分钟窗口：P90/P95 渠道 attempt 总耗时、P95 TTFT、P95 上游响应头首字节、上游缓存命中率和 Token 每分钟吞吐。页面还展示渠道 inflight、启用状态、固定错误分类和重试原因，可在多个 `channel_id` 之间对比。

变量：

- `cluster`
- `instance`
- `channel_id`

上游缓存命中率定义为 `cache_read Token / input Token`，不是 Redis 命中率，也不是按请求条数统计。TTFT 从当前渠道 attempt 开始计时，只记录该渠道到第一个可交付内容的时间，不包含前一个失败渠道的耗时。Provider 已返回 Usage 且 `cache_read=0` 时显示 `0%`；Provider 完全没有返回 Usage 时，缓存命中率和 Token 吞吐面板显示“暂无数据”，不伪造为 `0`。

渠道 Histogram 被关闭时，P90/P95、TTFT 和上游首字节面板显示 `No data`；attempt、retry、inflight、成功率和 Token Counter 仍然可用。上游首字节指标使用 `httptrace.GetConn` → `GotFirstResponseByte` 口径，覆盖共享 HTTP、AWS Bedrock 原生 SDK 和已接入的 WebSocket Upgrade 路径；它不是完整握手或第一条应用消息耗时。鉴权、文件上传、模型管理和任务轮询等辅助请求不混入。

### 计费总览（扩展监控）

包含 Token 用量、内部 quota 扣除/退款/净额度、按冻结分组倍率还原的实际额度、计费操作成功/失败、固定失败原因、订阅拒绝和 quota saturation。所有 Counter 面板使用 `rate` 或净速率表达，不用于财务对账。

变量：

- `cluster`
- `instance`
- `billing_source`：`wallet` / `subscription` / `unknown`

消费日志开关关闭时，consume 的 quota/Token 面板可为 `No data`；refund 和计费操作面板仍按实际事件展示。Prometheus 指标只用于趋势和告警，使用日志才是精确对账数据源。

### 任务总览（扩展监控）

包含任务提交 RPM、completion 成功比例、poll 错误比例、成功/失败终态 RPM、P50/P95 端到端耗时、按 platform/state 的当前积压和 `task_queue` collector 健康状态。

变量：

- `cluster`
- `instance`：只过滤每实例事件指标；Master-only 队列面板故意不应用该变量，避免选择 Slave 时把共享队列误显示为无数据。
- `platform`：`midjourney` / `suno` / `video` / `other`

`newapi_task_poll_total` 只描述实际上游查询是否成功，不代表本地终态持久化、退款或结算结果。`newapi_task_completions_total` 和 duration 只在首次终态 CAS 获胜时记录，重复轮询、重复 Notify 和 CAS 失败不会重复增加。

队列 collector 会为 4 个 platform × 3 个 state 零填充，因此队列面板显示 `0` 表示当前没有积压；显示 `No data`/`ABSENT` 表示 Master collector 缺失或查询失败。积压告警当前使用 `> 100` 且持续 15 分钟的候选阈值，必须在生产观察期后校准，不能当作所有部署的最终容量上限。

## 七、常用 PromQL

以下应用示例默认 `cluster="default", job="new-api"`；Exporter 示例显式使用各自的 `job`。

```promql
# 最终请求 RPM
newapi:relay_request_rate:5m{cluster="default",job="new-api"} * 60

# 最终请求成功比例
newapi:relay_success_ratio:5m{cluster="default",job="new-api"}

# 按 Relay 格式同时查看观测分位数、配置阈值和 5 分钟最终请求数
newapi:relay_duration_seconds:p95_5m{cluster="default",job="new-api",relay_format="openai"}
newapi:relay_duration_seconds:p99_5m{cluster="default",job="new-api",relay_format="openai"}
newapi_relay_latency_threshold_seconds{cluster="default",job="new-api",relay_format="openai",quantile=~"p95|p99"}
newapi:relay_request_increase_by_format:5m{cluster="default",job="new-api",relay_format="openai"}

# 固定错误类型的 5 分钟比例和事件量
newapi:relay_error_ratio:5m{cluster="default",job="new-api",error_type=~"rate_limit|upstream_5xx|timeout"}
newapi:relay_error_increase:5m{cluster="default",job="new-api",error_type=~"rate_limit|upstream_5xx|timeout"}

# 按 Relay 格式同时查看实际并发和配置阈值
newapi:relay_inflight_by_format{cluster="default",job="new-api",relay_format="openai"}
newapi_relay_inflight_threshold{cluster="default",job="new-api",relay_format="openai",severity=~"warning|critical"}

# 宿主机 CPU 使用率、可用内存比例和根文件系统剩余比例
100 * (1 - avg by (cluster, instance) (rate(node_cpu_seconds_total{cluster="default",job="node-exporter",mode="idle"}[5m])))
100 * node_memory_MemAvailable_bytes{cluster="default",job="node-exporter"} / node_memory_MemTotal_bytes{cluster="default",job="node-exporter"}
100 * node_filesystem_avail_bytes{cluster="default",job="node-exporter",mountpoint="/"} / node_filesystem_size_bytes{cluster="default",job="node-exporter",mountpoint="/"}

# PostgreSQL 当前连接和缓存命中率
sum by (cluster, instance, datname) (pg_stat_database_numbackends{cluster="default",job="postgres-exporter"})
100 * sum by (cluster, instance, datname) (rate(pg_stat_database_blks_hit{cluster="default",job="postgres-exporter"}[5m]))
/
clamp_min(sum by (cluster, instance, datname) (rate(pg_stat_database_blks_hit{cluster="default",job="postgres-exporter"}[5m]) + rate(pg_stat_database_blks_read{cluster="default",job="postgres-exporter"}[5m])), 1)

# Redis 内存使用和连接客户端
redis_memory_used_bytes{cluster="default",job="redis-exporter"}
redis_connected_clients{cluster="default",job="redis-exporter"}

# 渠道 1 分钟实时流量、成功率和超时率
newapi:channel_attempt_rpm:1m{cluster="default",job="new-api"}
newapi:channel_failure_rpm:1m{cluster="default",job="new-api"}
newapi:channel_retry_rpm:1m{cluster="default",job="new-api"}
newapi:channel_success_ratio:1m{cluster="default",job="new-api"}
newapi:channel_timeout_ratio:1m{cluster="default",job="new-api"}

# 渠道 5 分钟 P90/P95、TTFT P95 和上游响应头首字节 P95
newapi:channel_duration_seconds:p90_5m{cluster="default",job="new-api"}
newapi:channel_duration_seconds:p95_5m{cluster="default",job="new-api"}
newapi:channel_ttft_seconds:p95_5m{cluster="default",job="new-api"}
newapi:channel_first_byte_seconds:p95_5m{cluster="default",job="new-api"}

# 渠道上游缓存命中率与 Token 每分钟吞吐
newapi:channel_cache_hit_ratio:5m{cluster="default",job="new-api"}
newapi:channel_tokens_per_minute:5m{cluster="default",job="new-api"}

# 计费操作失败比例（告警额外要求 15 分钟最小事件量）
newapi:billing_failure_ratio:15m{cluster="default",job="new-api"}

# 订阅拒绝事件
sum by (reason) (newapi:subscription_rejection_increase:15m{cluster="default",job="new-api"})

# 额度饱和事件
sum by (kind, operation) (newapi:quota_saturation_increase:15m{cluster="default",job="new-api"})

# 异步任务提交 RPM
sum by (platform, result) (
  newapi:task_submission_rate:5m{cluster="default",job="new-api"}
) * 60

# 已完成任务成功比例
newapi:task_completion_success_ratio:15m{cluster="default",job="new-api"}

# 上游 poll 错误比例
newapi:task_poll_error_ratio:5m{cluster="default",job="new-api"}

# 任务 P95 端到端耗时
newapi:task_duration_seconds:p95_15m{cluster="default",job="new-api"}

# 每个平台当前未完成任务总量
newapi:task_queue_total:platform{cluster="default",job="new-api"}

# Task queue collector 缺失或查询失败
absent(newapi_shared_collector_up{cluster="default",job="new-api",collector="task_queue"})
or
max by (cluster, job) (newapi_shared_collector_up{cluster="default",job="new-api",collector="task_queue"}) == 0

# 单实例 DB 连接池利用率；无上限连接池不会返回序列
newapi:db_pool_utilization:instance{cluster="default",job="new-api"}

# 单实例 DB 5 分钟等待次数与平均等待时长
newapi:db_wait_increase:5m{cluster="default",job="new-api"}
newapi:db_wait_average_seconds:5m{cluster="default",job="new-api"}

# 单实例 Goroutine 和 Go heap 的 30 分钟每秒增长率
newapi:go_goroutines_growth_per_second:30m{cluster="default",job="new-api"}
newapi:go_heap_alloc_bytes_growth_per_second:30m{cluster="default",job="new-api"}

# Master collector 缺失
absent(newapi_shared_collector_up{cluster="default",job="new-api",collector="channel_state"})

# 限流拒绝 RPM；scope/reason 都是固定枚举
sum by (scope, reason) (
  rate(newapi_rate_limit_rejections_total{cluster="default",job="new-api"}[5m])
) * 60

# Redis 操作 RPM；pipeline 和 pipeline_command 是不同口径
sum by (command, operation_type, result) (
  rate(newapi_redis_operations_total{cluster="default",job="new-api"}[5m])
) * 60

# 应用缓存命中率；按 redis/memory 后端分别计算
sum by (backend) (
  rate(newapi_cache_lookups_total{cluster="default",job="new-api",result="hit"}[5m])
)
/
clamp_min(
  sum by (backend) (
    rate(newapi_cache_lookups_total{cluster="default",job="new-api"}[5m])
  ),
  0.000001
)
```

Counter 必须通过 `rate()` 或 `increase()` 使用，不能把各实例当前 Counter 值直接相加后解释为长期累计账务。应用重启、扩缩容和 Prometheus 数据缺口都会影响时间窗口。

## 八、告警、恢复、抑制与静默

当前首批告警覆盖：

- 服务成功率 warning/critical，并带最低请求量门槛。
- Relay P95/P99 延迟超过按 `cluster/job/relay_format` 配置的阈值；没有阈值序列时默认休眠。
- Relay inflight 超过按 `cluster/job/relay_format` 配置的 warning/critical 阈值；没有阈值序列时默认休眠。
- Relay `rate_limit`、`upstream_5xx` 和 `timeout` 异常比例；均同时要求最低总请求量和最低错误事件量。本地限流拒绝使用独立 Counter 门槛，不与上游 429 混算。
- 渠道失败比例、重试比例和窗口内无成功。
- 单实例 DB 连接池利用率 warning/critical。
- Redis 命令错误和业务降级持续异常；均要求 5 分钟内至少 5 个事件并持续 5 分钟。
- 计费失败、订阅拒绝和 quota saturation；分别要求总操作量/失败量、拒绝次数或饱和次数的最小事件量，不会被单次异常触发。
- 异步任务积压超过候选阈值并持续 15 分钟，以及 task_queue collector 查询失败或 Master collector 缺失。
- 应用 target down。
- Master-only 渠道 collector 缺失。
- Node、PostgreSQL、MySQL、Redis Exporter 可用性，Prometheus 抓取接近超时和应用采集器错误。
- 主机 CPU、可用内存、文件系统空间/只读、磁盘 I/O 与持续 swap。
- 应用进程 CPU、文件句柄，PostgreSQL/MySQL 连接数、PostgreSQL 死锁，以及 Redis 不可用、内存上限、淘汰和拒绝连接。
- 渠道流式首字实时等待：超过 30 秒的并发 `>= 3` 持续 2 分钟发 warning；超过 60 秒的并发 `>= 5` 持续 2 分钟发 critical。集群级门槛为 10/20。
- 渠道 P95 总耗时、TTFT、上游首字节和渠道并发使用独立阈值文件；未配置渠道不会产生该类告警。

错误异常告警的初始门槛：

- Relay `rate_limit`：5 分钟比例 `> 10%`、最终请求数 `>= 100`、对应错误数 `>= 10`，持续 10 分钟。
- Relay `upstream_5xx`：5 分钟比例 `> 5%`、最终请求数 `>= 100`、对应错误数 `>= 10`，持续 10 分钟。
- Relay `timeout`：5 分钟比例 `> 5%`、最终请求数 `>= 50`、对应错误数 `>= 5`，持续 10 分钟。
- 本地限流拒绝：5 分钟事件数 `>= 20`，持续 10 分钟。处理前先按 `scope/reason` 下钻，不应仅因告警触发就直接放宽限额。

Alertmanager 的 webhook receiver 设置 `send_resolved: true`，告警恢复后会发送恢复通知。

抑制规则：

- `NewAPIServiceSuccessRateCritical` 抑制同一 `cluster/job` 的 `NewAPIServiceSuccessRateLow`。
- `NewAPIDBPoolUtilizationCritical` 抑制同一 `cluster/job/instance/database` 的 `NewAPIDBPoolUtilizationHigh`。
- `NewAPIRelayP99LatencyHigh` 抑制同一 `cluster/job/relay_format` 的 `NewAPIRelayP95LatencyHigh`。
- `NewAPIRelayInflightCritical` 抑制同一 `cluster/job/relay_format` 的 `NewAPIRelayInflightHigh`。
- 渠道 TTFT/首字节/总耗时/并发和流式首字等待的 critical 均抑制同一渠道的 warning；主机 CPU/内存、PostgreSQL 连接数与 Redis 内存同样按实例抑制 warning。

维护窗口可使用 Alertmanager UI 创建 silence，或使用 `amtool`：

```bash
amtool --alertmanager.url=http://localhost:9093 silence add \
  cluster=default job=new-api \
  --duration=2h \
  --comment='planned maintenance'
```

Silence 只停止通知，不停止 Prometheus 记录指标或计算告警状态。维护结束后应确认 silence 已到期或主动删除。

### Relay 延迟阈值配置与启用

`deploy/monitoring/relay-latency-thresholds.yml` 默认只有空的 `rules: []`，不会导出 `newapi_relay_latency_threshold_seconds`，因此 P95/P99 延迟告警默认不会进入 pending。必须先观察真实的按格式耗时基线，再为具体 `cluster/job/relay_format/quantile` 增加阈值；禁止用一个统一秒数覆盖文本、图片、音频、 Realtime 和异步任务。

同一格式通常分别配置 P95 和 P99，例如：

```yaml
groups:
  - name: newapi-relay-latency-thresholds
    rules:
      - record: newapi_relay_latency_threshold_seconds
        expr: vector(8)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          quantile: p95
      - record: newapi_relay_latency_threshold_seconds
        expr: vector(12)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          quantile: p99
```

允许的 `quantile` 只有 `p95`、`p99`。允许的 `relay_format` 只有：

```text
openai
claude
gemini
openai_responses
openai_responses_compaction
openai_alpha_search
openai_audio
openai_image
openai_realtime
rerank
embedding
task
mj_proxy
other
```

阈值必须写成有限正数秒的 `vector(<number>)`。相同 `cluster/job/relay_format/quantile` 不能重复，不能增加 `instance`、模型、渠道、用户、Token、IP、Request ID 或错误文本等标签。P95 warning 要求 5 分钟最终请求数 `>= 50`，P99 critical 要求 `>= 100`，两者都必须持续 `10m`。

修改阈值后先执行完整校验，再让 Prometheus 重载规则：

```bash
PROMTOOL_BIN=/path/to/promtool deploy/monitoring/validate.sh
curl -fsS -X POST http://localhost:9090/-/reload
```

重载后在 Prometheus `Status → Rules` 检查阈值规则和告警状态。阈值配置只是运维告警条件，不会自动禁用渠道、修改路由、终止请求或改变 Relay、计费和退款行为。

### Relay 并发阈值配置与启用

`deploy/monitoring/relay-concurrency-thresholds.yml` 默认只有空的 `rules: []`，不会导出 `newapi_relay_inflight_threshold`，因此 warning/critical 并发告警默认不会进入 pending。必须先观察真实的按格式并发基线，再为具体 `cluster/job/relay_format/severity` 增加阈值。

同一格式可以只配置一个严重级别，也可以同时配置 warning 和 critical。例如：

```yaml
groups:
  - name: newapi-relay-concurrency-thresholds
    rules:
      - record: newapi_relay_inflight_threshold
        expr: vector(30)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          severity: warning
      - record: newapi_relay_inflight_threshold
        expr: vector(50)
        labels:
          cluster: default
          job: new-api
          relay_format: openai
          severity: critical
```

允许的 `severity` 只有 `warning`、`critical`。允许的 `relay_format` 只有：

```text
openai
claude
gemini
openai_responses
openai_responses_compaction
openai_alpha_search
openai_audio
openai_image
openai_realtime
rerank
embedding
task
mj_proxy
other
```

阈值必须写成正整数请求数的 `vector(<integer>)`。相同 `cluster/job/relay_format/severity` 不能重复，不能增加 `instance`、模型、渠道、用户、Token、IP、Request ID 或错误文本等标签。同一 `cluster/job/relay_format` 同时配置两个严重级别时，critical 必须严格大于 warning。

实际并发使用 `newapi:relay_inflight_by_format`，按 `cluster/job/relay_format` 汇总所有实例和流式状态。warning 需要实际并发严格大于阈值并持续 `10m`，critical 持续 `5m`；等于阈值或短暂峰值不会触发。多实例 Gauge 的求和是抓取时刻的趋势，不是严格同时刻的精确快照。

修改阈值后先执行完整校验，再让 Prometheus 重载规则：

```bash
PROMTOOL_BIN=/path/to/promtool deploy/monitoring/validate.sh
curl -fsS -X POST http://localhost:9090/-/reload
```

重载后可同时查询实际值和阈值：

```promql
newapi:relay_inflight_by_format{cluster="default",job="new-api",relay_format="openai"}
newapi_relay_inflight_threshold{cluster="default",job="new-api",relay_format="openai",severity=~"warning|critical"}
```

并发阈值只是运维告警条件，不是应用硬限制，不会拒绝请求、修改路由、禁用渠道或改变 Relay、计费和退款行为。

### 渠道阈值与流式首字等待

`deploy/monitoring/channel-latency-thresholds.yml` 与 `deploy/monitoring/channel-concurrency-thresholds.yml` 默认都是空规则，因此渠道 P95 总耗时、TTFT、上游首字节和渠道并发告警默认休眠。它们必须按具体 `channel_id` 写入阈值，不能使用模型、用户、IP、Request ID 等高基数标签。

```yaml
groups:
  - name: newapi-channel-latency-thresholds
    rules:
      - record: newapi_channel_latency_threshold_seconds
        expr: vector(8)
        labels:
          cluster: default
          job: new-api
          channel_id: "1"
          metric: ttft_p95
          severity: warning
      - record: newapi_channel_latency_threshold_seconds
        expr: vector(15)
        labels:
          cluster: default
          job: new-api
          channel_id: "1"
          metric: ttft_p95
          severity: critical
```

`metric` 仅允许 `duration_p95`、`ttft_p95`、`first_byte_p95`；`severity` 仅允许 `warning`、`critical`。延迟 warning/critical 分别要求 5 分钟至少 30/50 个样本，持续 10/5 分钟。渠道并发阈值的记录名为 `newapi_channel_inflight_threshold`，标签为 `cluster/job/channel_id/severity`，值必须是正整数；warning/critical 分别持续 10/5 分钟。

`newapi_channel_stream_first_token_waiting` 是实时 Gauge，不等待请求结束：它只统计当前仍未收到首个有效流式内容的渠道 attempt；收到首字、取消、失败、超时、panic 或正常结束后都会移除。该指标固定导出 `threshold_seconds="30"` 与 `"60"` 两个超过阈值的并发计数。它不把 HTTP 响应头当作首字，不受渠道 Histogram 开关影响。

## 九、无数据与常见故障

### new-api target 为 DOWN / 403

检查：

1. 应用是否设置 `PROMETHEUS_ENABLED=true`。
2. 应用 Bearer Token 与 `PROMETHEUS_BEARER_TOKEN_FILE` 内容是否完全一致，文件末尾换行通常不会成为 Token 内容，但应避免额外空格。
3. `PROMETHEUS_ALLOWED_IPS` 是否包含 Prometheus 实际来源地址。
4. target 地址、端口、DNS 和容器网络是否可达。
5. Prometheus target 详情中的 `Last Error`。

### Grafana 面板全部无数据

检查 Prometheus datasource 是否健康、dashboard 的 `cluster` 变量是否有值、`job` 是否仍为固定的 `new-api`，并在 Prometheus 直接执行：

```promql
up{job="new-api"}
```

首次启动后，`rate(...[5m])` 需要至少两个样本；完整 5 分钟窗口稳定前数值可能波动。

### 只有渠道耗时无数据

检查应用是否设置 `PROMETHEUS_DISABLE_CHANNEL_HISTOGRAM=true`。若已关闭，这是预期行为。

### DB 利用率无数据

`max_open_connections == 0` 表示连接池没有设置上限，Recording Rule 会主动排除，避免除零和错误告警。连接数面板仍然可用。

### Redis 面板无数据

先查看 `newapi_redis_enabled`。值为 `0` 表示该实例按配置未启用 Redis，不直接触发告警；值为 `1` 但操作面板无数据，表示当前时间窗口尚无 Redis 操作。缓存命中率只统计 `common` Redis 缓存 helper 和 `pkg/cachex.HybridCache.Get` 的真实读取结果，不把普通 Redis 写入或限流脚本操作当作缓存命中。

`newapi_redis_operations_total{result="miss"}` 只表达 go-redis 能识别的空结果，例如 `GET redis.Nil`；应用级 `HGETALL` 空结果、解码错误和内存缓存读取由 `newapi_cache_lookups_total` 表达。指标标签不会包含 Redis Key，具体失败 Key 只能通过受控日志排查。

### Master collector 显示 ABSENT

确认至少一个应用实例以 Master 身份运行，并且该实例的 `/metrics` 正在被抓取。Slave 不会重复导出共享渠道状态或任务队列。`channel_state` 和 `task_queue` 是两个独立 collector：前者正常但后者 `DOWN` 时，应检查主库连接以及 `tasks`/`midjourneys` 两次分组查询。

### 告警规则未加载

运行 `promtool check rules`，检查 Prometheus `Status → Rules` 和容器日志。修改规则后可以重启 Prometheus，或调用启用了 lifecycle 的 reload：

```bash
curl -fsS -X POST http://localhost:9090/-/reload
```

### Relay 延迟告警未触发或阈值线无数据

依次检查：

1. `relay-latency-thresholds.yml` 是否仍为默认空规则；空规则表示告警按设计休眠，不是 0 秒阈值。
2. 阈值的 `cluster`、`job`、`relay_format` 是否与观测分位数完全一致；这三个标签不会跨值匹配。
3. `quantile` 是否为告警所需的 `p95` 或 `p99`。
4. `newapi:relay_request_increase_by_format:5m` 是否达到 P95 的 50 个或 P99 的 100 个最终请求门槛。
5. 延迟是否连续超过阈值 10 分钟；瞬时峰值不会触发。
6. 修改文件后是否通过 `validate.sh` 并成功调用 Prometheus reload。

如果校验报告重复阈值、未知格式、额外标签或非正数，修正配置后再重载；不要绕过校验直接把错误规则放入生产。

### Relay 并发告警未触发或阈值线无数据

依次检查：

1. `relay-concurrency-thresholds.yml` 是否仍为默认空规则；空规则表示告警按设计休眠，不是 0 请求阈值。
2. 阈值的 `cluster`、`job`、`relay_format` 是否与 `newapi:relay_inflight_by_format` 完全一致；这三个标签不会跨值匹配。
3. `severity` 是否为 `warning` 或 `critical`，表达式是否为正整数 `vector(<integer>)`。
4. 实际并发是否严格大于阈值；等于阈值不会触发。
5. warning 是否连续超过阈值 10 分钟、critical 是否连续超过 5 分钟；短暂峰值不会触发。
6. 同格式同时配置两个级别时，critical 是否严格大于 warning。
7. 修改文件后是否通过 `validate.sh` 并成功调用 Prometheus reload。

如果校验报告重复阈值、未知格式、未知严重级别、额外标签、非正整数或 critical 不高于 warning，修正配置后再重载；不要绕过校验直接把错误规则放入生产。

## 十、基数验收

上线前记录实际产生样本的业务路由数 `R` 和渠道数 `N`。P0 自定义指标保守上界：

```text
80R + 111N + 1,800
```

门槛：

- 单实例自定义序列不超过 `50,000`。
- 单个指标不超过总预算的 `40%`。
- 预算超过 `45,000` 或 `N >= 300` 时关闭渠道 Histogram。

上线后核对：

```promql
prometheus_tsdb_head_series

count by (__name__) ({__name__=~"newapi_.*"})
```

把结果写入发布记录，不要只保留口头结论。

## 十一、备份与恢复

需要持久化和备份的卷：

- `new-api-monitoring_prometheus_data`
- `new-api-monitoring_grafana_data`
- `new-api-monitoring_alertmanager_data`

配置、规则、dashboard 和文档已经在 Git 中，恢复时优先从版本库恢复。数据卷备份前建议停止对应组件或使用存储层快照，避免复制进行中的 TSDB 文件产生不一致。

恢复后必须重新执行静态校验，并检查 target、rules、dashboard provisioning 和 Alertmanager 状态。

## 十二、升级流程

1. 阅读 Prometheus、Alertmanager、Grafana 对应版本的 release notes 和 breaking changes。
2. 备份三个数据卷。
3. 修改 `docker-compose.monitoring.yml` 中的固定镜像版本，禁止改成 `latest`。
4. 若 Grafana 主版本变化，同步检查 dashboard `schemaVersion`、provisioning 和 datasource 字段。
5. 运行 `deploy/monitoring/validate.sh`。
6. 拉取并启动新版本：

```bash
docker compose -f docker-compose.monitoring.yml pull
docker compose -f docker-compose.monitoring.yml up -d
docker compose -f docker-compose.monitoring.yml ps
```

7. 验证三个健康端点、Prometheus target/rules、Grafana 六个 dashboard 和一次测试告警恢复链路。

出现兼容问题时使用上一个固定镜像版本回滚，并恢复对应数据卷快照；不要用删除数据卷作为常规回滚方式。
