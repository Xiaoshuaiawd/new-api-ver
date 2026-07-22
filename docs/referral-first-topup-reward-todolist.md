# 邀请首充双向奖励二开 TodoList

> 来源文档：`docs/referral-first-topup-reward-prd.md`  
> 产品口径：被邀请用户首笔有效钱包充值实付金额 `>= 30`，被邀请用户和邀请者各获得该笔 `BaseQuota` 的 `10%` 奖励。  
> MVP 范围：仅钱包充值；不含订阅、兑换码、后台纯补偿；邀请者奖励延迟结算；退款按比例取消或扣回；管理员侧边栏新增“邀请统计”。

---

## 0. 开发前确认

- [x] 确认默认首充口径：`strict_first`，即第一笔成功钱包充值低于 30 后不再触发。
- [x] 确认门槛比较符：`Money >= 30.00`。
- [x] 确认奖励基数：只按 `TopUp.BaseQuota`，不叠加普通充值活动 `BonusQuota`。
- [x] 确认被邀请用户奖励直接进入 `Quota`。
- [x] 确认邀请者奖励先进入 `pending`，默认 7 天后结算进 `AffQuota`。
- [x] 确认订阅订单不触发邀请首充奖励。
- [x] 确认管理员“邀请统计”页面路由：推荐 `/referrals`。
- [x] 确认管理员侧边栏位置：Admin 分组中放在 `Users` 后、`Redemption Codes` 前。

---

## 1. 数据模型与迁移

### 1.1 新增邀请关系审计表

- [x] 新增 `model.ReferralInvite` 模型。
- [x] 字段包含：`id`、`inviter_id`、`invitee_id`、`aff_code`、`source`、`bind_ip`、`user_agent_hash`、`device_fingerprint`、`risk_flags`、`created_at`、`updated_at`。
- [x] 增加唯一索引：`invitee_id`。
- [x] 增加普通索引：`inviter_id`、`aff_code`、`created_at`。
- [x] `risk_flags` 使用 `TEXT` 存 JSON，保持 SQLite/MySQL/PostgreSQL 兼容。
- [x] JSON marshal/unmarshal 使用 `common.Marshal` / `common.Unmarshal`。
- [x] 在 `model/main.go` 的 AutoMigrate 注册该模型。
- [x] 添加历史回填方案：从 `users.inviter_id` 回填 `referral_invites`，source=`legacy_backfill`。

### 1.2 新增邀请奖励流水表

- [x] 新增 `model.ReferralReward` 模型。
- [x] 字段包含：`activity_id`、`activity_name`、`reward_role`、`inviter_id`、`invitee_id`、`topup_id`、`trade_no`、`payment_provider`。
- [x] 字段包含：`paid_money`、`base_quota`、`reward_percent`、`reward_quota`、`settled_quota`、`reversed_quota`、`refund_amount`。
- [x] 字段包含：`status`、`risk_status`、`risk_reason`、`risk_snapshot`。
- [x] 字段包含：`settle_at`、`settled_at`、`cancelled_at`、`reversed_at`、`created_at`、`updated_at`。
- [x] 增加唯一索引：`topup_id + reward_role`。
- [x] 增加索引：`inviter_id + status`、`invitee_id + status`、`trade_no`、`status + risk_status + settle_at`、`created_at`。
- [x] 定义 `reward_role` 常量：`invitee`、`inviter`。
- [x] 定义业务状态常量：`pending`、`settled`、`cancelled`、`reversed`、`partial_reversed`。
- [x] 定义风控状态常量：`normal`、`review`、`blocked`、`approved`、`rejected`。
- [x] 在 `model/main.go` 的 AutoMigrate 注册该模型。

---

## 2. 配置项

### 2.1 后端配置结构

- [x] 在 `setting/operation_setting/payment_setting.go` 新增 `ReferralFirstTopUpRewardSetting`。
- [x] 在 `PaymentSetting` 中新增字段：`ReferralFirstTopUpReward ReferralFirstTopUpRewardSetting`。
- [x] 默认配置：`enabled=false`。
- [x] 默认 `activity_id=referral_first_topup_v1`。
- [x] 默认 `activity_name=邀请首充双向奖励`。
- [x] 默认 `min_paid_money=30`。
- [x] 默认 `threshold_operator=gte`。
- [x] 默认 `first_topup_mode=strict_first`。
- [x] 默认 `invitee_reward_percent=10`。
- [x] 默认 `inviter_reward_percent=10`。
- [x] 默认 `inviter_settle_delay_days=7`。
- [x] 默认 `stack_with_topup_bonus=true`。
- [x] 默认 `auto_block_risky_rewards=true`。
- [x] 默认 `visible=true`。

