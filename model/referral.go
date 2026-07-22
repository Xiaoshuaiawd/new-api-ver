package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ReferralInviteSourceRegister       = "register"
	ReferralInviteSourceOAuth          = "oauth"
	ReferralInviteSourceLegacyBackfill = "legacy_backfill"

	ReferralRewardRoleInvitee = "invitee"
	ReferralRewardRoleInviter = "inviter"

	ReferralRewardStatusPending         = "pending"
	ReferralRewardStatusSettled         = "settled"
	ReferralRewardStatusCancelled       = "cancelled"
	ReferralRewardStatusReversed        = "reversed"
	ReferralRewardStatusPartialReversed = "partial_reversed"

	ReferralRewardRiskNormal   = "normal"
	ReferralRewardRiskReview   = "review"
	ReferralRewardRiskBlocked  = "blocked"
	ReferralRewardRiskApproved = "approved"
	ReferralRewardRiskRejected = "rejected"

	ReferralFirstTopUpModeStrictFirst    = "strict_first"
	ReferralFirstTopUpModeFirstQualified = "first_qualified"

	ReferralThresholdOperatorGTE = "gte"
	ReferralThresholdOperatorGT  = "gt"
)

var (
	ErrReferralRewardNotFound      = errors.New("referral reward not found")
	ErrReferralRewardStatusInvalid = errors.New("referral reward status invalid")
)

type ReferralInvite struct {
	Id                int    `json:"id"`
	InviterId         int    `json:"inviter_id" gorm:"index"`
	InviteeId         int    `json:"invitee_id" gorm:"uniqueIndex"`
	AffCode           string `json:"aff_code" gorm:"type:varchar(32);index"`
	Source            string `json:"source" gorm:"type:varchar(32);default:''"`
	BindIp            string `json:"bind_ip" gorm:"type:varchar(64);default:''"`
	UserAgentHash     string `json:"user_agent_hash" gorm:"type:varchar(128);default:''"`
	DeviceFingerprint string `json:"device_fingerprint" gorm:"type:varchar(128);default:''"`
	RiskFlags         string `json:"risk_flags" gorm:"type:text"`
	CreatedAt         int64  `json:"created_at" gorm:"index"`
	UpdatedAt         int64  `json:"updated_at"`
}

func (invite *ReferralInvite) SetRiskFlags(flags []string) error {
	normalized := make([]string, 0, len(flags))
	seen := map[string]struct{}{}
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}
		if _, ok := seen[flag]; ok {
			continue
		}
		seen[flag] = struct{}{}
		normalized = append(normalized, flag)
	}
	data, err := common.Marshal(normalized)
	if err != nil {
		return err
	}
	invite.RiskFlags = string(data)
	return nil
}

func (invite ReferralInvite) GetRiskFlags() ([]string, error) {
	if strings.TrimSpace(invite.RiskFlags) == "" {
		return nil, nil
	}
	var flags []string
	if err := common.Unmarshal([]byte(invite.RiskFlags), &flags); err != nil {
		return nil, err
	}
	return flags, nil
}

type ReferralReward struct {
	Id                 int     `json:"id"`
	ActivityID         string  `json:"activity_id" gorm:"type:varchar(128);default:''"`
	ActivityName       string  `json:"activity_name" gorm:"type:varchar(255);default:''"`
	RewardRole         string  `json:"reward_role" gorm:"type:varchar(32);uniqueIndex:idx_referral_reward_topup_role"`
	InviterId          int     `json:"inviter_id" gorm:"index:idx_referral_reward_inviter_status,priority:1"`
	InviteeId          int     `json:"invitee_id" gorm:"index:idx_referral_reward_invitee_status,priority:1"`
	TopUpId            int     `json:"topup_id" gorm:"uniqueIndex:idx_referral_reward_topup_role"`
	TradeNo            string  `json:"trade_no" gorm:"type:varchar(255);index"`
	PaymentProvider    string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	PaymentAccountHash string  `json:"payment_account_hash" gorm:"type:varchar(128);default:'';index"`
	PaidMoney          float64 `json:"paid_money" gorm:"default:0"`
	BaseQuota          int     `json:"base_quota" gorm:"default:0"`
	RewardPercent      float64 `json:"reward_percent" gorm:"default:0"`
	RewardQuota        int     `json:"reward_quota" gorm:"default:0"`
	SettledQuota       int     `json:"settled_quota" gorm:"default:0"`
	ReversedQuota      int     `json:"reversed_quota" gorm:"default:0"`
	OwedQuota          int     `json:"owed_quota" gorm:"default:0"`
	RefundAmount       float64 `json:"refund_amount" gorm:"default:0"`
	Status             string  `json:"status" gorm:"type:varchar(32);index:idx_referral_reward_status_risk_settle,priority:1;index:idx_referral_reward_inviter_status,priority:2;index:idx_referral_reward_invitee_status,priority:2"`
	RiskStatus         string  `json:"risk_status" gorm:"type:varchar(32);index:idx_referral_reward_status_risk_settle,priority:2"`
	RiskReason         string  `json:"risk_reason" gorm:"type:varchar(255);default:''"`
	RiskSnapshot       string  `json:"risk_snapshot" gorm:"type:text"`
	SettleAt           int64   `json:"settle_at" gorm:"index:idx_referral_reward_status_risk_settle,priority:3"`
	SettledAt          int64   `json:"settled_at" gorm:"default:0"`
	CancelledAt        int64   `json:"cancelled_at" gorm:"default:0"`
	ReversedAt         int64   `json:"reversed_at" gorm:"default:0"`
	CreatedAt          int64   `json:"created_at" gorm:"index"`
	UpdatedAt          int64   `json:"updated_at"`
}

type ReferralRewardQuery struct {
	Keyword         string
	InviterId       int
	InviteeId       int
	InviterKeyword  string
	InviteeKeyword  string
	RewardRole      string
	Status          string
	RiskStatus      string
	PaymentProvider string
	UserGroup       string
	RefundOnly      bool
	StartTime       int64
	EndTime         int64
}

type ReferralStatsFilter struct {
	StartTime       int64
	EndTime         int64
	ActivityID      string
	InviterId       int
	InviteeId       int
	InviterKeyword  string
	InviteeKeyword  string
	PaymentProvider string
	Status          string
	RiskStatus      string
	UserGroup       string
	RefundOnly      bool
	Bucket          string
	Sort            string
}

type ReferralStatsSummary struct {
	InviteRegisteredCount       int64   `json:"invite_registered_count"`
	FirstTopUpCount             int64   `json:"first_topup_count"`
	QualifiedFirstTopUpCount    int64   `json:"qualified_first_topup_count"`
	QualifiedFirstTopUpNetMoney float64 `json:"qualified_first_topup_net_money"`
	InviteeSettledRewardQuota   int     `json:"invitee_settled_reward_quota"`
	InviterSettledRewardQuota   int     `json:"inviter_settled_reward_quota"`
	PendingRewardQuota          int     `json:"pending_reward_quota"`
	ReversedRewardQuota         int     `json:"reversed_reward_quota"`
	RefundMoney                 float64 `json:"refund_money"`
	RefundRate                  float64 `json:"refund_rate"`
	ConversionRate              float64 `json:"conversion_rate"`
	RewardCostRate              float64 `json:"reward_cost_rate"`
	ROI                         float64 `json:"roi"`
	BlockedRewardCount          int64   `json:"blocked_reward_count"`
}

type ReferralTopInviterStat struct {
	InviterId                 int     `json:"inviter_id"`
	InviterUsername           string  `json:"inviter_username"`
	InviteRegisteredCount     int64   `json:"invite_registered_count"`
	QualifiedFirstTopUpCount  int64   `json:"qualified_first_topup_count"`
	FirstTopUpNetMoney        float64 `json:"first_topup_net_money"`
	InviterSettledRewardQuota int     `json:"inviter_settled_reward_quota"`
	PendingRewardQuota        int     `json:"pending_reward_quota"`
	InviteeRewardQuota        int     `json:"invitee_reward_quota"`
	RefundMoney               float64 `json:"refund_money"`
	RefundRate                float64 `json:"refund_rate"`
	ROI                       float64 `json:"roi"`
	RiskStatus                string  `json:"risk_status"`
}

