package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id" gorm:"index"`
	Amount             int64   `json:"amount"`
	Money              float64 `json:"money"`
	TradeNo            string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod      string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider    string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	PaymentIntentID    string  `json:"payment_intent_id" gorm:"type:varchar(128);default:'';index"`
	BaseQuota          int     `json:"base_quota" gorm:"default:0"`
	BonusAmount        int64   `json:"bonus_amount" gorm:"default:0"`
	BonusQuota         int     `json:"bonus_quota" gorm:"default:0"`
	BonusActivityID    string  `json:"bonus_activity_id" gorm:"type:varchar(128);default:''"`
	BonusActivityName  string  `json:"bonus_activity_name" gorm:"type:varchar(255);default:''"`
	RefundAmount       float64 `json:"refund_amount" gorm:"default:0"`
	RefundQuota        int     `json:"refund_quota" gorm:"default:0"`
	RefundReferenceIDs string  `json:"refund_reference_ids" gorm:"type:text"`
	CreateTime         int64   `json:"create_time"`
	CompleteTime       int64   `json:"complete_time"`
	Status             string  `json:"status"`
}

const (
	PaymentMethodAlipayF2F    = "alipay_f2f"
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderAlipayF2F    = "alipay_f2f"
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrTopUpRefundQuotaShort = errors.New("topup refund quota insufficient")
)

type TopUpSettlement struct {
	BaseAmount    int64
	BonusAmount   int64
	BaseQuota     int
	BonusQuota    int
	TotalQuota    int
	ActivityID    string
	ActivityName  string
	BonusEligible bool
}

func paidMoneyToBonusAmount(money float64) int64 {
	return decimal.NewFromFloat(money).IntPart()
}