### 2.2 配置读写与校验

- [x] 管理后台保存配置时校验比例不能为负数。
- [x] 校验 `min_paid_money >= 0`。
- [x] 校验 `threshold_operator` 只能为 `gte` 或 `gt`。
- [x] 校验 `first_topup_mode` 只能为 `strict_first` 或 `first_qualified`。
- [x] 校验延迟结算天数不能为负数。
- [x] 启用奖励且比例大于 0 时，要求 `operation_setting.IsPaymentComplianceConfirmed()` 为 true。
- [x] `/api/user/topup/info` 返回可见的邀请首充活动配置，用于充值页提示。

---

## 3. 注册与邀请关系绑定

### 3.1 普通注册

- [x] 在普通注册解析 `aff_code` 后，继续写入 `User.InviterId`。
- [x] 注册成功后新增 `ReferralInvite` 审计记录。
- [x] 记录 `source=register`。
- [x] 记录注册 IP。
- [x] 记录 User-Agent hash。
- [x] 保证同一 `invitee_id` 幂等，只创建一条邀请关系。
- [x] 邀请码无效时正常注册但不绑定邀请关系。

### 3.2 OAuth 注册

- [x] OAuth state 继续支持携带 `aff`。
- [x] OAuth 创建用户完成后新增 `ReferralInvite` 审计记录。
- [x] 记录 `source=oauth`。
- [x] 保证 OAuth 注册事务提交后再做后置审计与日志。

### 3.3 前端 aff 存储

- [x] `web/default/src/features/auth/lib/storage.ts` 将 `aff` 从纯字符串升级为带过期时间结构。
- [x] 默认有效期 7 天。
- [x] 读取时过期自动清理。
- [x] 注册成功后清除本地 `aff`。
- [x] OAuth 成功发起后或注册成功后清除本地 `aff`。
- [x] 注册页展示当前邀请来源提示。
- [x] 用户切换不同邀请链接时，按最近一次有效链接覆盖。
- [x] 为新增文案补齐 i18n 六语言。

---

## 4. 充值成功奖励结算

### 4.1 统一触发点

- [x] 在 `applyTopUpSettlementTx` 完成 `TopUp` 成功入账后接入 `applyReferralFirstTopUpRewardTx`。
- [x] 保证 Epay、Stripe、Creem、Waffo、Alipay F2F、Waffo Pancake、管理员补单都走同一奖励判断。
- [x] 不在单个支付回调里单独发邀请奖励。
- [x] 订阅订单镜像 `TopUp` 不触发邀请首充奖励。

### 4.2 资格判断

- [x] 配置未开启时跳过。
- [x] 支付合规未确认时跳过。
- [x] `topUp.UserId` 没有 `InviterId` 时跳过。
- [x] 邀请者不存在时跳过。
- [x] 邀请者或被邀请用户状态异常时取消或跳过。
- [x] `topUp.Money < 30.00` 时不触发。
- [x] `settlement.BaseQuota <= 0` 时不触发。
- [x] 排除配置中的支付渠道。
- [x] 排除配置中的用户组。
- [x] 自邀请直接取消奖励并记录风险原因。
- [x] 同一 `topup_id + reward_role` 已存在时幂等跳过。

### 4.3 首充判断

- [x] 严格首单模式：被邀请用户第一笔成功钱包充值必须是当前订单。
- [x] 查询成功钱包充值时排除订阅镜像订单。
- [x] 查询成功钱包充值时排除 `BaseQuota <= 0` 的订单。
- [x] 第一笔成功钱包充值金额 `< 30` 时记录不满足原因，并不再触发严格首单奖励。
- [x] 预留 `first_qualified` 模式，但 MVP 默认不启用。

### 4.4 奖励计算

- [x] 使用 decimal 计算奖励额度，避免浮点误差。
- [x] 被邀请用户奖励：`floor(BaseQuota * invitee_reward_percent / 100)`。
- [x] 邀请者奖励：`floor(BaseQuota * inviter_reward_percent / 100)`。
- [x] 应用单笔被邀请用户奖励上限。
- [x] 应用单笔邀请者奖励上限。
- [x] 应用邀请者月奖励上限。
- [x] 应用全站活动预算上限。
- [x] 奖励额度为 0 时不生成流水或生成 cancelled 原因。

