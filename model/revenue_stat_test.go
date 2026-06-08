package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestSumRevenueByTimeRangeCountsSubscriptionAndWalletWithoutMirrorTopUp(t *testing.T) {
	truncateTables(t)

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	tomorrowStart := todayStart + 24*60*60
	yesterday := todayStart - 1

	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:       1,
		PlanId:       1,
		Money:        32,
		TradeNo:      "sub-today",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 60,
		CompleteTime: todayStart + 120,
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:       1,
		Money:        32,
		TradeNo:      "sub-today",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 60,
		CompleteTime: todayStart + 120,
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:       2,
		Money:        10,
		TradeNo:      "wallet-today",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   todayStart + 180,
		CompleteTime: todayStart + 240,
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{
		UserId:       3,
		PlanId:       2,
		Money:        99,
		TradeNo:      "sub-yesterday",
		Status:       common.TopUpStatusSuccess,
		CreateTime:   yesterday,
		CompleteTime: yesterday,
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:       4,
		Money:        88,
		TradeNo:      "wallet-pending",
		Status:       common.TopUpStatusPending,
		CreateTime:   todayStart + 300,
		CompleteTime: todayStart + 360,
	}).Error)

	revenue, err := SumRevenueByTimeRange(todayStart, tomorrowStart)

	require.NoError(t, err)
	require.Equal(t, 42.0, revenue)
}