type ReferralFunnelItem struct {
	Stage     string  `json:"stage"`
	Count     int64   `json:"count"`
	Rate      float64 `json:"rate"`
	PriorRate float64 `json:"prior_rate"`
}

type ReferralTrendItem struct {
	Bucket                   string  `json:"bucket"`
	NetMoney                 float64 `json:"net_money"`
	RewardCostQuota          int     `json:"reward_cost_quota"`
	QualifiedFirstTopUpCount int64   `json:"qualified_first_topup_count"`
	RefundMoney              float64 `json:"refund_money"`
}

type ReferralRiskSnapshot struct {
	Flags                         []string `json:"flags,omitempty"`
	SameIP24hRegisterCount        int64    `json:"same_ip_24h_register_count,omitempty"`
	SameDevice7dBindCount         int64    `json:"same_device_7d_bind_count,omitempty"`
	SamePaymentAccountInviteCount int64    `json:"same_payment_account_invitee_count,omitempty"`
	Inviter24hRewardQuota         int      `json:"inviter_24h_reward_quota,omitempty"`
	Inviter30dRefundRate          float64  `json:"inviter_30d_refund_rate,omitempty"`
	RegisterToTopUpSeconds        int64    `json:"register_to_topup_seconds,omitempty"`
	PaymentAccountHash            string   `json:"payment_account_hash,omitempty"`
	OwedQuota                     int      `json:"owed_quota,omitempty"`
}

type ReferralFirstTopUpState struct {
	HasInviter               bool `json:"has_inviter"`
	FirstWalletTopUpDone     bool `json:"first_wallet_topup_done"`
	QualifiedRewardGenerated bool `json:"qualified_reward_generated"`
}

func CreateReferralInviteIfNeeded(inviterId int, inviteeId int, affCode string, source string, bindIP string, userAgent string, deviceFingerprint string) error {
	if inviterId <= 0 || inviteeId <= 0 || inviterId == inviteeId {
		return nil
	}
	now := common.GetTimestamp()
	invite := ReferralInvite{
		InviterId:         inviterId,
		InviteeId:         inviteeId,
		AffCode:           strings.TrimSpace(affCode),
		Source:            strings.TrimSpace(source),
		BindIp:            strings.TrimSpace(bindIP),
		UserAgentHash:     hashReferralUserAgent(userAgent),
		DeviceFingerprint: hashReferralUserAgent(deviceFingerprint),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	var existing ReferralInvite
	err := DB.Where("invitee_id = ?", inviteeId).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Create(&invite).Error
}

func BackfillReferralInvitesFromUsers() error {
	if !DB.Migrator().HasTable(&ReferralInvite{}) || !DB.Migrator().HasTable(&User{}) {
		return nil
	}
	var users []User
	return DB.Select("id", "inviter_id", "created_at").
		Where("inviter_id > 0").
		FindInBatches(&users, 500, func(tx *gorm.DB, _ int) error {
			for i := range users {
				if users[i].InviterId <= 0 || users[i].InviterId == users[i].Id {
					continue
				}
				var count int64
				if err := tx.Model(&ReferralInvite{}).Where("invitee_id = ?", users[i].Id).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					continue
				}
				affCode := ""
				var inviter User
				if err := tx.Select("aff_code").First(&inviter, "id = ?", users[i].InviterId).Error; err == nil {
					affCode = inviter.AffCode
				}
				createdAt := users[i].CreatedAt
				if createdAt <= 0 {
					createdAt = common.GetTimestamp()
				}
				invite := ReferralInvite{
					InviterId: users[i].InviterId,
					InviteeId: users[i].Id,
					AffCode:   affCode,
					Source:    ReferralInviteSourceLegacyBackfill,
					CreatedAt: createdAt,
					UpdatedAt: common.GetTimestamp(),
				}
				if err := tx.Create(&invite).Error; err != nil {
					return err
				}
			}
			return nil
		}).Error
}

func hashReferralUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(userAgent))
	return fmt.Sprintf("%x", sum[:])
}

