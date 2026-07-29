package main

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	prometheusmetrics "github.com/QuantumNous/new-api/pkg/prometheus_metrics"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskQueueMetricsSourceGroupsBothTaskTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Midjourney{}))

	require.NoError(t, db.Create([]model.Task{
		{TaskID: "suno-wait-1", Platform: constant.TaskPlatformSuno, Status: model.TaskStatusSubmitted},
		{TaskID: "suno-wait-2", Platform: constant.TaskPlatformSuno, Status: model.TaskStatusSubmitted},
		{TaskID: "video-running", Platform: constant.TaskPlatform("15"), Status: model.TaskStatusInProgress},
		{TaskID: "video-success", Platform: constant.TaskPlatform("15"), Status: model.TaskStatusSuccess},
		{TaskID: "video-failure", Platform: constant.TaskPlatform("15"), Status: model.TaskStatusFailure},
	}).Error)
	require.NoError(t, db.Create([]model.Midjourney{
		{MjId: "mj-waiting", Status: "", Progress: "0%"},
		{MjId: "mj-running", Status: string(model.TaskStatusInProgress), Progress: "50%"},
		{MjId: "mj-complete", Status: string(model.TaskStatusSuccess), Progress: "100%"},
	}).Error)

	records, err := taskQueueMetricsSource(db)()
	require.NoError(t, err)
	assert.ElementsMatch(t, []prometheusmetrics.TaskQueueRecord{
		{Source: prometheusmetrics.TaskQueueSourceTasks, Platform: "suno", Status: "SUBMITTED", Count: 2},
		{Source: prometheusmetrics.TaskQueueSourceTasks, Platform: "15", Status: "IN_PROGRESS", Count: 1},
		{Source: prometheusmetrics.TaskQueueSourceMidjourneys, Status: "", Count: 1},
		{Source: prometheusmetrics.TaskQueueSourceMidjourneys, Status: "IN_PROGRESS", Count: 1},
	}, records)
}

func TestTaskQueueMetricsSourceStopsAfterTaskQueryFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = taskQueueMetricsSource(db)()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query task queue counts")
}