### 4.5 被邀请用户即时到账

- [x] 创建 `reward_role=invitee` 的 `ReferralReward`。
- [x] 状态设为 `settled`。
- [x] `settled_quota=reward_quota`。
- [x] 增加被邀请用户 `Quota`。
- [x] 记录用户日志：好友邀请首充奖励到账。

### 4.6 邀请者待结算

- [x] 创建 `reward_role=inviter` 的 `ReferralReward`。
- [x] 状态设为 `pending`。
- [x] `settle_at = now + inviter_settle_delay_days`。
- [x] 风控正常时 `risk_status=normal`。
- [x] 命中风险时 `risk_status=review` 或 `blocked`。
- [x] 不立即增加邀请者 `AffQuota`。
- [x] 记录邀请者待结算日志。

---

## 5. 邀请者延迟结算任务

- [x] 新增后台定时任务或 job：扫描可结算邀请者奖励。
- [x] 条件：`status=pending`。
- [x] 条件：`risk_status in (normal, approved)`。
- [x] 条件：`settle_at <= now`。
- [x] 事务内锁定 `ReferralReward`。
- [x] 事务内锁定邀请者 `User`。
- [x] 复查订单未全额退款。
- [x] 增加邀请者 `AffQuota`。
- [x] 增加邀请者 `AffHistoryQuota`。
- [x] 更新流水为 `settled`。
- [x] 写入 `settled_at`。
- [x] 记录系统日志。
- [x] 任务支持幂等重复执行。
- [x] 任务失败时记录错误日志，不影响下一轮扫描。

---

## 6. 退款扣回

### 6.1 接入退款链路

- [x] 在 `RefundTopUpWithReference` 成功扣回充值额度后，接入邀请奖励退款处理。
- [x] `RefundTopUpCumulativeWithReference` 通过差额退款时保持幂等。
- [x] `RefundTopUpByRemainingRefundAmountWithReference` 通过累计退款转换后保持幂等。
- [x] Stripe payment intent 退款链路覆盖邀请奖励扣回。
- [x] Waffo / Waffo Pancake 退款回调覆盖邀请奖励扣回。

### 6.2 扣回计算

- [x] 退款比例：`actualRefundMoney / topUp.Money`。
- [x] 本次应扣：`floor(reward_quota * 累计退款比例) - reversed_quota`。
- [x] 部分退款时更新为 `partial_reversed` 或减少 pending。
- [x] 全额退款时更新为 `reversed` 或 `cancelled`。
- [x] 重复退款回调不能重复扣。

### 6.3 不同状态处理

- [x] invitee 已到账奖励：从被邀请用户 `Quota` 扣回。
- [x] inviter pending 奖励：减少待结算额度或取消。
- [x] inviter settled 奖励：优先从 `AffQuota` 扣回。
- [x] `AffQuota` 不足时从 `Quota` 扣回或记录欠扣。
- [x] cancelled/reversed 流水跳过。
- [x] 余额不足时记录告警和欠扣，不让支付平台退款状态和站内状态长期不一致。

---

## 7. 风控能力

### 7.1 P0 同步硬规则

- [x] 自邀请：取消奖励。
- [x] 同一订单重复奖励：唯一索引幂等。
- [x] 无邀请关系：跳过。
- [x] 首单金额 `< 30`：跳过。
- [x] 订单非 success：跳过。
- [x] `BaseQuota <= 0`：跳过。
- [x] 支付合规未确认：跳过。
- [x] 邀请者或被邀请者 disabled/deleted：取消奖励。
- [x] 退款时按比例取消或扣回。

### 7.2 P1 风险冻结规则

- [x] 同 IP 24 小时注册超过阈值：邀请者奖励冻结。
- [x] 同设备指纹 7 天内绑定多个被邀请账号：冻结。
- [x] 同支付账户绑定多个被邀请账号首充：冻结。
- [x] 邀请者 24 小时产生首充奖励超过阈值：新奖励冻结。
- [x] 邀请者 30 天退款率超过阈值：后续奖励冻结。
- [x] 注册到首充间隔过短：冻结。
- [x] 首充后短时间退款：取消或扣回。

### 7.3 人工审核