func quotaFromTopUpAmount(amount int64) int {
	return int(decimal.NewFromInt(amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
}

func CalculateTopUpSettlement(amount int64, userHistoricalBonusAmount int64, totalHistoricalBonusAmount int64) TopUpSettlement {
	return CalculateTopUpSettlementWithBaseQuota(amount, quotaFromTopUpAmount(amount), userHistoricalBonusAmount, totalHistoricalBonusAmount)
}

func CalculateTopUpSettlementWithBaseQuota(amount int64, baseQuota int, userHistoricalBonusAmount int64, totalHistoricalBonusAmount int64) TopUpSettlement {
	return CalculateTopUpSettlementWithPaidAmount(amount, amount, baseQuota, userHistoricalBonusAmount, totalHistoricalBonusAmount)
}

func CalculateTopUpSettlementWithPaidAmount(amount int64, paidAmount int64, baseQuota int, userHistoricalBonusAmount int64, totalHistoricalBonusAmount int64) TopUpSettlement {
	result := TopUpSettlement{
		BaseAmount: amount,
		BaseQuota:  baseQuota,
		TotalQuota: baseQuota,
	}
	setting := operation_setting.GetPaymentSetting().TopUpBonus
	now := common.GetTimestamp()
	if !setting.Enabled ||
		paidAmount <= 0 ||
		setting.MinAmount <= 0 ||
		paidAmount < setting.MinAmount ||
		setting.BonusPercent <= 0 ||
		(setting.StartTime > 0 && now < setting.StartTime) ||
		(setting.EndTime > 0 && now > setting.EndTime) {
		return result
	}

	bonusAmount := decimal.NewFromInt(paidAmount).
		Mul(decimal.NewFromFloat(setting.BonusPercent)).
		Div(decimal.NewFromInt(100)).
		IntPart()
	if setting.SingleBonusMaxAmount > 0 && bonusAmount > setting.SingleBonusMaxAmount {
		bonusAmount = setting.SingleBonusMaxAmount
	}
	if setting.UserBonusMaxAmount > 0 {
		remaining := setting.UserBonusMaxAmount - userHistoricalBonusAmount
		if remaining <= 0 {
			bonusAmount = 0
		} else if bonusAmount > remaining {
			bonusAmount = remaining
		}
	}
	if setting.TotalBonusBudgetAmount > 0 {
		remaining := setting.TotalBonusBudgetAmount - totalHistoricalBonusAmount
		if remaining <= 0 {
			bonusAmount = 0
		} else if bonusAmount > remaining {
			bonusAmount = remaining
		}
	}
	if bonusAmount <= 0 {
		return result
	}

	bonusQuota := int(decimal.NewFromInt(bonusAmount).
		Mul(decimal.NewFromInt(int64(baseQuota))).
		Div(decimal.NewFromInt(paidAmount)).
		IntPart())
	if bonusQuota <= 0 {
		return result
	}
	result.BonusAmount = bonusAmount
	result.BonusQuota = bonusQuota
	result.TotalQuota = result.BaseQuota + result.BonusQuota
	result.ActivityID = strings.TrimSpace(setting.ActivityID)
	result.ActivityName = strings.TrimSpace(setting.ActivityName)
	result.BonusEligible = true
	return result
}

func calculateTopUpSettlementTx(tx *gorm.DB, topUp *TopUp) (TopUpSettlement, error) {
	return calculateTopUpSettlementWithBaseQuotaTx(tx, topUp, topUp.Amount, quotaFromTopUpAmount(topUp.Amount))
}

func calculateTopUpSettlementWithBaseQuotaTx(tx *gorm.DB, topUp *TopUp, amount int64, baseQuota int) (TopUpSettlement, error) {
	return calculateTopUpSettlementWithPaidAmountTx(tx, topUp, amount, paidMoneyToBonusAmount(topUp.Money), baseQuota)
}

func calculateTopUpSettlementWithPaidAmountTx(tx *gorm.DB, topUp *TopUp, amount int64, paidAmount int64, baseQuota int) (TopUpSettlement, error) {
	if operation_setting.GetPaymentSetting().TopUpBonus.FirstTopUpOnly {
		var count int64
		if err := tx.Model(&TopUp{}).
			Where("user_id = ? AND status = ?", topUp.UserId, common.TopUpStatusSuccess).
			Count(&count).Error; err != nil {
			return TopUpSettlement{}, err
		}
		if count > 0 {
			return TopUpSettlement{
				BaseAmount: amount,
				BaseQuota:  baseQuota,
				TotalQuota: baseQuota,
			}, nil
		}
	}

	userBonusAmount := int64(0)
	if operation_setting.GetPaymentSetting().TopUpBonus.UserBonusMaxAmount > 0 {
		total, err := sumNetTopUpBonusAmountTx(tx, "user_id = ? AND status = ?", topUp.UserId, common.TopUpStatusSuccess)
		if err != nil {
			return TopUpSettlement{}, err
		}
		userBonusAmount = total
	}

	totalBonusAmount := int64(0)
	if operation_setting.GetPaymentSetting().TopUpBonus.TotalBonusBudgetAmount > 0 {
		total, err := sumNetTopUpBonusAmountTx(tx, "status = ?", common.TopUpStatusSuccess)
		if err != nil {
			return TopUpSettlement{}, err
		}
		totalBonusAmount = total
	}

	return CalculateTopUpSettlementWithPaidAmount(amount, paidAmount, baseQuota, userBonusAmount, totalBonusAmount), nil
}

func sumNetTopUpBonusAmountTx(tx *gorm.DB, where string, args ...interface{}) (int64, error) {
	var topUps []TopUp
	if err := tx.Select("bonus_amount", "money", "refund_amount").
		Where(where, args...).
		Where("bonus_amount > 0").
		Find(&topUps).Error; err != nil {
		return 0, err
	}

	total := int64(0)
	for _, item := range topUps {
		netBonus := item.BonusAmount
		if item.Money > 0 && item.RefundAmount > 0 {
			refundRatio := decimal.NewFromFloat(item.RefundAmount).Div(decimal.NewFromFloat(item.Money))
			refundedBonus := decimal.NewFromInt(item.BonusAmount).Mul(refundRatio).IntPart()
			netBonus -= refundedBonus
		}
		if netBonus > 0 {
			total += netBonus
		}
	}
	return total, nil
}

func applyTopUpSettlementTx(tx *gorm.DB, topUp *TopUp, settlement TopUpSettlement, extraUserUpdates map[string]interface{}) error {
	if settlement.TotalQuota <= 0 {
		return errors.New("无效的充值额度")
	}

	topUp.CompleteTime = common.GetTimestamp()
	topUp.Status = common.TopUpStatusSuccess
	topUp.BaseQuota = settlement.BaseQuota
	topUp.BonusAmount = settlement.BonusAmount
	topUp.BonusQuota = settlement.BonusQuota
	topUp.BonusActivityID = settlement.ActivityID
	topUp.BonusActivityName = settlement.ActivityName
	if err := tx.Save(topUp).Error; err != nil {
		return err
	}

	updateFields := map[string]interface{}{
		"quota": gorm.Expr("quota + ?", settlement.TotalQuota),
	}
	for key, value := range extraUserUpdates {
		updateFields[key] = value
	}
	return tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
}

func topUpLogContent(prefix string, settlement TopUpSettlement, payMoney float64) string {
	if settlement.BonusQuota > 0 {
		activityParts := make([]string, 0, 2)
		if settlement.ActivityID != "" {
			activityParts = append(activityParts, "活动ID: "+settlement.ActivityID)
		}
		if settlement.ActivityName != "" {
			activityParts = append(activityParts, "活动名称: "+settlement.ActivityName)
		}
		activityInfo := ""
		if len(activityParts) > 0 {
			activityInfo = "，" + strings.Join(activityParts, "，")
		}
		return fmt.Sprintf("%s，充值额度: %v，活动赠送: %v%s，支付金额: %.2f", prefix, logger.FormatQuota(settlement.BaseQuota), logger.FormatQuota(settlement.BonusQuota), activityInfo, payMoney)
	}
	return fmt.Sprintf("%s，充值额度: %v，支付金额: %.2f", prefix, logger.FormatQuota(settlement.BaseQuota), payMoney)
}

func topUpRefundQuota(topUp *TopUp, refundMoney float64) (baseQuota int, bonusQuota int, totalQuota int) {
	if topUp == nil || topUp.Money <= 0 || refundMoney <= 0 {
		return 0, 0, 0
	}
	if refundMoney > topUp.Money {
		refundMoney = topUp.Money
	}
	ratio := decimal.NewFromFloat(refundMoney).Div(decimal.NewFromFloat(topUp.Money))
	if topUp.BaseQuota > 0 {
		baseQuota = int(decimal.NewFromInt(int64(topUp.BaseQuota)).Mul(ratio).IntPart())
	} else {
		baseQuota = int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(ratio).IntPart())
		if topUp.PaymentProvider == PaymentProviderStripe {
			baseQuota = int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(ratio).IntPart())
		}
		if topUp.PaymentProvider == PaymentProviderCreem {
			baseQuota = int(decimal.NewFromInt(topUp.Amount).Mul(ratio).IntPart())
		}
	}
	bonusQuota = int(decimal.NewFromInt(int64(topUp.BonusQuota)).Mul(ratio).IntPart())
	totalQuota = baseQuota + bonusQuota
	return baseQuota, bonusQuota, totalQuota
}

