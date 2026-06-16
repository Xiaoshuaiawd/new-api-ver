package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGetFullRequestURLTrimsClientV1WhenBaseURLAlreadyHasVersionPath(t *testing.T) {
	got := GetFullRequestURL(
		"https://ark.cn-beijing.volces.com/api/coding/v3",
		"/v1/responses",
		constant.ChannelTypeOpenAI,
	)

	require.Equal(t, "https://ark.cn-beijing.volces.com/api/coding/v3/responses", got)
}

func TestGetFullRequestURLKeepsClientV1ForRootOpenAIBaseURL(t *testing.T) {
	got := GetFullRequestURL(
		"https://api.openai.com",
		"/v1/responses",
		constant.ChannelTypeOpenAI,
	)

	require.Equal(t, "https://api.openai.com/v1/responses", got)
}

func TestGetFullRequestURLTrimsClientV1WithVersionPathAndQuery(t *testing.T) {
	got := GetFullRequestURL(
		"https://ark.cn-beijing.volces.com/api/coding/v3",
		"/v1/responses?foo=bar",
		constant.ChannelTypeOpenAI,
	)

	require.Equal(t, "https://ark.cn-beijing.volces.com/api/coding/v3/responses?foo=bar", got)
}
