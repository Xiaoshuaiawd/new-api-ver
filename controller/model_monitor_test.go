package controller

import (
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMonitorJSONDoesNotExposeRequestCounts(t *testing.T) {
	payload := struct {
		Summary modelMonitorSummary `json:"summary"`
		Groups  []modelMonitorGroup `json:"groups"`
	}{
		Summary: modelMonitorSummary{RequestCount: 123, SuccessRate: 99.9},
		Groups: []modelMonitorGroup{
			{
				Name:    "default",
				Summary: modelMonitorSummary{RequestCount: 456, SuccessRate: 98.8},
				Models: []modelMonitorModel{
					{
						ModelName:    "gpt-monitor-private",
						RequestCount: 789,
						SuccessRate:  97.7,
					},
				},
			},
		},
	}

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "request_count")
}

func TestModelMonitorSortsModelsByStatusWithoutUsingRequestCount(t *testing.T) {
	models := []modelMonitorModel{
		{ModelName: "z-idle", Status: "idle", RequestCount: 9000},
		{ModelName: "b-healthy", Status: "healthy", RequestCount: 7000},
		{ModelName: "c-critical", Status: "critical", RequestCount: 1},
		{ModelName: "a-degraded", Status: "degraded", RequestCount: 2},
	}

	sort.Slice(models, func(i, j int) bool {
		leftRank := modelMonitorStatusRank(models[i].Status)
		rightRank := modelMonitorStatusRank(models[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return models[i].ModelName < models[j].ModelName
	})

	assert.Equal(t, []string{
		"c-critical",
		"a-degraded",
		"b-healthy",
		"z-idle",
	}, []string{
		models[0].ModelName,
		models[1].ModelName,
		models[2].ModelName,
		models[3].ModelName,
	})
}