func RefundTopUp(tradeNo string, expectedPaymentProvider string, refundMoney float64, callerIp string) error {
	return RefundTopUpWithReference(tradeNo, expectedPaymentProvider, refundMoney, "", callerIp)
}

func parseTopUpRefundReferenceSet(raw string) map[string]struct{} {
	refs := make(map[string]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return refs
	}
	var values []string
	if err := common.UnmarshalJsonStr(raw, &values); err != nil {
		return refs
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			refs[value] = struct{}{}
		}
	}
	return refs
}

func encodeTopUpRefundReferences(refs map[string]struct{}) string {
	if len(refs) == 0 {
		return ""
	}
	values := make([]string, 0, len(refs))
	for ref := range refs {
		values = append(values, ref)
	}
	bytes, err := common.Marshal(values)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func RefundTopUpWithReference(tradeNo string, expectedPaymentProvider string, refundMoney float64, refundReferenceID string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}
	if refundMoney <= 0 {
		return errors.New("退款金额必须大于 0")
	}
	refundReferenceID = strings.TrimSpace(refundReferenceID)

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var paymentMethod string
	var baseQuota int
	var bonusQuota int
	var totalQuota int
	var actualRefundMoney float64
	refundApplied := false

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if refundReferenceID != "" {
			refs := parseTopUpRefundReferenceSet(topUp.RefundReferenceIDs)
			if _, ok := refs[refundReferenceID]; ok {
				return nil
			}
		}
		if topUp.Status != common.TopUpStatusSuccess {
			return ErrTopUpStatusInvalid
		}

		refundableMoney := topUp.Money - topUp.RefundAmount
		if refundableMoney <= 0 {
			return ErrTopUpStatusInvalid
		}
		actualRefundMoney = refundMoney
		if actualRefundMoney > refundableMoney {
			actualRefundMoney = refundableMoney
		}
		baseQuota, bonusQuota, totalQuota = topUpRefundQuota(topUp, actualRefundMoney)
		if totalQuota <= 0 {
			return errors.New("无效的退款额度")
		}

		var user User
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
			return err
		}
		if user.Quota < totalQuota {
			return ErrTopUpRefundQuotaShort
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota - ?", totalQuota)).Error; err != nil {
			return err
		}
		topUp.RefundAmount += actualRefundMoney
		topUp.RefundQuota += totalQuota
		if refundReferenceID != "" {
			refs := parseTopUpRefundReferenceSet(topUp.RefundReferenceIDs)
			refs[refundReferenceID] = struct{}{}
			topUp.RefundReferenceIDs = encodeTopUpRefundReferences(refs)
		}
		if topUp.RefundAmount >= topUp.Money {
			topUp.Status = common.TopUpStatusRefunded
		}
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		userId = topUp.UserId
		paymentMethod = topUp.PaymentMethod
		refundApplied = true
		return nil
	})
	if err != nil {
		return err
	}
	if !refundApplied {
		return nil
	}

	content := fmt.Sprintf("充值退款，扣回充值额度: %v，扣回活动赠送: %v，退款金额: %.2f", logger.FormatQuota(baseQuota), logger.FormatQuota(bonusQuota), actualRefundMoney)
	RecordTopupLog(userId, content, callerIp, paymentMethod, "refund")
	return nil
}