func referralJSON(value any) string {
	data, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func referralRewardRiskStatus(cfg operation_setting.ReferralFirstTopUpRewardSetting, flags []string) string {
	if len(flags) == 0 {
		return ReferralRewardRiskNormal
	}
	if cfg.AutoBlockRiskyRewards {
		return ReferralRewardRiskBlocked
	}
	return ReferralRewardRiskReview
}

func referralRiskReason(flags []string) string {
	if len(flags) == 0 {
		return ""
	}
	return strings.Join(flags, ",")
}

func referralPaymentAccountHash(topUp *TopUp, invitee *User) string {
	values := []string{}
	if topUp != nil {
		if strings.TrimSpace(topUp.PaymentIntentID) != "" {
			values = append(values, topUp.PaymentProvider, topUp.PaymentIntentID)
		}
	}
	if invitee != nil && strings.TrimSpace(invitee.StripeCustomer) != "" {
		values = append(values, PaymentProviderStripe, invitee.StripeCustomer)
	}
	raw := strings.TrimSpace(strings.Join(values, ":"))
	if raw == "" {
		return ""
	}
	return hashReferralUserAgent(raw)
}

func getReferralInviteForUserTx(tx *gorm.DB, userID int) (*ReferralInvite, error) {
	var invite ReferralInvite
	err := tx.Where("invitee_id = ?", userID).First(&invite).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &invite, nil
}

func evaluateReferralRewardRiskTx(tx *gorm.DB, topUp *TopUp, inviter *User, invitee *User, cfg operation_setting.ReferralFirstTopUpRewardSetting) (ReferralRiskSnapshot, error) {
	snapshot := ReferralRiskSnapshot{}
	if topUp == nil || inviter == nil || invitee == nil {
		return snapshot, nil
	}
	invite, err := getReferralInviteForUserTx(tx, invitee.Id)
	if err != nil {
		return snapshot, err
	}
	now := common.GetTimestamp()
	flags := make([]string, 0)
	if invite != nil {
		if cfg.RiskSameIP24hRegisterLimit > 0 && strings.TrimSpace(invite.BindIp) != "" {
			start := now - 24*60*60
			if err := tx.Model(&ReferralInvite{}).
				Where("inviter_id = ? AND bind_ip = ? AND created_at >= ?", inviter.Id, invite.BindIp, start).
				Count(&snapshot.SameIP24hRegisterCount).Error; err != nil {
				return snapshot, err
			}
			if int(snapshot.SameIP24hRegisterCount) > cfg.RiskSameIP24hRegisterLimit {
				flags = append(flags, "same_ip_24h_register_limit")
			}
		}
		if cfg.RiskSameDevice7dBindLimit > 0 && strings.TrimSpace(invite.DeviceFingerprint) != "" {
			start := now - 7*24*60*60
			if err := tx.Model(&ReferralInvite{}).
				Where("inviter_id = ? AND device_fingerprint = ? AND created_at >= ?", inviter.Id, invite.DeviceFingerprint, start).
				Count(&snapshot.SameDevice7dBindCount).Error; err != nil {
				return snapshot, err
			}
			if int(snapshot.SameDevice7dBindCount) > cfg.RiskSameDevice7dBindLimit {
				flags = append(flags, "same_device_7d_bind_limit")
			}
		}
		if cfg.RiskRegisterToTopUpMinSec > 0 && invite.CreatedAt > 0 {
			completeTime := topUp.CompleteTime
			if completeTime <= 0 {
				completeTime = now
			}
			snapshot.RegisterToTopUpSeconds = completeTime - invite.CreatedAt
			if snapshot.RegisterToTopUpSeconds >= 0 && snapshot.RegisterToTopUpSeconds < int64(cfg.RiskRegisterToTopUpMinSec) {
				flags = append(flags, "register_to_topup_too_short")
			}
		}
	}
	paymentAccountHash := referralPaymentAccountHash(topUp, invitee)
	snapshot.PaymentAccountHash = paymentAccountHash
	if cfg.RiskSamePaymentAccountLimit > 0 && paymentAccountHash != "" {
		if err := tx.Model(&ReferralReward{}).
			Where("inviter_id = ? AND reward_role = ? AND payment_account_hash = ?", inviter.Id, ReferralRewardRoleInviter, paymentAccountHash).
			Count(&snapshot.SamePaymentAccountInviteCount).Error; err != nil {
			return snapshot, err
		}
		snapshot.SamePaymentAccountInviteCount++
		if int(snapshot.SamePaymentAccountInviteCount) > cfg.RiskSamePaymentAccountLimit {
			flags = append(flags, "same_payment_account_limit")
		}
	}
	if cfg.RiskInviter24hRewardQuota > 0 {
		start := now - 24*60*60
		var rewards []ReferralReward
		if err := tx.Where("inviter_id = ? AND reward_role = ? AND created_at >= ?", inviter.Id, ReferralRewardRoleInviter, start).
			Where("status IN ?", []string{ReferralRewardStatusPending, ReferralRewardStatusSettled, ReferralRewardStatusPartialReversed}).
			Find(&rewards).Error; err != nil {
			return snapshot, err
		}
		for _, reward := range rewards {
			if net := reward.RewardQuota - reward.ReversedQuota; net > 0 {
				snapshot.Inviter24hRewardQuota += net
			}
		}
		if snapshot.Inviter24hRewardQuota > cfg.RiskInviter24hRewardQuota {
			flags = append(flags, "inviter_24h_reward_quota_limit")
		}
	}
	if cfg.RiskInviter30dRefundRate > 0 {
		start := now - 30*24*60*60
		var rows []ReferralReward
		if err := tx.Where("inviter_id = ? AND reward_role = ? AND created_at >= ?", inviter.Id, ReferralRewardRoleInviter, start).
			Find(&rows).Error; err != nil {
			return snapshot, err
		}
		orderIDs := map[int]struct{}{}
		refundIDs := map[int]struct{}{}
		for _, row := range rows {
			if row.RewardQuota <= 0 {
				continue
			}
			orderIDs[row.TopUpId] = struct{}{}
			if row.RefundAmount > 0 || row.ReversedQuota > 0 || row.Status == ReferralRewardStatusReversed || row.Status == ReferralRewardStatusPartialReversed {
				refundIDs[row.TopUpId] = struct{}{}
			}
		}
		if len(orderIDs) > 0 {
			snapshot.Inviter30dRefundRate = float64(len(refundIDs)) / float64(len(orderIDs))
			if snapshot.Inviter30dRefundRate > cfg.RiskInviter30dRefundRate {
				flags = append(flags, "inviter_30d_refund_rate_limit")
			}
		}
	}
	snapshot.Flags = flags
	if invite != nil && len(flags) > 0 {
		if err := invite.SetRiskFlags(flags); err == nil {
			_ = tx.Model(invite).Updates(map[string]interface{}{
				"risk_flags": invite.RiskFlags,
				"updated_at": now,
			}).Error
		}
	}
	return snapshot, nil
}

func recordReferralFirstTopUpFailureTx(tx *gorm.DB, inviteeID int, reason string) error {
	invite, err := getReferralInviteForUserTx(tx, inviteeID)
	if err != nil || invite == nil {
		return err
	}
	flags, err := invite.GetRiskFlags()
	if err != nil {
		flags = nil
	}
	flags = append(flags, reason)
	if err := invite.SetRiskFlags(flags); err != nil {
		return err
	}
	return tx.Model(invite).Updates(map[string]interface{}{
		"risk_flags": invite.RiskFlags,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func applyReferralFirstTopUpRewardTx(tx *gorm.DB, topUp *TopUp, settlement TopUpSettlement) error {
	cfg := operation_setting.GetPaymentSetting().ReferralFirstTopUpReward.Normalized()
	if !cfg.Enabled || !operation_setting.IsPaymentComplianceConfirmed() {
		return nil
	}
	if topUp == nil || topUp.Status != common.TopUpStatusSuccess || topUp.UserId <= 0 {
		return nil
	}
	if settlement.BaseQuota <= 0 || topUp.Money <= 0 {
		return nil
	}
	if isReferralExcludedProvider(cfg, topUp.PaymentProvider) {
		return nil
	}

	var invitee User
	if err := lockForUpdate(tx).First(&invitee, "id = ?", topUp.UserId).Error; err != nil {
		return err
	}
	if invitee.InviterId <= 0 || invitee.Status != common.UserStatusEnabled {
		return nil
	}
	if isReferralExcludedGroup(cfg, invitee.Group) {
		return nil
	}

	var inviter User
	if err := lockForUpdate(tx).First(&inviter, "id = ?", invitee.InviterId).Error; err != nil {
		return nil
	}
	if inviter.Status != common.UserStatusEnabled || inviter.Id == invitee.Id || isReferralExcludedGroup(cfg, inviter.Group) {
		return createCancelledReferralRewardsTx(tx, topUp, &inviter, &invitee, settlement, cfg, "invalid_inviter")
	}
	if !referralPaidMoneyPassesThreshold(topUp.Money, cfg.MinPaidMoney, cfg.ThresholdOperator) {
		if cfg.FirstTopUpMode == ReferralFirstTopUpModeStrictFirst {
			if ok, err := isReferralEligibleFirstTopUpTx(tx, topUp, cfg.FirstTopUpMode); err != nil || !ok {
				return err
			}
			return recordReferralFirstTopUpFailureTx(tx, invitee.Id, "below_threshold")
		}
		return nil
	}
	if ok, err := isReferralEligibleFirstTopUpTx(tx, topUp, cfg.FirstTopUpMode); err != nil || !ok {
		return err
	}
	riskSnapshot, err := evaluateReferralRewardRiskTx(tx, topUp, &inviter, &invitee, cfg)
	if err != nil {
		return err
	}

	inviteeQuota := referralRewardQuota(settlement.BaseQuota, cfg.InviteeRewardPercent, cfg.SingleInviteeRewardMaxQuota)
	inviterQuota := referralRewardQuota(settlement.BaseQuota, cfg.InviterRewardPercent, cfg.SingleInviterRewardMaxQuota)
	if inviteeQuota <= 0 && inviterQuota <= 0 {
		return nil
	}
	if cfg.TotalBudgetQuota > 0 {
		used, err := sumReferralNetRewardQuotaTx(tx, cfg.ActivityID)
		if err != nil {
			return err
		}
		remaining := cfg.TotalBudgetQuota - used
		if remaining <= 0 {
			return createCancelledReferralRewardsTx(tx, topUp, &inviter, &invitee, settlement, cfg, "budget_exhausted")
		}
		if inviteeQuota+inviterQuota > remaining {
			if inviteeQuota >= remaining {
				inviteeQuota = remaining
				inviterQuota = 0
			} else {
				inviterQuota = remaining - inviteeQuota
			}
		}
	}
	if cfg.InviterMonthlyMaxQuota > 0 && inviterQuota > 0 {
		used, err := sumInviterMonthlyReferralQuotaTx(tx, inviter.Id, common.GetTimestamp())
		if err != nil {
			return err
		}
		remaining := cfg.InviterMonthlyMaxQuota - used
		if remaining <= 0 {
			inviterQuota = 0
		} else if inviterQuota > remaining {
			inviterQuota = remaining
		}
	}
	if inviteeQuota > 0 {
		if err := createInviteeReferralRewardTx(tx, topUp, &inviter, &invitee, settlement, cfg, inviteeQuota, riskSnapshot); err != nil {
			return err
		}
	}
	if inviterQuota > 0 {
		if err := createInviterReferralRewardTx(tx, topUp, &inviter, &invitee, settlement, cfg, inviterQuota, riskSnapshot); err != nil {
			return err
		}
	}
	return nil
}

func isReferralExcludedProvider(cfg operation_setting.ReferralFirstTopUpRewardSetting, provider string) bool {
	provider = strings.TrimSpace(provider)
	for _, item := range cfg.ExcludedPaymentProviders {
		if strings.EqualFold(strings.TrimSpace(item), provider) {
			return true
		}
	}
	return false
}

func isReferralExcludedGroup(cfg operation_setting.ReferralFirstTopUpRewardSetting, group string) bool {
	group = strings.TrimSpace(group)
	for _, item := range cfg.ExcludedUserGroups {
		if strings.EqualFold(strings.TrimSpace(item), group) {
			return true
		}
	}
	return false
}

func referralPaidMoneyPassesThreshold(money float64, minMoney float64, operator string) bool {
	moneyDec := decimal.NewFromFloat(money)
	minDec := decimal.NewFromFloat(minMoney)
	if strings.EqualFold(operator, ReferralThresholdOperatorGT) {
		return moneyDec.GreaterThan(minDec)
	}
	return moneyDec.GreaterThanOrEqual(minDec)
}

func referralRewardQuota(baseQuota int, percent float64, maxQuota int) int {
	if baseQuota <= 0 || percent <= 0 {
		return 0
	}
	quota := int(decimal.NewFromInt(int64(baseQuota)).Mul(decimal.NewFromFloat(percent)).Div(decimal.NewFromInt(100)).IntPart())
	if maxQuota > 0 && quota > maxQuota {
		quota = maxQuota
	}
	if quota < 0 {
		return 0
	}
	return quota
}

func isReferralEligibleFirstTopUpTx(tx *gorm.DB, topUp *TopUp, mode string) (bool, error) {
	query := tx.Model(&TopUp{}).
		Where("user_id = ? AND status = ?", topUp.UserId, common.TopUpStatusSuccess).
		Where("base_quota > 0")
	if mode == ReferralFirstTopUpModeFirstQualified {
		query = query.Where("money >= ?", operation_setting.GetPaymentSetting().ReferralFirstTopUpReward.Normalized().MinPaidMoney)
	}
	var first TopUp
	if err := query.Order("complete_time asc, id asc").First(&first).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true, nil
		}
		return false, err
	}
	return first.Id == topUp.Id, nil
}

func baseReferralReward(topUp *TopUp, inviter *User, invitee *User, settlement TopUpSettlement, cfg operation_setting.ReferralFirstTopUpRewardSetting, role string, quota int) ReferralReward {
	now := common.GetTimestamp()
	status := ReferralRewardStatusPending
	settleAt := now + int64(cfg.InviterSettleDelayDays)*24*60*60
	if role == ReferralRewardRoleInvitee {
		status = ReferralRewardStatusSettled
		settleAt = now
	}
	return ReferralReward{
		ActivityID:         strings.TrimSpace(cfg.ActivityID),
		ActivityName:       strings.TrimSpace(cfg.ActivityName),
		RewardRole:         role,
		InviterId:          inviter.Id,
		InviteeId:          invitee.Id,
		TopUpId:            topUp.Id,
		TradeNo:            topUp.TradeNo,
		PaymentProvider:    topUp.PaymentProvider,
		PaymentAccountHash: referralPaymentAccountHash(topUp, invitee),
		PaidMoney:          topUp.Money,
		BaseQuota:          settlement.BaseQuota,
		RewardQuota:        quota,
		Status:             status,
		RiskStatus:         ReferralRewardRiskNormal,
		SettleAt:           settleAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func createInviteeReferralRewardTx(tx *gorm.DB, topUp *TopUp, inviter *User, invitee *User, settlement TopUpSettlement, cfg operation_setting.ReferralFirstTopUpRewardSetting, quota int, riskSnapshot ReferralRiskSnapshot) error {
	var count int64
	if err := tx.Model(&ReferralReward{}).Where("top_up_id = ? AND reward_role = ?", topUp.Id, ReferralRewardRoleInvitee).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	reward := baseReferralReward(topUp, inviter, invitee, settlement, cfg, ReferralRewardRoleInvitee, quota)
	reward.RewardPercent = cfg.InviteeRewardPercent
	reward.SettledQuota = quota
	reward.SettledAt = common.GetTimestamp()
	reward.RiskSnapshot = referralJSON(riskSnapshot)
	if err := tx.Create(&reward).Error; err != nil {
		return err
	}
	if err := tx.Model(&User{}).Where("id = ?", invitee.Id).Update("quota", gorm.Expr("quota + ?", quota)).Error; err != nil {
		return err
	}
	recordReferralRewardLogTx(tx, invitee.Id, "referral_first_topup_invitee_reward", quota, reward.Id, topUp.TradeNo)
	return nil
}

func createInviterReferralRewardTx(tx *gorm.DB, topUp *TopUp, inviter *User, invitee *User, settlement TopUpSettlement, cfg operation_setting.ReferralFirstTopUpRewardSetting, quota int, riskSnapshot ReferralRiskSnapshot) error {
	var count int64
	if err := tx.Model(&ReferralReward{}).Where("top_up_id = ? AND reward_role = ?", topUp.Id, ReferralRewardRoleInviter).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	reward := baseReferralReward(topUp, inviter, invitee, settlement, cfg, ReferralRewardRoleInviter, quota)
	reward.RewardPercent = cfg.InviterRewardPercent
	reward.RiskStatus = referralRewardRiskStatus(cfg, riskSnapshot.Flags)
	reward.RiskReason = referralRiskReason(riskSnapshot.Flags)
	reward.RiskSnapshot = referralJSON(riskSnapshot)
	if reward.RiskStatus == ReferralRewardRiskBlocked {
		reward.RiskReason = referralRiskReason(riskSnapshot.Flags)
	}
	if err := tx.Create(&reward).Error; err != nil {
		return err
	}
	recordReferralRewardLogTx(tx, inviter.Id, "referral_first_topup_inviter_reward_pending", quota, reward.Id, topUp.TradeNo)
	return nil
}

func createCancelledReferralRewardsTx(tx *gorm.DB, topUp *TopUp, inviter *User, invitee *User, settlement TopUpSettlement, cfg operation_setting.ReferralFirstTopUpRewardSetting, reason string) error {
	if inviter == nil || invitee == nil || topUp == nil {
		return nil
	}
	now := common.GetTimestamp()
	roles := []struct {
		role    string
		percent float64
	}{
		{ReferralRewardRoleInvitee, cfg.InviteeRewardPercent},
		{ReferralRewardRoleInviter, cfg.InviterRewardPercent},
	}
	for _, item := range roles {
		var count int64
		if err := tx.Model(&ReferralReward{}).Where("top_up_id = ? AND reward_role = ?", topUp.Id, item.role).Count(&count).Error; err != nil || count > 0 {
			if err != nil {
				return err
			}
			continue
		}
		reward := baseReferralReward(topUp, inviter, invitee, settlement, cfg, item.role, 0)
		reward.RewardPercent = item.percent
		reward.Status = ReferralRewardStatusCancelled
		reward.RiskStatus = ReferralRewardRiskRejected
		reward.RiskReason = reason
		reward.CancelledAt = now
		reward.RiskSnapshot = referralJSON(ReferralRiskSnapshot{Flags: []string{reason}})
		if err := tx.Create(&reward).Error; err != nil {
			return err
		}
	}
	return nil
}

func recordReferralRewardLogTx(tx *gorm.DB, userID int, action string, quota int, rewardID int, tradeNo string) {
	if userID <= 0 || quota <= 0 {
		return
	}
	username := ""
	if tx != nil {
		_ = tx.Model(&User{}).Where("id = ?", userID).Pluck("username", &username).Error
	}
	params := map[string]interface{}{
		"quota":     logger.LogQuota(quota),
		"reward_id": rewardID,
		"trade_no":  tradeNo,
	}
	content := fmt.Sprintf("%s %s", action, logger.LogQuota(quota))
	log := &Log{
		UserId:    userID,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeSystem,
		Content:   content,
		Quota:     quota,
		Other: common.MapToJsonStr(map[string]interface{}{
			"op": buildOpField(action, params),
		}),
	}
	db := tx
	if db == nil {
		db = LOG_DB
	}
	if err := db.Create(log).Error; err != nil {
		common.SysLog("failed to record referral reward log: " + err.Error())
	}
}

func sumReferralNetRewardQuotaTx(tx *gorm.DB, activityID string) (int, error) {
	query := tx.Model(&ReferralReward{}).Where("status <> ?", ReferralRewardStatusCancelled)
	if strings.TrimSpace(activityID) != "" {
		query = query.Where("activity_id = ?", strings.TrimSpace(activityID))
	}
	var rows []ReferralReward
	if err := query.Find(&rows).Error; err != nil {
		return 0, err
	}
	total := 0
	for _, row := range rows {
		net := row.RewardQuota - row.ReversedQuota
		if net > 0 {
			total += net
		}
	}
	return total, nil
}

func sumInviterMonthlyReferralQuotaTx(tx *gorm.DB, inviterID int, now int64) (int, error) {
	start := time.Unix(now, 0).Local()
	monthStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location()).Unix()
	var rows []ReferralReward
	if err := tx.Where("inviter_id = ? AND reward_role = ? AND created_at >= ?", inviterID, ReferralRewardRoleInviter, monthStart).
		Where("status IN ?", []string{ReferralRewardStatusPending, ReferralRewardStatusSettled, ReferralRewardStatusPartialReversed}).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	total := 0
	for _, row := range rows {
		net := row.RewardQuota - row.ReversedQuota
		if net > 0 {
			total += net
		}
	}
	return total, nil
}

func SettleDueReferralRewards(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	now := common.GetTimestamp()
	var rewards []ReferralReward
	if err := DB.Where("status = ? AND risk_status IN ? AND settle_at <= ?", ReferralRewardStatusPending, []string{ReferralRewardRiskNormal, ReferralRewardRiskApproved}, now).
		Order("settle_at asc, id asc").Limit(limit).Find(&rewards).Error; err != nil {
		return 0, err
	}
	settled := 0
	for _, reward := range rewards {
		if err := settleReferralRewardByID(reward.Id); err != nil {
			common.SysError(fmt.Sprintf("failed to settle referral reward %d: %v", reward.Id, err))
			continue
		}
		settled++
	}
	return settled, nil
}

func settleReferralRewardByID(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var reward ReferralReward
		if err := lockForUpdate(tx).First(&reward, "id = ?", id).Error; err != nil {
			return err
		}
		if reward.Status != ReferralRewardStatusPending {
			return nil
		}
		var topUp TopUp
		if err := lockForUpdate(tx).Select("id", "money", "refund_amount", "status").First(&topUp, "id = ?", reward.TopUpId).Error; err != nil {
			return err
		}
		if topUp.Status == common.TopUpStatusRefunded || (topUp.Money > 0 && topUp.RefundAmount >= topUp.Money) {
			now := common.GetTimestamp()
			return tx.Model(&reward).Updates(map[string]interface{}{
				"status":       ReferralRewardStatusCancelled,
				"risk_reason":  "topup_fully_refunded",
				"cancelled_at": now,
				"updated_at":   now,
			}).Error
		}
		remaining := reward.RewardQuota - reward.ReversedQuota
		if remaining <= 0 {
			now := common.GetTimestamp()
			return tx.Model(&reward).Updates(map[string]interface{}{
				"status":       ReferralRewardStatusCancelled,
				"cancelled_at": now,
				"updated_at":   now,
			}).Error
		}
		var user User
		if err := lockForUpdate(tx).First(&user, "id = ?", reward.InviterId).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		if err := tx.Model(&User{}).Where("id = ?", reward.InviterId).Updates(map[string]interface{}{
			"aff_quota":   gorm.Expr("aff_quota + ?", remaining),
			"aff_history": gorm.Expr("aff_history + ?", remaining),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&reward).Updates(map[string]interface{}{
			"status":        ReferralRewardStatusSettled,
			"settled_quota": gorm.Expr("settled_quota + ?", remaining),
			"settled_at":    now,
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
		recordReferralRewardLogTx(tx, reward.InviterId, "referral_first_topup_inviter_reward_settled", remaining, reward.Id, reward.TradeNo)
		return nil
	})
}

func reverseReferralRewardsForTopUpTx(tx *gorm.DB, topUp *TopUp, refundMoney float64) error {
	if topUp == nil || topUp.Id <= 0 || topUp.Money <= 0 || refundMoney <= 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&ReferralReward{}) {
		return nil
	}
	var rewards []ReferralReward
	if err := lockForUpdate(tx).Where("top_up_id = ?", topUp.Id).Find(&rewards).Error; err != nil {
		return err
	}
	if len(rewards) == 0 {
		return nil
	}
	for _, reward := range rewards {
		if err := reverseReferralRewardTx(tx, &reward, refundMoney, topUp.Money); err != nil {
			return err
		}
	}
	return nil
}

func reverseReferralRewardTx(tx *gorm.DB, reward *ReferralReward, refundMoney float64, topUpMoney float64) error {
	if reward == nil || reward.RewardQuota <= 0 || topUpMoney <= 0 || refundMoney <= 0 {
		return nil
	}
	if reward.Status == ReferralRewardStatusCancelled || reward.Status == ReferralRewardStatusReversed {
		return nil
	}
	deltaQuota := int(decimal.NewFromInt(int64(reward.RewardQuota)).Mul(decimal.NewFromFloat(refundMoney)).Div(decimal.NewFromFloat(topUpMoney)).IntPart())
	if deltaQuota <= 0 {
		return nil
	}
	remainingReverse := reward.RewardQuota - reward.ReversedQuota
	if deltaQuota > remainingReverse {
		deltaQuota = remainingReverse
	}
	if deltaQuota <= 0 {
		return nil
	}
	now := common.GetTimestamp()
	newReversed := reward.ReversedQuota + deltaQuota
	newRefundAmount := reward.RefundAmount + refundMoney
	newStatus := reward.Status
	updates := map[string]interface{}{
		"reversed_quota": newReversed,
		"refund_amount":  newRefundAmount,
		"reversed_at":    now,
		"updated_at":     now,
	}
	if reward.Status == ReferralRewardStatusPending {
		if newReversed >= reward.RewardQuota {
			newStatus = ReferralRewardStatusCancelled
			updates["status"] = newStatus
			updates["cancelled_at"] = now
		}
		if err := tx.Model(reward).Updates(updates).Error; err != nil {
			return err
		}
		recordReferralRewardLogTx(tx, referralRewardUserID(reward), "referral_first_topup_reward_reversed", deltaQuota, reward.Id, reward.TradeNo)
		return nil
	}
	if reward.Status == ReferralRewardStatusSettled || reward.Status == ReferralRewardStatusPartialReversed {
		owedQuota, err := deductSettledReferralRewardTx(tx, reward, deltaQuota)
		if err != nil {
			return err
		}
		if owedQuota > 0 {
			updates["owed_quota"] = gorm.Expr("owed_quota + ?", owedQuota)
			updates["risk_status"] = ReferralRewardRiskBlocked
			updates["risk_reason"] = "insufficient_quota_reverse_owed"
			common.SysError(fmt.Sprintf("referral reward %d reverse owed quota: %d", reward.Id, owedQuota))
		}
		if newReversed >= reward.RewardQuota {
			newStatus = ReferralRewardStatusReversed
		} else {
			newStatus = ReferralRewardStatusPartialReversed
		}
		updates["status"] = newStatus
		if err := tx.Model(reward).Updates(updates).Error; err != nil {
			return err
		}
		recordReferralRewardLogTx(tx, referralRewardUserID(reward), "referral_first_topup_reward_reversed", deltaQuota, reward.Id, reward.TradeNo)
		if owedQuota > 0 {
			recordReferralRewardLogTx(tx, referralRewardUserID(reward), "referral_first_topup_reward_reverse_owed", owedQuota, reward.Id, reward.TradeNo)
		}
		return nil
	}
	return nil
}

func referralRewardUserID(reward *ReferralReward) int {
	if reward == nil {
		return 0
	}
	if reward.RewardRole == ReferralRewardRoleInviter {
		return reward.InviterId
	}
	return reward.InviteeId
}

func deductSettledReferralRewardTx(tx *gorm.DB, reward *ReferralReward, quota int) (int, error) {
	userID := reward.InviteeId
	if reward.RewardRole == ReferralRewardRoleInviter {
		userID = reward.InviterId
	}
	var user User
	if err := lockForUpdate(tx).First(&user, "id = ?", userID).Error; err != nil {
		return 0, err
	}
	if reward.RewardRole == ReferralRewardRoleInviter {
		fromAff := quota
		if user.AffQuota < fromAff {
			fromAff = user.AffQuota
		}
		fromQuota := quota - fromAff
		owed := 0
		if user.Quota < fromQuota {
			owed = fromQuota - user.Quota
			fromQuota = user.Quota
		}
		if fromAff > 0 || fromQuota > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
				"aff_quota": gorm.Expr("aff_quota - ?", fromAff),
				"quota":     gorm.Expr("quota - ?", fromQuota),
			}).Error; err != nil {
				return 0, err
			}
		}
		return owed, nil
	}
	owed := 0
	if user.Quota < quota {
		owed = quota - user.Quota
		quota = user.Quota
	}
	if quota <= 0 {
		return owed, nil
	}
	return owed, tx.Model(&User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota - ?", quota)).Error
}

func SearchReferralRewards(query ReferralRewardQuery, offset int, limit int) ([]ReferralReward, int64, error) {
	var rewards []ReferralReward
	db := DB.Model(&ReferralReward{})
	db = applyReferralRewardFilters(db, query)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("id desc").Limit(limit).Offset(offset).Find(&rewards).Error; err != nil {
		return nil, 0, err
	}
	return rewards, total, nil
}

func applyReferralRewardFilters(db *gorm.DB, query ReferralRewardQuery) *gorm.DB {
	if query.Keyword != "" {
		like := "%" + query.Keyword + "%"
		db = db.Where("trade_no LIKE ? OR activity_id LIKE ?", like, like)
	}
	if query.InviterId > 0 {
		db = db.Where("inviter_id = ?", query.InviterId)
	}
	if query.InviteeId > 0 {
		db = db.Where("invitee_id = ?", query.InviteeId)
	}
	db = applyReferralUserKeywordFilter(db, "inviter_id", query.InviterKeyword)
	db = applyReferralUserKeywordFilter(db, "invitee_id", query.InviteeKeyword)
	if query.UserGroup != "" {
		db = applyReferralUserGroupFilter(db, "invitee_id", query.UserGroup)
	}
	if query.RewardRole != "" {
		db = db.Where("reward_role = ?", query.RewardRole)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.RiskStatus != "" {
		if strings.Contains(query.RiskStatus, ",") {
			items := make([]string, 0)
			for _, item := range strings.Split(query.RiskStatus, ",") {
				item = strings.TrimSpace(item)
				if item != "" {
					items = append(items, item)
				}
			}
			if len(items) > 0 {
				db = db.Where("risk_status IN ?", items)
			}
		} else {
			db = db.Where("risk_status = ?", query.RiskStatus)
		}
	}
	if query.PaymentProvider != "" {
		db = db.Where("payment_provider = ?", query.PaymentProvider)
	}
	if query.RefundOnly {
		db = db.Where("refund_amount > 0 OR reversed_quota > 0")
	}
	if query.StartTime > 0 {
		db = db.Where("created_at >= ?", query.StartTime)
	}
	if query.EndTime > 0 {
		db = db.Where("created_at <= ?", query.EndTime)
	}
	return db
}

func referralUserIDsByKeyword(keyword string) []int {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}
	like := "%" + keyword + "%"
	query := DB.Model(&User{}).Select("id").Where("username LIKE ? OR display_name LIKE ?", like, like)
	if parsed, err := strconv.Atoi(keyword); err == nil && parsed > 0 {
		query = query.Or("id = ?", parsed)
	}
	var ids []int
	_ = query.Pluck("id", &ids).Error
	return ids
}

func referralUserIDsByGroup(group string) []int {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil
	}
	var ids []int
	_ = DB.Model(&User{}).Where(commonGroupCol+" = ?", group).Pluck("id", &ids).Error
	return ids
}

