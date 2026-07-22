package operation_setting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type PaymentSetting struct {
	AmountOptions            []int                           `json:"amount_options"`
	AmountDiscount           map[int]float64                 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	TopUpBonus               TopUpBonusSetting               `json:"topup_bonus"`
	ReferralFirstTopUpReward ReferralFirstTopUpRewardSetting `json:"referral_first_topup_reward"`

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

type TopUpBonusSetting struct {
	Enabled                bool    `json:"enabled"`
	ActivityID             string  `json:"activity_id"`
	ActivityName           string  `json:"activity_name"`
	StartTime              int64   `json:"start_time"`
	EndTime                int64   `json:"end_time"`
	MinAmount              int64   `json:"min_amount"`
	BonusPercent           float64 `json:"bonus_percent"`
	SingleBonusMaxAmount   int64   `json:"single_bonus_max_amount"`
	UserBonusMaxAmount     int64   `json:"user_bonus_max_amount"`
	TotalBonusBudgetAmount int64   `json:"total_bonus_budget_amount"`
	FirstTopUpOnly         bool    `json:"first_topup_only"`
	Visible                bool    `json:"visible"`
}

type ReferralFirstTopUpRewardSetting struct {
	Enabled                     bool     `json:"enabled"`
	ActivityID                  string   `json:"activity_id"`
	ActivityName                string   `json:"activity_name"`
	StartTime                   int64    `json:"start_time"`
	EndTime                     int64    `json:"end_time"`
	MinPaidMoney                float64  `json:"min_paid_money"`
	ThresholdOperator           string   `json:"threshold_operator"`
	FirstTopUpMode              string   `json:"first_topup_mode"`
	InviteeRewardPercent        float64  `json:"invitee_reward_percent"`
	InviterRewardPercent        float64  `json:"inviter_reward_percent"`
	InviterSettleDelayDays      int      `json:"inviter_settle_delay_days"`
	SingleInviteeRewardMaxQuota int      `json:"single_invitee_reward_max_quota"`
	SingleInviterRewardMaxQuota int      `json:"single_inviter_reward_max_quota"`
	InviterMonthlyMaxQuota      int      `json:"inviter_monthly_max_quota"`
	TotalBudgetQuota            int      `json:"total_budget_quota"`
	StackWithTopUpBonus         bool     `json:"stack_with_topup_bonus"`
	ExcludedPaymentProviders    []string `json:"excluded_payment_providers"`
	ExcludedUserGroups          []string `json:"excluded_user_groups"`
	AutoBlockRiskyRewards       bool     `json:"auto_block_risky_rewards"`
	RiskSameIP24hRegisterLimit  int      `json:"risk_same_ip_24h_register_limit"`
	RiskSameDevice7dBindLimit   int      `json:"risk_same_device_7d_bind_limit"`
	RiskSamePaymentAccountLimit int      `json:"risk_same_payment_account_limit"`
	RiskInviter24hRewardQuota   int      `json:"risk_inviter_24h_reward_quota"`
	RiskInviter30dRefundRate    float64  `json:"risk_inviter_30d_refund_rate"`
	RiskRegisterToTopUpMinSec   int      `json:"risk_register_to_topup_min_sec"`
	RiskRefundWindowSec         int      `json:"risk_refund_window_sec"`
	Visible                     bool     `json:"visible"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	TopUpBonus: TopUpBonusSetting{
		Visible: true,
	},
	ReferralFirstTopUpReward: DefaultReferralFirstTopUpRewardSetting(),
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}

func DefaultReferralFirstTopUpRewardSetting() ReferralFirstTopUpRewardSetting {
	return ReferralFirstTopUpRewardSetting{
		Enabled:                     false,
		ActivityID:                  "referral_first_topup_v1",
		ActivityName:                "邀请首充双向奖励",
		MinPaidMoney:                30,
		ThresholdOperator:           "gte",
		FirstTopUpMode:              "strict_first",
		InviteeRewardPercent:        10,
		InviterRewardPercent:        10,
		InviterSettleDelayDays:      7,
		StackWithTopUpBonus:         true,
		AutoBlockRiskyRewards:       true,
		RiskSameIP24hRegisterLimit:  3,
		RiskSameDevice7dBindLimit:   3,
		RiskSamePaymentAccountLimit: 2,
		RiskInviter24hRewardQuota:   0,
		RiskInviter30dRefundRate:    0.5,
		RiskRegisterToTopUpMinSec:   60,
		RiskRefundWindowSec:         24 * 60 * 60,
		Visible:                     true,
	}
}

func (s ReferralFirstTopUpRewardSetting) Normalized() ReferralFirstTopUpRewardSetting {
	defaults := DefaultReferralFirstTopUpRewardSetting()
	if strings.TrimSpace(s.ActivityID) == "" {
		s.ActivityID = defaults.ActivityID
	}
	if strings.TrimSpace(s.ActivityName) == "" {
		s.ActivityName = defaults.ActivityName
	}
	if s.MinPaidMoney <= 0 {
		s.MinPaidMoney = defaults.MinPaidMoney
	}
	if s.ThresholdOperator != "gt" && s.ThresholdOperator != "gte" {
		s.ThresholdOperator = defaults.ThresholdOperator
	}
	if s.FirstTopUpMode != "first_qualified" && s.FirstTopUpMode != "strict_first" {
		s.FirstTopUpMode = defaults.FirstTopUpMode
	}
	if s.InviteeRewardPercent < 0 {
		s.InviteeRewardPercent = 0
	}
	if s.InviterRewardPercent < 0 {
		s.InviterRewardPercent = 0
	}
	if s.InviterSettleDelayDays < 0 {
		s.InviterSettleDelayDays = 0
	}
	if s.RiskSameIP24hRegisterLimit < 0 {
		s.RiskSameIP24hRegisterLimit = 0
	}
	if s.RiskSameDevice7dBindLimit < 0 {
		s.RiskSameDevice7dBindLimit = 0
	}
	if s.RiskSamePaymentAccountLimit < 0 {
		s.RiskSamePaymentAccountLimit = 0
	}
	if s.RiskInviter24hRewardQuota < 0 {
		s.RiskInviter24hRewardQuota = 0
	}
	if s.RiskInviter30dRefundRate < 0 {
		s.RiskInviter30dRefundRate = 0
	}
	if s.RiskRegisterToTopUpMinSec < 0 {
		s.RiskRegisterToTopUpMinSec = 0
	}
	if s.RiskRefundWindowSec < 0 {
		s.RiskRefundWindowSec = 0
	}
	return s
}

func (s ReferralFirstTopUpRewardSetting) Validate() error {
	if s.MinPaidMoney < 0 {
		return errors.New("min_paid_money must be >= 0")
	}
	if s.ThresholdOperator != "" && s.ThresholdOperator != "gt" && s.ThresholdOperator != "gte" {
		return errors.New("threshold_operator must be gte or gt")
	}
	if s.FirstTopUpMode != "" && s.FirstTopUpMode != "first_qualified" && s.FirstTopUpMode != "strict_first" {
		return errors.New("first_topup_mode must be strict_first or first_qualified")
	}
	if s.InviteeRewardPercent < 0 || s.InviterRewardPercent < 0 {
		return errors.New("reward percent must be >= 0")
	}
	if s.InviterSettleDelayDays < 0 {
		return errors.New("inviter_settle_delay_days must be >= 0")
	}
	if s.SingleInviteeRewardMaxQuota < 0 ||
		s.SingleInviterRewardMaxQuota < 0 ||
		s.InviterMonthlyMaxQuota < 0 ||
		s.TotalBudgetQuota < 0 ||
		s.RiskSameIP24hRegisterLimit < 0 ||
		s.RiskSameDevice7dBindLimit < 0 ||
		s.RiskSamePaymentAccountLimit < 0 ||
		s.RiskInviter24hRewardQuota < 0 ||
		s.RiskInviter30dRefundRate < 0 ||
		s.RiskRegisterToTopUpMinSec < 0 ||
		s.RiskRefundWindowSec < 0 {
		return errors.New("quota, threshold and risk fields must be >= 0")
	}
	if s.Enabled && (s.InviteeRewardPercent > 0 || s.InviterRewardPercent > 0) && !IsPaymentComplianceConfirmed() {
		return fmt.Errorf("payment compliance must be confirmed before enabling referral first top-up rewards")
	}
	return nil
}
