package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type PaymentSetting struct {
	AmountOptions  []int             `json:"amount_options"`
	AmountDiscount map[int]float64   `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	TopUpBonus     TopUpBonusSetting `json:"topup_bonus"`

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

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	TopUpBonus: TopUpBonusSetting{
		Visible: true,
	},
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
