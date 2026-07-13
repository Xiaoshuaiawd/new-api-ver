package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedRedemptionUser(t *testing.T, userId int, quota int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id:       userId,
		Username: "redemption_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}).Error)
}

func TestRedeemQuotaCodeKeepsLegacyBehavior(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 501, 100)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "legacy quota",
		Key:         "legacy-quota-key",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       250,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	result, err := Redeem("legacy-quota-key", 501)
	require.NoError(t, err)
	assert.Equal(t, RedemptionRewardTypeQuota, result.Type)
	assert.Equal(t, 250, result.Quota)

	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", 501).First(&user).Error)
	assert.Equal(t, 350, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.Where("key = ?", "legacy-quota-key").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, 501, redemption.UsedUserId)
}

func TestRedeemSubscriptionCodeCreatesUserSubscriptionFromPlan(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 502, 100)
	plan := &SubscriptionPlan{
		Id:                      701,
		Title:                   "Codex Day",
		PriceAmount:             32,
		Currency:                "CNY",
		DurationUnit:            SubscriptionDurationDay,
		DurationValue:           1,
		Enabled:                 true,
		TotalAmount:             200000,
		QuotaResetPeriod:        SubscriptionResetDaily,
		AvailableGroups:         SubscriptionAvailableGroups{"Codex", "Codex-combo"},
		MaxPurchasePerUser:      1,
		QuotaResetCustomSeconds: 0,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "subscription code",
		Key:         "subscription-key",
		Status:      common.RedemptionCodeStatusEnabled,
		RewardType:  RedemptionRewardTypeSubscription,
		PlanId:      plan.Id,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	result, err := Redeem("subscription-key", 502)
	require.NoError(t, err)
	assert.Equal(t, RedemptionRewardTypeSubscription, result.Type)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, plan.Title, result.PlanTitle)
	assert.Equal(t, 0, result.Quota)

	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", 502).First(&user).Error)
	assert.Equal(t, 100, user.Quota)

	var sub UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", 502, plan.Id).First(&sub).Error)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, "redemption", sub.Source)
	assert.Equal(t, plan.TotalAmount, sub.AmountTotal)
	assert.Equal(t, SubscriptionAvailableGroups{"Codex", "Codex-combo"}, sub.AvailableGroups)
	assert.Equal(t, "Codex", sub.UpgradeGroup)
	assert.Greater(t, sub.EndTime, sub.StartTime)
	assert.Greater(t, sub.NextResetTime, int64(0))
}

func TestRedeemSubscriptionCodeRejectsInvalidPlan(t *testing.T) {
	truncateTables(t)

	seedRedemptionUser(t, 503, 100)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "bad subscription code",
		Key:         "bad-subscription-key",
		Status:      common.RedemptionCodeStatusEnabled,
		RewardType:  RedemptionRewardTypeSubscription,
		PlanId:      9999,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	_, err := Redeem("bad-subscription-key", 503)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", 503).Count(&count).Error)
	assert.Zero(t, count)

	var redemption Redemption
	require.NoError(t, DB.Where("key = ?", "bad-subscription-key").First(&redemption).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestSearchRedemptionsFindsByCode(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&Redemption{
		Name:        "search by code",
		Key:         "CODE-SEARCH-123456",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&Redemption{
		Name:        "other code",
		Key:         "OTHER-CODE-654321",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}).Error)

	redemptions, total, err := SearchRedemptions("CODE-SEARCH", "", 0, 10)

	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, redemptions, 1)
	assert.Equal(t, "CODE-SEARCH-123456", redemptions[0].Key)
}

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{name: "no filters returns all rows", num: 10, wantTotal: 5, wantIds: []int{5, 4, 3, 2, 1}},
		{name: "keyword filters by name prefix", keyword: "alpha", num: 10, wantTotal: 3, wantIds: []int{3, 2, 1}},
		{name: "enabled status excludes expired rows", status: "1", num: 10, wantTotal: 2, wantIds: []int{2, 1}},
		{name: "expired status returns enabled expired rows", status: "expired", num: 10, wantTotal: 1, wantIds: []int{3}},
		{name: "disabled status", status: "2", num: 10, wantTotal: 1, wantIds: []int{4}},
		{name: "used status", status: "3", num: 10, wantTotal: 1, wantIds: []int{5}},
		{name: "pagination keeps unpaged total", startIdx: 1, num: 2, wantTotal: 5, wantIds: []int{4, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	result, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, result.Quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}
