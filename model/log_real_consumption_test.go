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

func TestCalculateRealConsumptionCentsUsesFinalWalletQuota(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
	})

	assert.EqualValues(t, 23_000, calculateRealConsumptionCents(115_000_000, nil))
}

func TestCalculateRealConsumptionCentsProratesSubscriptionPrice(t *testing.T) {
	fullPrice := map[string]interface{}{
		"billing_source":        "subscription",
		"subscription_consumed": int64(7_500_000),
		"subscription_total":    int64(100_000_000),
		"admin_info": map[string]interface{}{
			"subscription_price": 32.0,
		},
	}

	assert.EqualValues(t, 240, calculateRealConsumptionCents(0, fullPrice))
	assert.EqualValues(t, 340, calculateRealConsumptionCents(0, map[string]interface{}{
		"billing_source":        "subscription",
		"subscription_consumed": int64(15),
		"subscription_total":    int64(15),
		"admin_info": map[string]interface{}{
			"subscription_price": 3.4,
		},
	}))
}

func TestSumUsedQuotaIncludesRealConsumptionCents(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{
		Type:                 LogTypeConsume,
		Quota:                10,
		RealConsumptionCents: 240,
	}).Error)

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.EqualValues(t, 240, stat.RealConsumptionCents)
}
