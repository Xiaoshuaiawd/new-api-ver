package channel_health_setting

const (
	MinTTFTTimeoutSeconds = 0
	MaxTTFTTimeoutSeconds = 3600
	MinWindowSize         = 10
	MaxWindowSize         = 1000
	MinCooldownAfter      = 1
	MaxCooldownAfter      = 1000
	MinCooldownMinutes    = 1
	MaxCooldownMinutes    = 1440
	MinReferenceTTFTMs    = 100
	MaxReferenceTTFTMs    = 600000
	MinWarmupThreshold    = 1
	MaxWarmupThreshold    = 1000
	MinMultiplierPercent  = 1
	MaxMultiplierPercent  = 99
)

// Enabled controls whether channel health tracking affects weight selection.
var Enabled = true

// TTFTTimeoutSeconds is seconds to wait for the first token before retrying; 0 = disabled.
var TTFTTimeoutSeconds = 0

// WindowSize is the sliding window length for success-rate tracking.
var WindowSize = 50

// CooldownAfter is the number of consecutive failures before cooldown is triggered.
var CooldownAfter = 5

// CooldownDurationMinutes is the cooldown period length in minutes.
var CooldownDurationMinutes = 2

// ReferenceTTFTMs is the reference first-token latency in ms; channels faster than this get factor 1.0.
var ReferenceTTFTMs = 2000

// WarmupThreshold is the minimum number of samples before success-rate penalises a channel.
var WarmupThreshold = 10

// MinMultiplierPct is the minimum multiplier as a percentage (5 means 0.05).
var MinMultiplierPct = 5
