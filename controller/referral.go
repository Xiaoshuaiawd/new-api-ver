package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type referralRewardActionRequest struct {
	Reason string `json:"reason"`
}

func parseReferralStatsFilter(c *gin.Context) model.ReferralStatsFilter {
	filter := model.ReferralStatsFilter{
		ActivityID:      strings.TrimSpace(c.Query("activity_id")),
		InviterKeyword:  strings.TrimSpace(c.Query("inviter_keyword")),
		InviteeKeyword:  strings.TrimSpace(c.Query("invitee_keyword")),
		PaymentProvider: strings.TrimSpace(c.Query("payment_provider")),
		Status:          strings.TrimSpace(c.Query("status")),
		RiskStatus:      strings.TrimSpace(c.Query("risk_status")),
		UserGroup:       strings.TrimSpace(c.Query("user_group")),
		Bucket:          strings.TrimSpace(c.Query("bucket")),
		Sort:            strings.TrimSpace(c.Query("sort")),
	}
	filter.StartTime, _ = strconv.ParseInt(c.Query("start_time"), 10, 64)
	filter.EndTime, _ = strconv.ParseInt(c.Query("end_time"), 10, 64)
	filter.InviterId, _ = strconv.Atoi(c.Query("inviter_id"))
	filter.InviteeId, _ = strconv.Atoi(c.Query("invitee_id"))
	filter.RefundOnly = c.Query("refund_only") == "true" || c.Query("refund_only") == "1"
	return filter
}

func parseReferralRewardQuery(c *gin.Context) model.ReferralRewardQuery {
	query := model.ReferralRewardQuery{
		Keyword:         strings.TrimSpace(c.Query("keyword")),
		InviterKeyword:  strings.TrimSpace(c.Query("inviter_keyword")),
		InviteeKeyword:  strings.TrimSpace(c.Query("invitee_keyword")),
		RewardRole:      strings.TrimSpace(c.Query("reward_role")),
		Status:          strings.TrimSpace(c.Query("status")),
		RiskStatus:      strings.TrimSpace(c.Query("risk_status")),
		PaymentProvider: strings.TrimSpace(c.Query("payment_provider")),
		UserGroup:       strings.TrimSpace(c.Query("user_group")),
	}
	query.StartTime, _ = strconv.ParseInt(c.Query("start_time"), 10, 64)
	query.EndTime, _ = strconv.ParseInt(c.Query("end_time"), 10, 64)
	query.InviterId, _ = strconv.Atoi(c.Query("inviter_id"))
	query.InviteeId, _ = strconv.Atoi(c.Query("invitee_id"))
	query.RefundOnly = c.Query("refund_only") == "true" || c.Query("refund_only") == "1"
	return query
}

func GetReferralActivity(c *gin.Context) {
	cfg := operation_setting.GetPaymentSetting().ReferralFirstTopUpReward.Normalized()
	common.ApiSuccess(c, cfg)
}

func GetReferralSummary(c *gin.Context) {
	data, err := model.GetReferralSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}