- [x] 管理员可以把 review/blocked 改为 approved。
- [x] 管理员可以冻结 pending 奖励。
- [x] 管理员可以取消 pending 奖励。
- [x] 超级管理员可以对 settled 奖励人工扣回。
- [x] 所有人工操作写系统日志。
- [x] 记录操作人、IP、原因、前后状态、奖励 ID。

---

## 8. 后端 API

### 8.1 用户侧 API

- [x] `GET /api/user/referral/summary`：返回邀请码、邀请链接、邀请数、有效首充数、待结算、已到账、已扣回。
- [x] `GET /api/user/referral/rewards`：查询用户自己的邀请奖励记录。
- [x] `GET /api/user/referral/activity`：返回当前邀请首充活动可见配置。

### 8.2 管理侧 API

- [x] `GET /api/admin/referral/rewards`：奖励流水列表。
- [x] `POST /api/admin/referral/rewards/:id/approve`：审核通过。
- [x] `POST /api/admin/referral/rewards/:id/block`：冻结。
- [x] `POST /api/admin/referral/rewards/:id/cancel`：取消。
- [x] `POST /api/admin/referral/rewards/:id/reverse`：人工扣回。
- [x] `GET /api/admin/referral/stats`：MVP 统计汇总。
- [x] `GET /api/admin/referral/stats/summary`：核心统计卡片。
- [x] `GET /api/admin/referral/stats/funnel`：转化漏斗。
- [x] `GET /api/admin/referral/stats/trend`：趋势图。
- [x] `GET /api/admin/referral/stats/top-inviters`：邀请者排行榜。
- [x] `GET /api/admin/referral/risk-rewards`：风控队列。

---

## 9. 管理后台邀请统计页面

### 9.1 路由与侧边栏

- [x] 新增路由：`web/default/src/routes/_authenticated/referrals/index.tsx`。
- [x] 新增功能目录：`web/default/src/features/referrals/`。
- [x] `web/default/src/hooks/use-sidebar-data.ts` Admin 导航新增 `Referral Stats`。
- [x] 侧边栏路径使用 `/referrals`。
- [x] 图标使用 `Handshake`，若不可用则用 `Share2` 或 `UsersRound`。
- [x] 放在 `Users` 后、`Redemption Codes` 前。
- [x] `web/default/src/hooks/use-sidebar-config.ts` 新增 `admin.referral=true`。
- [x] `URL_TO_CONFIG_MAP` 新增 `/referrals -> admin.referral`。
- [x] 后端默认 SidebarModulesAdmin 配置补充 `referral=true`。
- [x] 用户自定义 sidebar_modules 解析兼容新增模块。

### 9.2 页面筛选区

- [x] 时间范围：今天、昨天、近 7 天、近 30 天、本月、上月、自定义。
- [x] 活动 ID 筛选。
- [x] 邀请者 ID / 用户名搜索。
- [x] 被邀请用户 ID / 用户名搜索。
- [x] 支付渠道筛选。
- [x] 奖励状态筛选。
- [x] 风险状态筛选。
- [x] 用户组筛选。
- [x] 是否退款筛选。
- [x] 默认近 30 天。

### 9.3 核心统计卡片

- [x] 邀请注册数。
- [x] 达标首充人数。
- [x] 邀请首充实付金额。
- [x] 已送奖励。
- [x] 待结算奖励。
- [x] 已扣回奖励。
- [x] 首充转化率。
- [x] 奖励成本率。
- [x] ROI。
- [x] 退款金额。
- [x] 退款率。
- [x] 风控冻结数。
- [x] “已送奖励” tooltip 说明：不含 pending，不含已扣回部分。

### 9.4 图表区

- [x] 邀请转化漏斗：注册绑定 -> 首次充值 -> 首充达标 >=30 -> 奖励生成 -> 邀请者结算。
- [x] 趋势图：邀请首充净实付金额。
- [x] 趋势图：奖励成本。
- [x] 趋势图：达标首充人数。
- [x] 趋势图：退款金额。
- [x] 支持按日/周/月聚合。

### 9.5 邀请者排行榜

- [x] 邀请者 ID / 用户名。
- [x] 邀请注册数。
- [x] 达标首充人数。
- [x] 首充净实付金额。
- [x] 已结算奖励。
- [x] 待结算奖励。
- [x] 被邀请人奖励。
- [x] 退款金额 / 退款率。
- [x] ROI。
- [x] 风险状态。
- [x] 操作：查看明细、冻结待结算、导出。
- [x] 默认排序：首充净实付金额 desc。
- [x] 支持按已结算奖励、ROI、退款率排序。

