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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateRealConsumptionAmountUsesActualQuotaAndGroupRatio(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
	})

	amount := calculateRealConsumptionAmount(115_000_000, map[string]interface{}{
		"group_ratio": 0.23,
	})
	assert.True(t, amount.Equal(decimal.NewFromInt(230)))
}

func TestCalculateRealConsumptionAmountProratesSubscriptionPrice(t *testing.T) {
	fullPrice := map[string]interface{}{
		"billing_source":        "subscription",
		"subscription_consumed": int64(7_500_000),
		"subscription_total":    int64(100_000_000),
		"admin_info": map[string]interface{}{
			"subscription_price": 32.0,
		},
	}

	assert.True(t, calculateRealConsumptionAmount(0, fullPrice).Equal(decimal.RequireFromString("2.4")))
	assert.True(t, calculateRealConsumptionAmount(0, map[string]interface{}{
		"billing_source":        "subscription",
		"subscription_consumed": int64(15),
		"subscription_total":    int64(15),
		"admin_info": map[string]interface{}{
			"subscription_price": 3.4,
		},
	}).Equal(decimal.RequireFromString("3.4")))
}

func TestSumUsedQuotaAggregatesRealConsumptionBeforeRounding(t *testing.T) {
	truncateTables(t)
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
	})
	for range 2 {
		require.NoError(t, LOG_DB.Create(&Log{
			Type:  LogTypeConsume,
			Quota: 3_000,
			Other: `{"group_ratio":1}`,
		}).Error)
	}

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.InDelta(t, 0.012, stat.RealConsumption, 0.000001)
}