func RefundTopUpCumulative(tradeNo string, expectedPaymentProvider string, cumulativeRefundMoney float64, callerIp string) error {
	return RefundTopUpCumulativeWithReference(tradeNo, expectedPaymentProvider, cumulativeRefundMoney, "", callerIp)
}

func RefundTopUpCumulativeWithReference(tradeNo string, expectedPaymentProvider string, cumulativeRefundMoney float64, refundReferenceID string, callerIp string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}
	if cumulativeRefundMoney <= 0 {
		return errors.New("退款金额必须大于 0")
	}

	var topUp TopUp
	if err := DB.Select("trade_no", "payment_provider", "money", "refund_amount", "refund_reference_ids", "status").
		Where("trade_no = ?", tradeNo).
		First(&topUp).Error; err != nil {
		return ErrTopUpNotFound
	}
	if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
		return ErrPaymentMethodMismatch
	}
	refundReferenceID = strings.TrimSpace(refundReferenceID)
	if refundReferenceID != "" {
		refs := parseTopUpRefundReferenceSet(topUp.RefundReferenceIDs)
		if _, ok := refs[refundReferenceID]; ok {
			return nil
		}
	}
	if cumulativeRefundMoney > topUp.Money {
		cumulativeRefundMoney = topUp.Money
	}
	refundDelta := cumulativeRefundMoney - topUp.RefundAmount
	if refundDelta <= 0 {
		return nil
	}
	if topUp.Status != common.TopUpStatusSuccess {
		return ErrTopUpStatusInvalid
	}
	return RefundTopUpWithReference(tradeNo, expectedPaymentProvider, refundDelta, refundReferenceID, callerIp)
}

