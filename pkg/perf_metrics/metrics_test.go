package perfmetrics

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPerfMetricsTestDB(t *testing.T) {
	t.Helper()

	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))

	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = oldDB
		hotBuckets.Range(func(key, _ any) bool {
			hotBuckets.Delete(key)
			return true
		})
	})
}

func TestQueryModelGroupSummaryAllMergesBucketsByGroup(t *testing.T) {
	setupPerfMetricsTestDB(t)

	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.PerfMetric{
		ModelName:      "gpt-monitor-a",
		Group:          "default",
		BucketTs:       now - 120,
		RequestCount:   2,
		SuccessCount:   1,
		TotalLatencyMs: 400,
		TtftSumMs:      100,
		TtftCount:      1,
		OutputTokens:   40,
		GenerationMs:   2000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.PerfMetric{
		ModelName:      "gpt-monitor-a",
		Group:          "hidden",
		BucketTs:       now - 120,
		RequestCount:   10,
		SuccessCount:   10,
		TotalLatencyMs: 100,
	}).Error)

	current := &atomicBucket{}
	current.add(Sample{
		Model:        "gpt-monitor-a",
		Group:        "default",
		LatencyMs:    100,
		TtftMs:       50,
		HasTtft:      true,
		Success:      true,
		OutputTokens: 10,
		GenerationMs: 500,
	})
	hotBuckets.Store(bucketKey{
		model:    "gpt-monitor-a",
		group:    "default",
		bucketTs: bucketStart(now),
	}, current)

	result, err := QueryModelGroupSummaryAll(1, []string{"default"})
	require.NoError(t, err)
	require.Len(t, result.Models, 1)

	summary := result.Models[0]
	assert.Equal(t, "gpt-monitor-a", summary.ModelName)
	assert.Equal(t, "default", summary.Group)
	assert.EqualValues(t, 3, summary.RequestCount)
	assert.InDelta(t, 66.67, summary.SuccessRate, 0.01)
	assert.EqualValues(t, 166, summary.AvgLatencyMs)
	assert.EqualValues(t, 75, summary.AvgTtftMs)
	assert.InDelta(t, 20, summary.AvgTps, 0.01)
	assert.NotEmpty(t, summary.RecentSuccessRates)
}

func TestQueryModelGroupSummaryAllKeepsRecentSixtyBuckets(t *testing.T) {
	setupPerfMetricsTestDB(t)

	base := bucketStart(time.Now().Unix())
	for i := 0; i < 65; i++ {
		successCount := int64(1)
		if i%10 == 0 {
			successCount = 0
		}
		require.NoError(t, model.DB.Create(&model.PerfMetric{
			ModelName:      "gpt-monitor-trend",
			Group:          "default",
			BucketTs:       base - int64(i)*3600,
			RequestCount:   1,
			SuccessCount:   successCount,
			TotalLatencyMs: 100,
		}).Error)
	}

	result, err := QueryModelGroupSummaryAll(72, []string{"default"})
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Len(t, result.Models[0].RecentSuccessRates, 60)
}