func applyReferralUserKeywordFilter(db *gorm.DB, column string, keyword string) *gorm.DB {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return db
	}
	ids := referralUserIDsByKeyword(keyword)
	if len(ids) == 0 {
		return db.Where("1 = 0")
	}
	return db.Where(column+" IN ?", ids)
}

func applyReferralUserGroupFilter(db *gorm.DB, column string, group string) *gorm.DB {
	group = strings.TrimSpace(group)
	if group == "" {
		return db
	}
	ids := referralUserIDsByGroup(group)
	if len(ids) == 0 {
		return db.Where("1 = 0")
	}
	return db.Where(column+" IN ?", ids)
}

func GetReferralSummary(userID int) (map[string]interface{}, error) {
	var user User
	if err := DB.Select("id", "aff_code", "aff_count", "aff_quota", "aff_history", "inviter_id").First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	var inviteCount int64
	if err := DB.Model(&ReferralInvite{}).Where("inviter_id = ?", userID).Count(&inviteCount).Error; err != nil {
		return nil, err
	}
	var qualifiedCount int64
	if err := DB.Model(&ReferralReward{}).Where("inviter_id = ? AND reward_role = ? AND reward_quota > 0", userID, ReferralRewardRoleInviter).Count(&qualifiedCount).Error; err != nil {
		return nil, err
	}
	pending, settled, reversed, err := referralRewardQuotaBuckets("inviter_id = ? AND reward_role = ?", userID, ReferralRewardRoleInviter)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"aff_code":                    user.AffCode,
		"invite_link":                 referralInviteLink(user.AffCode),
		"invite_count":                inviteCount,
		"qualified_first_topup_count": qualifiedCount,
		"pending_reward_quota":        pending,
		"settled_reward_quota":        settled,
		"reversed_reward_quota":       reversed,
		"aff_quota":                   user.AffQuota,
		"aff_history_quota":           user.AffHistoryQuota,
		"inviter_id":                  user.InviterId,
	}, nil
}