func RefundTopUpByRemainingRefundAmount(tradeNo string, expectedPaymentProvider string, remainingRefundMoney float64, callerIp string) error {
	return RefundTopUpByRemainingRefundAmountWithReference(tradeNo, expectedPaymentProvider, remainingRefundMoney, "", callerIp)
}

func RefundTopUpByRemainingRefundAmountWithReference(tradeNo string, expectedPaymentProvider string, remainingRefundMoney float64, refundReferenceID string, callerIp string) error {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}
	if remainingRefundMoney < 0 {
		return errors.New("剩余可退款金额不能小于 0")
	}

	var topUp TopUp
	if err := DB.Select("trade_no", "payment_provider", "money").
		Where("trade_no = ?", tradeNo).
		First(&topUp).Error; err != nil {
		return ErrTopUpNotFound
	}
	if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
		return ErrPaymentMethodMismatch
	}
	cumulativeRefundMoney := topUp.Money - remainingRefundMoney
	if cumulativeRefundMoney <= 0 {
		return nil
	}
	return RefundTopUpCumulativeWithReference(tradeNo, expectedPaymentProvider, cumulativeRefundMoney, refundReferenceID, callerIp)
}

func RefundTopUpByPaymentIntent(paymentIntentID string, expectedPaymentProvider string, refundMoney float64, callerIp string) error {
	paymentIntentID = strings.TrimSpace(paymentIntentID)
	if paymentIntentID == "" {
		return errors.New("未提供支付意图 ID")
	}

	var topUp TopUp
	if err := DB.Select("trade_no").
		Where("payment_intent_id = ?", paymentIntentID).
		First(&topUp).Error; err != nil {
		return ErrTopUpNotFound
	}
	return RefundTopUp(topUp.TradeNo, expectedPaymentProvider, refundMoney, callerIp)
}

func RefundTopUpByPaymentIntentCumulative(paymentIntentID string, expectedPaymentProvider string, cumulativeRefundMoney float64, callerIp string) error {
	paymentIntentID = strings.TrimSpace(paymentIntentID)
	if paymentIntentID == "" {
		return errors.New("未提供支付意图 ID")
	}
	if cumulativeRefundMoney <= 0 {
		return errors.New("退款金额必须大于 0")
	}

	var topUp TopUp
	if err := DB.Select("trade_no", "refund_amount").
		Where("payment_intent_id = ?", paymentIntentID).
		First(&topUp).Error; err != nil {
		return ErrTopUpNotFound
	}

	refundDelta := cumulativeRefundMoney - topUp.RefundAmount
	if refundDelta <= 0 {
		return nil
	}
	return RefundTopUp(topUp.TradeNo, expectedPaymentProvider, refundDelta, callerIp)
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	return RechargeStripe(referenceId, customerId, 0, callerIp)
}

func RechargeStripe(referenceId string, customerId string, paidMoney float64, callerIp string) (err error) {
	return RechargeStripeWithPaymentIntent(referenceId, customerId, "", paidMoney, callerIp)
}