### 9.6 奖励流水与风控队列

- [x] 页面下半部分增加 Tab：奖励流水。
- [x] 页面下半部分增加 Tab：风控队列。
- [x] 奖励流水列：奖励 ID、活动 ID、角色、邀请者、被邀请用户、订单号、实付金额、基础额度、奖励比例。
- [x] 奖励流水列：原始奖励、已结算、已扣回、业务状态、风控状态、预计结算时间、创建时间、操作。
- [x] 风控队列列：风险等级、命中规则、同 IP 注册数、同设备注册数、同支付账户首充数、退款率、冻结金额、处理人、处理备注。
- [x] 移动端提供卡片列表布局。

### 9.7 前端 i18n

- [x] `web/default/src/i18n/static-keys.ts` 添加页面标题和所有新文案。
- [x] `web/default/src/i18n/locales/en.json` 补齐。
- [x] `web/default/src/i18n/locales/zh.json` 补齐。
- [x] `web/default/src/i18n/locales/fr.json` 补齐。
- [x] `web/default/src/i18n/locales/ja.json` 补齐。
- [x] `web/default/src/i18n/locales/ru.json` 补齐。
- [x] `web/default/src/i18n/locales/vi.json` 补齐。
- [x] 执行 `node scripts/sync-i18n.mjs` 或项目可用的 i18n 同步命令。

---

## 10. 用户侧页面改造

### 10.1 邀请页

- [x] 展示我的邀请码。
- [x] 展示邀请链接。
- [x] 展示总邀请人数。
- [x] 展示有效首充人数。
- [x] 展示待结算奖励。
- [x] 展示已到账奖励。
- [x] 展示已扣回奖励。
- [x] 展示活动规则说明：首单实付金额 `>= 30`。
- [x] 复制邀请链接后 toast 提示。

### 10.2 充值页

- [x] 当前用户有邀请者且未完成首笔成功钱包充值时展示活动提示。
- [x] 选择金额 `< 30` 时提示未达到邀请奖励门槛。
- [x] 选择金额 `>= 30` 时提示预计可获得 10% 奖励。
- [x] 充值成功后展示邀请首充奖励到账提示。

### 10.3 钱包流水

- [x] 增加流水类型：`referral_first_topup_invitee_reward`。
- [x] 增加流水类型：`referral_first_topup_inviter_reward_pending`。
- [x] 增加流水类型：`referral_first_topup_inviter_reward_settled`。
- [x] 增加流水类型：`referral_first_topup_reward_reversed`。
- [x] 前端日志格式化支持这些流水类型。

---

## 11. 统计口径

- [x] 邀请注册数：`referral_invites` 绑定人数。
- [x] 首充人数：绑定邀请关系后完成第一笔成功钱包充值的人数。
- [x] 达标首充人数：首笔成功钱包充值 `Money >= 30` 的人数。
- [x] 邀请首充净实付金额：达标首充订单 `Money - RefundAmount`。
- [x] 已送奖励：invitee 已到账 + inviter 已结算 - 已扣回。
- [x] 待结算奖励：pending 且未取消的 inviter 奖励。
- [x] 已扣回奖励：reversed + partial_reversed 的累计扣回额度。
- [x] 奖励成本率：总奖励额度 / 邀请首充基础额度。
- [x] ROI：邀请首充净实付金额 / 总奖励成本。
- [x] 退款率：退款订单数 / 达标首充订单数。
- [x] 风控冻结数：risk_status 为 review 或 blocked 的奖励数。

---

## 12. 测试清单

### 12.1 后端单元测试

- [x] 被邀请用户首单实付 30：生成两条奖励。
- [x] 被邀请用户首单实付 29.99：不生成奖励。
- [x] 被邀请用户首单实付 50：invitee settled，inviter pending。
- [x] 第一笔成功充值 20，第二笔成功充值 50：严格首单不触发。
- [x] 无 inviter_id 用户充值：不生成奖励。
- [x] 邀请者账号 disabled：奖励 cancelled。
- [x] BaseQuota 为 0：不生成奖励。
- [x] 重复支付回调：奖励只生成一次。
- [x] 并发回调：唯一索引保证不重复。
- [x] 全站预算耗尽：新奖励 cancelled。
- [x] 邀请者月度上限：超出部分不结算或裁剪。

