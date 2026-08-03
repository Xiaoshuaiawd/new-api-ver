/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUserSubscriptionSnapshotsPlanPrice(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username: "subscription-price-snapshot",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)

	plan := &SubscriptionPlan{
		Title:         "200 quota",
		PriceAmount:   32,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
	}
	require.NoError(t, DB.Create(plan).Error)

	subscription, err := CreateUserSubscriptionFromPlanTx(DB, user.Id, plan, "test")
	require.NoError(t, err)
	assert.Equal(t, 32.0, subscription.PriceAmount)
}

func TestBackfillLegacyUserSubscriptionPriceAmounts(t *testing.T) {
	truncateTables(t)

	plan := &SubscriptionPlan{
		Title:         "legacy 200 quota",
		PriceAmount:   32,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   200,
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		UserId:      1,
		PlanId:      plan.Id,
		AmountTotal: 200,
		Status:      "expired",
	}).Error)

	require.NoError(t, backfillLegacyUserSubscriptionPriceAmounts())

	var subscription UserSubscription
	require.NoError(t, DB.Where("plan_id = ?", plan.Id).First(&subscription).Error)
	assert.Equal(t, 32.0, subscription.PriceAmount)
}