func referralInviteLink(affCode string) string {
	affCode = strings.TrimSpace(affCode)
	if affCode == "" {
		return ""
	}
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	if base == "" {
		return "/sign-up?aff=" + affCode
	}
	return base + "/sign-up?aff=" + affCode
}

func GetReferralFirstTopUpState(userID int) ReferralFirstTopUpState {
	state := ReferralFirstTopUpState{}
	if userID <= 0 {
		return state
	}
	var user User
	if err := DB.Select("id", "inviter_id").First(&user, "id = ?", userID).Error; err != nil {
		return state
	}
	state.HasInviter = user.InviterId > 0
	if !state.HasInviter {
		return state
	}
	var count int64
	_ = DB.Model(&TopUp{}).
		Where("user_id = ? AND status = ? AND base_quota > 0", userID, common.TopUpStatusSuccess).
		Count(&count).Error
	state.FirstWalletTopUpDone = count > 0
	var rewardCount int64
	_ = DB.Model(&ReferralReward{}).
		Where("invitee_id = ? AND reward_role = ? AND reward_quota > 0", userID, ReferralRewardRoleInvitee).
		Count(&rewardCount).Error
	state.QualifiedRewardGenerated = rewardCount > 0
	return state
}

func referralRewardQuotaBuckets(where string, args ...interface{}) (int, int, int, error) {
	var rewards []ReferralReward
	if err := DB.Where(where, args...).Find(&rewards).Error; err != nil {
		return 0, 0, 0, err
	}
	pending, settled, reversed := 0, 0, 0
	for _, reward := range rewards {
		reversed += reward.ReversedQuota
		switch reward.Status {
		case ReferralRewardStatusPending:
			if remaining := reward.RewardQuota - reward.ReversedQuota; remaining > 0 {
				pending += remaining
			}
		case ReferralRewardStatusSettled, ReferralRewardStatusPartialReversed:
			if net := reward.SettledQuota - reward.ReversedQuota; net > 0 {
				settled += net
			}
		}
	}
	return pending, settled, reversed, nil
}