### 12.2 退款测试

- [x] invitee settled 后部分退款：按比例扣回。
- [x] invitee settled 后全额退款：全部扣回。
- [x] inviter pending 时退款：减少或取消待结算奖励。
- [x] inviter settled 后退款：优先从 AffQuota 扣回。
- [x] 重复退款回调：不重复扣回。
- [x] 累计退款：只扣新增差额。
- [x] 余额不足：记录欠扣和告警。

### 12.3 支付渠道集成测试

- [x] Epay 成功回调触发邀请奖励。
- [x] Stripe 成功回调触发邀请奖励。
- [x] Creem 成功回调触发邀请奖励。
- [x] Waffo 成功回调触发邀请奖励。
- [x] Alipay F2F 成功查询/回调触发邀请奖励。
- [x] Waffo Pancake 钱包充值成功触发邀请奖励。
- [x] 管理员补单触发邀请奖励。
- [x] 订阅订单成功不触发邀请奖励。

### 12.4 前端测试

- [x] 邀请链接进入注册页后保存 aff。
- [x] aff 超过 7 天自动失效。
- [x] 注册成功后清除 aff。
- [x] OAuth state 携带有效 aff。
- [x] 充值页根据 `>= 30` 展示奖励提示。
- [x] 管理员侧边栏展示邀请统计入口。
- [x] 邀请统计页面筛选项正常工作。
- [x] 邀请统计卡片数字格式正确。
- [x] 邀请者排行榜排序正常。
- [x] 奖励流水和风控队列分页正常。
- [x] 六语言无缺失 key。

---

## 13. 上线与回滚

### 13.1 上线前

- [x] 新配置默认关闭。
- [x] 数据库迁移在 SQLite/MySQL/PostgreSQL 通过。
- [x] 历史 `users.inviter_id` 回填脚本在测试库演练。
- [x] 奖励结算任务默认可关闭。
- [x] 管理端页面仅管理员可见。
- [x] 后端 API 权限校验完成。
- [x] 日志与告警完成。

### 13.2 灰度

- [ ] 测试环境开启活动。
- [ ] 小用户组灰度。
- [ ] 观察成功回调是否重复生成奖励。
- [ ] 观察退款扣回是否正确。
- [ ] 观察 pending 结算任务是否幂等。
- [ ] 观察管理员统计页金额是否与流水一致。

### 13.3 回滚

- [x] 关闭 `ReferralFirstTopUpReward.Enabled`。
- [x] 停止邀请者延迟结算任务。
- [x] 保留 `referral_rewards` 和 `referral_invites` 数据。
- [x] pending 奖励可批量取消。
- [x] 已结算奖励按运营策略保留或人工扣回。
- [x] 管理员页面可保留只读，用于历史审计。

---

## 14. 建议开发顺序

1. [x] 数据模型和迁移。
2. [x] 配置结构和默认值。
3. [x] 注册/OAuth 邀请关系审计。
4. [x] 统一充值成功奖励判断。
5. [x] 被邀请用户即时到账。
6. [x] 邀请者 pending 流水。
7. [x] 延迟结算任务。
8. [x] 退款扣回。
9. [x] 管理侧 rewards API。
10. [x] 管理侧 stats API。
11. [x] 管理员邀请统计页面与侧边栏入口。
12. [ ] 用户邀请页和充值页提示。
13. [x] i18n 六语言。
14. [ ] 单元测试与集成测试。
15. [ ] 灰度上线与监控。

---

## 15. MVP 完成标准

- [x] 实付金额 `>= 30` 的被邀请用户首笔钱包充值能生成双方奖励。
- [x] 30.00 金额明确触发奖励。
- [x] 29.99 金额不触发奖励。
- [x] 被邀请用户奖励即时进入 `Quota`。
- [x] 邀请者奖励进入 `pending`，到期后进入 `AffQuota`。
- [x] 重复回调和并发回调不会重复发奖。
- [x] 退款能按比例取消或扣回奖励。
- [x] 管理员侧边栏有“邀请统计”入口。
- [x] 管理员能看到“邀请送了多少钱”、待结算奖励、首充收入、ROI、退款率。
- [x] 管理员能查看奖励流水和风控队列。
- [x] 配置默认关闭，不影响现有充值和邀请流程。
- [x] 核心测试通过。
