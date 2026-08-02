// Package promptguard implements the Prompt Guard front-door safety check.
// It calls an external OpenAI-compatible guard service before the request
// reaches channel selection, pre-consume billing or any upstream API call.
//
// Guard interaction must be completely decoupled from auto-ban, violation
// counters, quota deduction and account-state mutations: a blocked request
// produces only a 403 response for that single request; repeated blocks do
// not affect the user, token or channel in any way.
package promptguard

import "time"

// ---- Error codes (stable, machine-readable) --------------------------------

const (
	ErrorCodeBlocked     = "prompt_guard_blocked"
	ErrorCodeUnavailable = "prompt_guard_unavailable"
)

// ---- Known scanner / category IDs ------------------------------------------

var AllScannerIDs = []string{
	"violent",
	"non_violent_illegal_acts",
	"sexual_content_or_sexual_acts",
	"pii",
	"suicide_and_self_harm",
	"unethical_acts",
	"politically_sensitive_topics",
	"copyright_violation",
	"jailbreak",
}

// scannerIDSet is a fast lookup used during response validation.
var scannerIDSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(AllScannerIDs))
	for _, id := range AllScannerIDs {
		m[id] = struct{}{}
	}
	return m
}()

func KnownScannerID(id string) bool {
	_, ok := scannerIDSet[id]
	return ok
}

// ---- Decision types --------------------------------------------------------

type DecisionKind string

const (
	DecisionAllow       DecisionKind = "allow"
	DecisionFlag        DecisionKind = "flag"  // controversial but allowed
	DecisionBlock       DecisionKind = "block"
	DecisionUnavailable DecisionKind = "unavailable"
)

// Decision is the result of a single guard evaluation.
type Decision struct {
	Kind      DecisionKind
	ErrorCode string
	// Categories populated for block/flag decisions (scanner IDs from response)
	Categories []string
	LatencyMS  int64
}

// ---- Config types ----------------------------------------------------------

const (
	DefaultTimeoutMS  = 4000
	DefaultTotalMS    = 7000
	DefaultInputLimit = 16000
	MinTimeoutMS      = 500
	MaxTimeoutMS      = 30000
	MinInputLimit     = 128
	MaxInputLimit     = 100000
)

// Endpoint is a single guard service node (runtime, with decrypted token).
type Endpoint struct {
	ID         string
	Name       string
	BaseURL    string
	Model      string
	Token      string
	TimeoutMS  int
	InputLimit int
	Enabled    bool
}

// Config is the validated, decrypted runtime snapshot passed to the evaluator.
type Config struct {
	Enabled                bool
	BlockingEnabled        bool
	LatestTurnOnly         bool
	StorePassEvents        bool
	Scanners               []string // enabled scanner IDs
	AllGroups              bool
	GroupNames             []string // token group names (empty = AllGroups)
	Endpoints              []Endpoint
	ConfigVersion          int64
}

// EnabledEndpoints returns only enabled endpoints.
func (c Config) EnabledEndpoints() []Endpoint {
	out := make([]Endpoint, 0, len(c.Endpoints))
	for _, ep := range c.Endpoints {
		if ep.Enabled {
			out = append(out, ep)
		}
	}
	return out
}

// IncludesGroup returns true when the config covers the given token group.
func (c Config) IncludesGroup(tokenGroup string) bool {
	if c.AllGroups {
		return true
	}
	for _, g := range c.GroupNames {
		if g == tokenGroup {
			return true
		}
	}
	return false
}

// minimumInputLimit returns the smallest InputLimit across all endpoints.
func minimumInputLimit(endpoints []Endpoint) int {
	limit := DefaultInputLimit
	for i, ep := range endpoints {
		v := ep.InputLimit
		if v <= 0 {
			v = DefaultInputLimit
		}
		if i == 0 || v < limit {
			limit = v
		}
	}
	return limit
}

// ---- Guard service response ------------------------------------------------

// guardResponse is the only accepted format from the guard model.
type guardResponse struct {
	Safety     string   `json:"safety"`
	Categories []string `json:"categories"`
}

var validSafetyValues = map[string]struct{}{
	"Safe":          {},
	"Controversial": {},
	"Unsafe":        {},
}

// ---- Snapshot (text to evaluate) -------------------------------------------

// Snapshot holds the text to be evaluated plus request metadata for logging.
// No secrets, full prompt text, auth headers or API keys are stored here.
type Snapshot struct {
	// ScanText is the (possibly truncated) text sent to the guard.
	ScanText string
	// PromptLength is the original input length in Unicode chars, for logging.
	PromptLength int
	// RequestID for log correlation.
	RequestID string
	// TokenGroup for group-inclusion check and logging.
	TokenGroup string
	// ModelName for logging only.
	ModelName string
}

// ---- Timing ----------------------------------------------------------------

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