func GetReferralStatsSummary(filter ReferralStatsFilter) (ReferralStatsSummary, error) {
	var summary ReferralStatsSummary
	inviteQuery := applyReferralInviteStatsFilter(DB.Model(&ReferralInvite{}), filter)
	if err := inviteQuery.Count(&summary.InviteRegisteredCount).Error; err != nil {
		return summary, err
	}
	var inviteeIDs []int
	if err := applyReferralInviteStatsFilter(DB.Model(&ReferralInvite{}), filter).Pluck("invitee_id", &inviteeIDs).Error; err == nil && len(inviteeIDs) > 0 {
		_ = DB.Model(&TopUp{}).
			Where("user_id IN ? AND status = ? AND base_quota > 0", inviteeIDs, common.TopUpStatusSuccess).
			Distinct("user_id").
			Count(&summary.FirstTopUpCount).Error
	}
	var rewards []ReferralReward
	rewardQuery := applyReferralStatsFilter(DB.Model(&ReferralReward{}), filter)
	if err := rewardQuery.Find(&rewards).Error; err != nil {
		return summary, err
	}
	topupIDs := make(map[int]struct{})
	refundTopupIDs := make(map[int]struct{})
	baseQuota := 0
	rewardCost := 0
	for _, reward := range rewards {
		if reward.RewardQuota > 0 {
			topupIDs[reward.TopUpId] = struct{}{}
		}
		if reward.RefundAmount > 0 {
			refundTopupIDs[reward.TopUpId] = struct{}{}
		}
		if reward.RewardRole == ReferralRewardRoleInvitee {
			summary.QualifiedFirstTopUpNetMoney += reward.PaidMoney - reward.RefundAmount
			baseQuota += reward.BaseQuota
			if reward.Status == ReferralRewardStatusSettled || reward.Status == ReferralRewardStatusPartialReversed || reward.Status == ReferralRewardStatusReversed {
				summary.InviteeSettledRewardQuota += reward.SettledQuota - reward.ReversedQuota
			}
		}
		if reward.RewardRole == ReferralRewardRoleInviter {
			switch reward.Status {
			case ReferralRewardStatusPending:
				summary.PendingRewardQuota += reward.RewardQuota - reward.ReversedQuota
			case ReferralRewardStatusSettled, ReferralRewardStatusPartialReversed, ReferralRewardStatusReversed:
				summary.InviterSettledRewardQuota += reward.SettledQuota - reward.ReversedQuota
			}
		}
		summary.ReversedRewardQuota += reward.ReversedQuota
		if reward.RewardRole == ReferralRewardRoleInvitee {
			summary.RefundMoney += reward.RefundAmount
		}
		if reward.RiskStatus == ReferralRewardRiskBlocked || reward.RiskStatus == ReferralRewardRiskReview {
			summary.BlockedRewardCount++
		}
		if net := reward.RewardQuota - reward.ReversedQuota; net > 0 {
			rewardCost += net
		}
	}
	summary.QualifiedFirstTopUpCount = int64(len(topupIDs))
	if summary.InviteRegisteredCount > 0 {
		summary.ConversionRate = float64(summary.QualifiedFirstTopUpCount) / float64(summary.InviteRegisteredCount)
	}
	if summary.QualifiedFirstTopUpCount > 0 {
		summary.RefundRate = float64(len(refundTopupIDs)) / float64(summary.QualifiedFirstTopUpCount)
	}
	if baseQuota > 0 {
		summary.RewardCostRate = float64(rewardCost) / float64(baseQuota)
	}
	if rewardCost > 0 {
		summary.ROI = summary.QualifiedFirstTopUpNetMoney / float64(rewardCost)
	}
	return summary, nil
}