func RechargeStripeWithPaymentIntent(referenceId string, customerId string, paymentIntentID string, paidMoney float64, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var settlement TopUpSettlement
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		if paidMoney > 0 {
			topUp.Money = paidMoney
		}
		if strings.TrimSpace(paymentIntentID) != "" {
			topUp.PaymentIntentID = strings.TrimSpace(paymentIntentID)
		}
		baseQuota := int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		settlement, err = calculateTopUpSettlementWithPaidAmountTx(tx, topUp, topUp.Amount, paidMoneyToBonusAmount(topUp.Money), baseQuota)
		if err != nil {
			return err
		}

		if err = applyTopUpSettlementTx(tx, topUp, settlement, map[string]interface{}{"stripe_customer": customerId}); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, topUpLogContent("使用在线充值成功", settlement, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var settlement TopUpSettlement
	var payMoney float64
	var paymentMethod string
	completed := false

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		baseQuota := quotaFromTopUpAmount(topUp.Amount)
		if topUp.PaymentProvider == PaymentProviderStripe {
			baseQuota = int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		}
		if topUp.PaymentProvider == PaymentProviderCreem {
			baseQuota = int(topUp.Amount)
		}
		settlement, err := calculateTopUpSettlementWithBaseQuotaTx(tx, topUp, topUp.Amount, baseQuota)
		if err != nil {
			return err
		}
		if err := applyTopUpSettlementTx(tx, topUp, settlement, nil); err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		completed = true
		return nil
	})

	if err != nil {
		return err
	}

	if completed {
		// 事务外记录日志，避免阻塞
		RecordTopupLog(userId, topUpLogContent("管理员补单成功", settlement, payMoney), callerIp, paymentMethod, "admin")
	}
	return nil
}

func CompleteEpayTopUp(tradeNo string, actualPaymentMethod string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var settlement TopUpSettlement
	var topUp TopUp
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(&topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}
		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}
		if strings.TrimSpace(actualPaymentMethod) != "" && topUp.PaymentMethod != actualPaymentMethod {
			topUp.PaymentMethod = actualPaymentMethod
		}
		var err error
		settlement, err = calculateTopUpSettlementTx(tx, &topUp)
		if err != nil {
			return err
		}
		return applyTopUpSettlementTx(tx, &topUp, settlement, nil)
	})
	if err != nil {
		return err
	}
	if settlement.TotalQuota > 0 {
		RecordTopupLog(topUp.UserId, topUpLogContent("使用在线充值成功", settlement, topUp.Money), callerIp, topUp.PaymentMethod, "epay")
	}
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var settlement TopUpSettlement
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		baseQuota := int(topUp.Amount)
		settlement, err = calculateTopUpSettlementWithBaseQuotaTx(tx, topUp, topUp.Amount, baseQuota)
		if err != nil {
			return err
		}

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		if err = applyTopUpSettlementTx(tx, topUp, settlement, updateFields); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, topUpLogContent("使用Creem充值成功", settlement, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var settlement TopUpSettlement
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		settlement, err = calculateTopUpSettlementTx(tx, topUp)
		if err != nil {
			return err
		}
		if err := applyTopUpSettlementTx(tx, topUp, settlement, nil); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if settlement.TotalQuota > 0 {
		RecordTopupLog(topUp.UserId, topUpLogContent("Waffo充值成功", settlement, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeAlipayF2F(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var settlement TopUpSettlement
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderAlipayF2F {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		settlement, err = calculateTopUpSettlementTx(tx, topUp)
		if err != nil {
			return err
		}
		if err := applyTopUpSettlementTx(tx, topUp, settlement, nil); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("alipay f2f topup failed: " + err.Error())
		return err
	}

	if settlement.TotalQuota > 0 {
		RecordTopupLog(topUp.UserId, topUpLogContent("支付宝当面付充值成功", settlement, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodAlipayF2F)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var settlement TopUpSettlement
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		settlement, err = calculateTopUpSettlementTx(tx, topUp)
		if err != nil {
			return err
		}
		if err := applyTopUpSettlementTx(tx, topUp, settlement, nil); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if settlement.TotalQuota > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, topUpLogContent("Waffo Pancake充值成功", settlement, topUp.Money))
	}

	return nil
}
