package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionMapTogglesLogBodyCaptureEnabled(t *testing.T) {
	previousEnabled := common.LogBodyCaptureEnabled
	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		common.LogBodyCaptureEnabled = previousEnabled
	})

	require.NoError(t, updateOptionMap("LogBodyCaptureEnabled", "true"))
	assert.True(t, common.LogBodyCaptureEnabled)

	require.NoError(t, updateOptionMap("LogBodyCaptureEnabled", "false"))
	assert.False(t, common.LogBodyCaptureEnabled)

	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "false", common.OptionMap["LogBodyCaptureEnabled"])
	common.OptionMapRWMutex.RUnlock()
}
