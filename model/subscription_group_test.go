package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUserSubscriptionFromPlanTxKeepsUserGroup(t *testing.T) {
	truncateTables(t)

	const userID = 9801
	require.NoError(t, DB.Create(&User{
		Id:       userID,
		Username: "subscription_group_user",
		Status:   common.UserStatusEnabled,
		Group:    "basic",
	}).Error)
	plan := &SubscriptionPlan{
		Id:              9801,
		Title:           "Premium",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		TotalAmount:     1000,
		AvailableGroups: SubscriptionAvailableGroups{"premium", "premium-fast"},
	}
	require.NoError(t, DB.Create(plan).Error)

	sub, err := CreateUserSubscriptionFromPlanTx(DB, userID, plan, "order")
	require.NoError(t, err)
	assert.Equal(t, SubscriptionAvailableGroups{"premium", "premium-fast"}, sub.AvailableGroups)
	assert.Equal(t, "premium", sub.UpgradeGroup)
	assert.Empty(t, sub.PrevUserGroup)

	var user User
	require.NoError(t, DB.Select("group").Where("id = ?", userID).First(&user).Error)
	assert.Equal(t, "basic", user.Group)
}