func applyReferralInviteStatsFilter(db *gorm.DB, filter ReferralStatsFilter) *gorm.DB {
	if filter.StartTime > 0 {
		db = db.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		db = db.Where("created_at <= ?", filter.EndTime)
	}
	if filter.InviterId > 0 {
		db = db.Where("inviter_id = ?", filter.InviterId)
	}
	if filter.InviteeId > 0 {
		db = db.Where("invitee_id = ?", filter.InviteeId)
	}
	db = applyReferralUserKeywordFilter(db, "inviter_id", filter.InviterKeyword)
	db = applyReferralUserKeywordFilter(db, "invitee_id", filter.InviteeKeyword)
	if filter.UserGroup != "" {
		db = applyReferralUserGroupFilter(db, "invitee_id", filter.UserGroup)
	}
	return db
}

func applyReferralStatsFilter(db *gorm.DB, filter ReferralStatsFilter) *gorm.DB {
	if filter.StartTime > 0 {
		db = db.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		db = db.Where("created_at <= ?", filter.EndTime)
	}
	if filter.ActivityID != "" {
		db = db.Where("activity_id = ?", filter.ActivityID)
	}
	if filter.InviterId > 0 {
		db = db.Where("inviter_id = ?", filter.InviterId)
	}
	if filter.InviteeId > 0 {
		db = db.Where("invitee_id = ?", filter.InviteeId)
	}
	db = applyReferralUserKeywordFilter(db, "inviter_id", filter.InviterKeyword)
	db = applyReferralUserKeywordFilter(db, "invitee_id", filter.InviteeKeyword)
	if filter.UserGroup != "" {
		db = applyReferralUserGroupFilter(db, "invitee_id", filter.UserGroup)
	}
	if filter.PaymentProvider != "" {
		db = db.Where("payment_provider = ?", filter.PaymentProvider)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	if filter.RiskStatus != "" {
		db = db.Where("risk_status = ?", filter.RiskStatus)
	}
	if filter.RefundOnly {
		db = db.Where("refund_amount > 0 OR reversed_quota > 0")
	}
	return db
}

func GetReferralTopInviters(filter ReferralStatsFilter, limit int) ([]ReferralTopInviterStat, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rewards []ReferralReward
	if err := applyReferralStatsFilter(DB.Model(&ReferralReward{}), filter).Find(&rewards).Error; err != nil {
		return nil, err
	}
	stats := make(map[int]*ReferralTopInviterStat)
	orderIDs := make(map[int]map[int]struct{})
	refundOrderIDs := make(map[int]map[int]struct{})
	for _, reward := range rewards {
		stat := stats[reward.InviterId]
		if stat == nil {
			stat = &ReferralTopInviterStat{InviterId: reward.InviterId, RiskStatus: ReferralRewardRiskNormal}
			stats[reward.InviterId] = stat
			orderIDs[reward.InviterId] = map[int]struct{}{}
			refundOrderIDs[reward.InviterId] = map[int]struct{}{}
		}
		if reward.RewardQuota > 0 {
			orderIDs[reward.InviterId][reward.TopUpId] = struct{}{}
		}
		if reward.RewardRole == ReferralRewardRoleInvitee {
			stat.QualifiedFirstTopUpCount++
			stat.FirstTopUpNetMoney += reward.PaidMoney - reward.RefundAmount
			stat.InviteeRewardQuota += reward.RewardQuota - reward.ReversedQuota
			stat.RefundMoney += reward.RefundAmount
		}
		if reward.RewardRole == ReferralRewardRoleInviter {
			if reward.Status == ReferralRewardStatusPending {
				stat.PendingRewardQuota += reward.RewardQuota - reward.ReversedQuota
			} else {
				stat.InviterSettledRewardQuota += reward.SettledQuota - reward.ReversedQuota
			}
		}
		if reward.RefundAmount > 0 || reward.ReversedQuota > 0 || reward.Status == ReferralRewardStatusPartialReversed || reward.Status == ReferralRewardStatusReversed {
			refundOrderIDs[reward.InviterId][reward.TopUpId] = struct{}{}
		}
		if reward.RiskStatus == ReferralRewardRiskBlocked || reward.RiskStatus == ReferralRewardRiskReview {
			stat.RiskStatus = reward.RiskStatus
		}
	}
	items := make([]ReferralTopInviterStat, 0, len(stats))
	for inviterID, stat := range stats {
		var inviteCount int64
		_ = DB.Model(&ReferralInvite{}).Where("inviter_id = ?", inviterID).Count(&inviteCount).Error
		stat.InviteRegisteredCount = inviteCount
		cost := stat.InviteeRewardQuota + stat.InviterSettledRewardQuota + stat.PendingRewardQuota
		if cost > 0 {
			stat.ROI = stat.FirstTopUpNetMoney / float64(cost)
		}
		if len(orderIDs[inviterID]) > 0 {
			stat.RefundRate = float64(len(refundOrderIDs[inviterID])) / float64(len(orderIDs[inviterID]))
		}
		stat.InviterUsername, _ = GetUsernameById(inviterID, false)
		items = append(items, *stat)
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if referralTopInviterLess(filter.Sort, items[i], items[j]) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func referralTopInviterLess(sort string, current ReferralTopInviterStat, candidate ReferralTopInviterStat) bool {
	switch sort {
	case "settled_reward_desc":
		return candidate.InviterSettledRewardQuota > current.InviterSettledRewardQuota
	case "roi_desc":
		return candidate.ROI > current.ROI
	case "refund_rate_desc":
		return candidate.RefundRate > current.RefundRate
	default:
		return candidate.FirstTopUpNetMoney > current.FirstTopUpNetMoney
	}
}

func GetReferralFunnel(filter ReferralStatsFilter) ([]ReferralFunnelItem, error) {
	summary, err := GetReferralStatsSummary(filter)
	if err != nil {
		return nil, err
	}
	generated := summary.QualifiedFirstTopUpCount
	settled := int64(0)
	if summary.InviterSettledRewardQuota > 0 || summary.InviteeSettledRewardQuota > 0 {
		settled = generated
	}
	items := []ReferralFunnelItem{
		{Stage: "registered", Count: summary.InviteRegisteredCount},
		{Stage: "first_topup", Count: summary.FirstTopUpCount},
		{Stage: "qualified_first_topup", Count: summary.QualifiedFirstTopUpCount},
		{Stage: "reward_generated", Count: generated},
		{Stage: "inviter_settled", Count: settled},
	}
	base := float64(summary.InviteRegisteredCount)
	prev := int64(0)
	for i := range items {
		if base > 0 {
			items[i].Rate = float64(items[i].Count) / base
		}
		if prev > 0 {
			items[i].PriorRate = float64(items[i].Count) / float64(prev)
		}
		prev = items[i].Count
	}
	return items, nil
}

func GetReferralTrend(filter ReferralStatsFilter) ([]ReferralTrendItem, error) {
	var rewards []ReferralReward
	if err := applyReferralStatsFilter(DB.Model(&ReferralReward{}), filter).Find(&rewards).Error; err != nil {
		return nil, err
	}
	buckets := map[string]*ReferralTrendItem{}
	for _, reward := range rewards {
		bucket := referralTrendBucket(reward.CreatedAt, filter.Bucket)
		item := buckets[bucket]
		if item == nil {
			item = &ReferralTrendItem{Bucket: bucket}
			buckets[bucket] = item
		}
		if reward.RewardRole == ReferralRewardRoleInvitee {
			item.NetMoney += reward.PaidMoney - reward.RefundAmount
			item.QualifiedFirstTopUpCount++
		}
		if net := reward.RewardQuota - reward.ReversedQuota; net > 0 {
			item.RewardCostQuota += net
		}
		item.RefundMoney += reward.RefundAmount
	}
	items := make([]ReferralTrendItem, 0, len(buckets))
	for _, item := range buckets {
		items = append(items, *item)
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].Bucket < items[i].Bucket {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items, nil
}

func referralTrendBucket(timestamp int64, bucket string) string {
	t := time.Unix(timestamp, 0).Local()
	switch bucket {
	case "week":
		year, week := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case "month":
		return t.Format("2006-01")
	default:
		return t.Format("2006-01-02")
	}
}

func UpdateReferralRewardRiskStatus(id int, status string, reason string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return ErrReferralRewardStatusInvalid
	}
	now := common.GetTimestamp()
	return DB.Model(&ReferralReward{}).Where("id = ?", id).Updates(map[string]interface{}{
		"risk_status": status,
		"risk_reason": strings.TrimSpace(reason),
		"updated_at":  now,
	}).Error
}

func CancelReferralReward(id int, reason string) error {
	now := common.GetTimestamp()
	return DB.Model(&ReferralReward{}).Where("id = ? AND status = ?", id, ReferralRewardStatusPending).Updates(map[string]interface{}{
		"status":       ReferralRewardStatusCancelled,
		"risk_status":  ReferralRewardRiskRejected,
		"risk_reason":  strings.TrimSpace(reason),
		"cancelled_at": now,
		"updated_at":   now,
	}).Error
}

func BlockInviterPendingReferralRewards(inviterID int, reason string) (int64, error) {
	if inviterID <= 0 {
		return 0, ErrReferralRewardNotFound
	}
	now := common.GetTimestamp()
	result := DB.Model(&ReferralReward{}).
		Where("inviter_id = ? AND reward_role = ? AND status = ?", inviterID, ReferralRewardRoleInviter, ReferralRewardStatusPending).
		Updates(map[string]interface{}{
			"risk_status": ReferralRewardRiskBlocked,
			"risk_reason": strings.TrimSpace(reason),
			"updated_at":  now,
		})
	return result.RowsAffected, result.Error
}

func ReverseReferralReward(id int, reason string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var reward ReferralReward
		if err := lockForUpdate(tx).First(&reward, "id = ?", id).Error; err != nil {
			return ErrReferralRewardNotFound
		}
		remaining := reward.RewardQuota - reward.ReversedQuota
		if remaining <= 0 {
			return nil
		}
		if reward.Status != ReferralRewardStatusSettled && reward.Status != ReferralRewardStatusPartialReversed {
			return ErrReferralRewardStatusInvalid
		}
		owedQuota, err := deductSettledReferralRewardTx(tx, &reward, remaining)
		if err != nil {
			return err
		}
		now := common.GetTimestamp()
		updates := map[string]interface{}{
			"status":         ReferralRewardStatusReversed,
			"reversed_quota": reward.ReversedQuota + remaining,
			"reversed_at":    now,
			"risk_reason":    strings.TrimSpace(reason),
			"updated_at":     now,
		}
		if owedQuota > 0 {
			updates["owed_quota"] = gorm.Expr("owed_quota + ?", owedQuota)
			updates["risk_status"] = ReferralRewardRiskBlocked
			updates["risk_reason"] = "manual_reverse_owed:" + strings.TrimSpace(reason)
		}
		if err := tx.Model(&reward).Updates(updates).Error; err != nil {
			return err
		}
		recordReferralRewardLogTx(tx, referralRewardUserID(&reward), "referral_first_topup_reward_reversed", remaining, reward.Id, reward.TradeNo)
		return nil
	})
}

func LogReferralRewardSettlement(userID int, quota int) {
	if quota <= 0 {
		return
	}
	RecordLog(userID, LogTypeSystem, fmt.Sprintf("邀请首充奖励结算 %s", logger.LogQuota(quota)))
}
