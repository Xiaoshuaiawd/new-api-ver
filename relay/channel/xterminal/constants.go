package xterminal

const (
	ChannelName = "xterminal"

	// DefaultBaseURL is the fixed upstream this channel talks to. It is also
	// the value constant.ChannelBaseURLs reports, so a channel created without
	// an API address still resolves to a usable endpoint.
	DefaultBaseURL = "https://api.xterminal.cn/chatv2"

	// ChatCompletionsPath is the only relay path the upstream serves.
	ChatCompletionsPath = "/v1/chat/completions"

	// tokenPoolSize is the number of tokens kept ready per channel.
	tokenPoolSize = 5

	// maxTokenAttempts bounds how many pooled tokens a single relay request
	// may burn before the upstream error is handed back to the caller.
	maxTokenAttempts = 3
)

// offlineTokenURL mints a bearer token for a random userId.
const offlineTokenURL = "https://api.xterminal.cn/user/offline"

// offlineUserAgent mirrors the desktop client so the token endpoint sees the
// same caller profile as the official application.
const offlineUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) xterminal/5.8.6 Chrome/144.0.7559.177 Electron/40.6.0 Safari/537.36"

// ModelList is empty because models are supplied by the administrator or
// fetched dynamically; the upstream exposes no public model catalogue.
var ModelList = []string{}