func GetReferralSelfRewards(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userID := c.GetInt("id")
	query := parseReferralRewardQuery(c)
	if strings.TrimSpace(c.Query("as")) == model.ReferralRewardRoleInvitee {
		query.InviteeId = userID
	} else {
		query.InviterId = userID
	}
	rewards, total, err := model.SearchReferralRewards(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetReferralRewards(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rewards, total, err := model.SearchReferralRewards(parseReferralRewardQuery(c), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetReferralRiskRewards(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := parseReferralRewardQuery(c)
	if query.RiskStatus == "" {
		query.RiskStatus = strings.Join([]string{
			model.ReferralRewardRiskReview,
			model.ReferralRewardRiskBlocked,
		}, ",")
	}
	rewards, total, err := model.SearchReferralRewards(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rewards)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetReferralStats(c *gin.Context) {
	AdminGetReferralStatsSummary(c)
}

func AdminGetReferralStatsSummary(c *gin.Context) {
	summary, err := model.GetReferralStatsSummary(parseReferralStatsFilter(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func AdminGetReferralStatsFunnel(c *gin.Context) {
	items, err := model.GetReferralFunnel(parseReferralStatsFilter(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func AdminGetReferralStatsTrend(c *gin.Context) {
	items, err := model.GetReferralTrend(parseReferralStatsFilter(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func AdminGetReferralTopInviters(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := model.GetReferralTopInviters(parseReferralStatsFilter(c), limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func AdminApproveReferralReward(c *gin.Context) {
	updateReferralRewardRiskStatus(c, model.ReferralRewardRiskApproved)
}

func AdminBlockReferralReward(c *gin.Context) {
	updateReferralRewardRiskStatus(c, model.ReferralRewardRiskBlocked)
}

func updateReferralRewardRiskStatus(c *gin.Context, status string) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req referralRewardActionRequest
	_ = c.ShouldBindJSON(&req)
	before := getReferralRewardAuditSnapshot(id)
	if err := model.UpdateReferralRewardRiskStatus(id, status, req.Reason); err != nil {
		common.ApiError(c, err)
		return
	}
	after := getReferralRewardAuditSnapshot(id)
	recordReferralAdminAudit(c, "referral.reward_risk_update", id, req.Reason, before, after)
	common.ApiSuccess(c, nil)
}

func AdminCancelReferralReward(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req referralRewardActionRequest
	_ = c.ShouldBindJSON(&req)
	before := getReferralRewardAuditSnapshot(id)
	if err := model.CancelReferralReward(id, req.Reason); err != nil {
		common.ApiError(c, err)
		return
	}
	after := getReferralRewardAuditSnapshot(id)
	recordReferralAdminAudit(c, "referral.reward_cancel", id, req.Reason, before, after)
	common.ApiSuccess(c, nil)
}

func AdminReverseReferralReward(c *gin.Context) {
	if c.GetInt("role") < common.RoleRootUser {
		common.ApiErrorMsg(c, "仅超级管理员可以扣回已结算邀请奖励")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req referralRewardActionRequest
	_ = c.ShouldBindJSON(&req)
	before := getReferralRewardAuditSnapshot(id)
	if err := model.ReverseReferralReward(id, req.Reason); err != nil {
		common.ApiError(c, err)
		return
	}
	after := getReferralRewardAuditSnapshot(id)
	recordReferralAdminAudit(c, "referral.reward_reverse", id, req.Reason, before, after)
	common.ApiSuccess(c, nil)
}

func AdminBlockInviterPendingReferralRewards(c *gin.Context) {
	inviterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req referralRewardActionRequest
	_ = c.ShouldBindJSON(&req)
	count, err := model.BlockInviterPendingReferralRewards(inviterID, req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOperationAuditLog(
		c.GetInt("id"),
		"Blocked inviter pending referral rewards",
		c.ClientIP(),
		"referral.inviter_pending_block",
		map[string]interface{}{
			"inviter_id": inviterID,
			"count":      count,
			"reason":     strings.TrimSpace(req.Reason),
		},
		map[string]interface{}{
			"operator_id": c.GetInt("id"),
			"ip":          c.ClientIP(),
		},
		nil,
	)
	common.ApiSuccess(c, nil)
}

func getReferralRewardAuditSnapshot(id int) map[string]interface{} {
	var reward model.ReferralReward
	if err := model.DB.Select("id", "status", "risk_status", "risk_reason", "reward_quota", "settled_quota", "reversed_quota", "owed_quota").First(&reward, "id = ?", id).Error; err != nil {
		return nil
	}
	return map[string]interface{}{
		"id":             reward.Id,
		"status":         reward.Status,
		"risk_status":    reward.RiskStatus,
		"risk_reason":    reward.RiskReason,
		"reward_quota":   reward.RewardQuota,
		"settled_quota":  reward.SettledQuota,
		"reversed_quota": reward.ReversedQuota,
		"owed_quota":     reward.OwedQuota,
	}
}

func recordReferralAdminAudit(c *gin.Context, action string, rewardID int, reason string, before map[string]interface{}, after map[string]interface{}) {
	model.RecordOperationAuditLog(
		c.GetInt("id"),
		"Updated referral reward",
		c.ClientIP(),
		action,
		map[string]interface{}{
			"reward_id": rewardID,
			"reason":    strings.TrimSpace(reason),
		},
		map[string]interface{}{
			"operator_id": c.GetInt("id"),
			"ip":          c.ClientIP(),
			"reward_id":   rewardID,
			"reason":      strings.TrimSpace(reason),
			"before":      before,
			"after":       after,
		},
		nil,
	)
}
