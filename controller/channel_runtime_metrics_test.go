package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachChannelRuntimeMetricsAddsCurrentSnapshot(t *testing.T) {
	const channelID = 876_543_210
	attempt := service.BeginChannelRuntimeAttempt(channelID)
	defer attempt.Done()

	channels := []*model.Channel{{Id: channelID}}
	attachChannelRuntimeMetrics(channels)

	require.NotNil(t, channels[0].RuntimeMetrics)
	assert.Equal(t, 1, channels[0].RuntimeMetrics.Concurrency)
	assert.Equal(t, 1, channels[0].RuntimeMetrics.RPM)
}
