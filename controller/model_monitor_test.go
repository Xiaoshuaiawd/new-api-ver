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
				Name:         "default",
				RequestCount: 456,
				SuccessRate:  98.8,
				Status:       "degraded",
			},
		},
	}

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "request_count")
	assert.NotContains(t, string(data), "models")
	assert.NotContains(t, string(data), "model_name")
}

func TestModelMonitorSortsGroupsByStatusWithoutUsingRequestCount(t *testing.T) {
	groups := []modelMonitorGroup{
		{Name: "z-idle", Status: "idle", RequestCount: 9000},
		{Name: "b-healthy", Status: "healthy", RequestCount: 7000},
		{Name: "c-critical", Status: "critical", RequestCount: 1},
		{Name: "a-degraded", Status: "degraded", RequestCount: 2},
	}

	sort.Slice(groups, func(i, j int) bool {
		leftRank := modelMonitorStatusRank(groups[i].Status)
		rightRank := modelMonitorStatusRank(groups[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return groups[i].Name < groups[j].Name
	})

	assert.Equal(t, []string{
		"c-critical",
		"a-degraded",
		"b-healthy",
		"z-idle",
	}, []string{
		groups[0].Name,
		groups[1].Name,
		groups[2].Name,
		groups[3].Name,
	})
}
