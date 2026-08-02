// Package channelhealth tracks per-channel runtime health for dynamic weight adjustment.
package channelhealth

import (
	"math"
	"sync"
	"time"
)

// ---- Config -----------------------------------------------------------------

// Config holds tunable parameters. Zero values are replaced by defaults on Configure.
type Config struct {
	Enabled          bool
	WindowSize       int
	EWMAAlpha        float64
	ReferenceTTFTMs  float64
	CooldownAfter    int
	CooldownDuration time.Duration
	WarmupThreshold  int
	MinMultiplier    float64
	TTFTTimeout      time.Duration // 0 = disabled
}

const maxWindowSize = 1024

func defaultConfig() Config {
	return Config{
		Enabled:          true,
		WindowSize:       50,
		EWMAAlpha:        0.15,
		ReferenceTTFTMs:  2000,
		CooldownAfter:    5,
		CooldownDuration: 2 * time.Minute,
		WarmupThreshold:  10,
		MinMultiplier:    0.05,
		TTFTTimeout:      0,
	}
}

var (
	globalConfigMu sync.RWMutex
	globalConfig   = defaultConfig()
)

// Configure atomically replaces the active config. Zero values fall back to defaults.
func Configure(cfg Config) {
	d := defaultConfig()
	if cfg.WindowSize <= 0 || cfg.WindowSize > maxWindowSize {
		cfg.WindowSize = d.WindowSize
	}
	if cfg.EWMAAlpha <= 0 || cfg.EWMAAlpha > 1 || math.IsNaN(cfg.EWMAAlpha) {
		cfg.EWMAAlpha = d.EWMAAlpha
	}
	if cfg.ReferenceTTFTMs <= 0 || math.IsNaN(cfg.ReferenceTTFTMs) {
		cfg.ReferenceTTFTMs = d.ReferenceTTFTMs
	}
	if cfg.CooldownAfter <= 0 {
		cfg.CooldownAfter = d.CooldownAfter
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = d.CooldownDuration
	}
	if cfg.WarmupThreshold <= 0 {
		cfg.WarmupThreshold = d.WarmupThreshold
	}
	if cfg.MinMultiplier <= 0 || math.IsNaN(cfg.MinMultiplier) {
		cfg.MinMultiplier = d.MinMultiplier
	} else if cfg.MinMultiplier > 1 {
		cfg.MinMultiplier = 1
	}
	if cfg.TTFTTimeout < 0 {
		cfg.TTFTTimeout = 0
	}
	globalConfigMu.Lock()
	globalConfig = cfg
	globalConfigMu.Unlock()
}

// GetConfig returns a snapshot of the active config.
func GetConfig() Config {
	globalConfigMu.RLock()
	defer globalConfigMu.RUnlock()
	return globalConfig
}

// ---- types ------------------------------------------------------------------

type entry struct {
	// sliding window — fixed-size backing array; effectiveWindowSize comes from cfg
	window    [maxWindowSize]bool // true = success
	windowPos int
	filled    int // capped at effectiveWindowSize

	// TTFT EWMA
	ewmaTTFT float64
	hasTTFT  bool

	// cooldown
	coolUntil time.Time

	// consecutive failure streak (reset on any success)
	streak int
}

type Tracker struct {
	mu        sync.Mutex
	store     map[int]*entry
	now       func() time.Time
	getConfig func() Config
}

// ---- package-level default --------------------------------------------------

var defaultTracker = NewTracker(time.Now)

// Record records the outcome of a relay attempt for channelID.
func Record(channelID int, success bool, ttftMs int64) {
	defaultTracker.Record(channelID, success, ttftMs)
}

// Multiplier returns a value in [0, 1] representing how healthy channelID is.
func Multiplier(channelID int) float64 {
	return defaultTracker.Multiplier(channelID)
}

// Snapshot returns a read-only view of all tracked channels for diagnostics.
func Snapshot() map[int]HealthSnapshot {
	return defaultTracker.Snapshot()
}

// ---- Tracker ----------------------------------------------------------------

func NewTracker(now func() time.Time) *Tracker {
	return &Tracker{store: make(map[int]*entry), now: now, getConfig: GetConfig}
}

func (t *Tracker) Record(channelID int, success bool, ttftMs int64) {
	if channelID <= 0 {
		return
	}
	cfg := t.getConfig()
	if !cfg.Enabled {
		return
	}
	winSize := cfg.WindowSize

	t.mu.Lock()
	e := t.getOrCreate(channelID)

	// sliding window
	e.window[e.windowPos] = success
	e.windowPos = (e.windowPos + 1) % winSize
	if e.filled < winSize {
		e.filled++
	}

	// consecutive streak
	if success {
		e.streak = 0
	} else {
		e.streak++
		if e.streak >= cfg.CooldownAfter {
			e.coolUntil = t.now().Add(cfg.CooldownDuration)
			e.streak = 0
		}
	}

	// TTFT EWMA
	if ttftMs > 0 {
		ms := float64(ttftMs)
		if !e.hasTTFT {
			e.ewmaTTFT = ms
			e.hasTTFT = true
		} else {
			e.ewmaTTFT = cfg.EWMAAlpha*ms + (1-cfg.EWMAAlpha)*e.ewmaTTFT
		}
	}

	t.mu.Unlock()
}

func (t *Tracker) Multiplier(channelID int) float64 {
	cfg := t.getConfig()
	if !cfg.Enabled {
		return 1.0
	}
	if channelID <= 0 {
		return 1.0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.store[channelID]
	if !ok {
		return 1.0
	}
	return multiplierFromEntry(e, t.now(), cfg)
}

// multiplierFromEntry computes the health multiplier from an entry.
// Caller must hold t.mu.
func multiplierFromEntry(e *entry, now time.Time, cfg Config) float64 {
	if now.Before(e.coolUntil) {
		return 0.0
	}
	if e.filled < cfg.WarmupThreshold {
		return 1.0
	}
	var wins int
	winSize := cfg.WindowSize
	for i := 0; i < winSize; i++ {
		if e.window[i] {
			wins++
		}
	}
	// e.filled tracks how many slots have been written so far, capped at the
	// windowSize that was active when each sample was recorded. If WindowSize
	// shrinks via Configure(), some filled slots beyond the new winSize are
	// no longer scanned, so cap count to winSize to keep the denominator sane.
	count := e.filled
	if count > winSize {
		count = winSize
	}
	successRate := float64(wins) / float64(count)
	srFactor := math.Pow(successRate, 1.5)

	ttftFactor := 1.0
	if e.hasTTFT && e.ewmaTTFT > 0 {
		ttftFactor = math.Min(1.0, cfg.ReferenceTTFTMs/e.ewmaTTFT)
	}

	m := srFactor * ttftFactor
	if m < cfg.MinMultiplier {
		m = cfg.MinMultiplier
	}
	return m
}

// ---- HealthSnapshot (diagnostics) ------------------------------------------

type HealthSnapshot struct {
	ChannelID   int
	SuccessRate float64
	EWMATTFTMs  float64
	HasTTFT     bool
	InCooldown  bool
	CoolUntil   time.Time
	Multiplier  float64
	SampleCount int
}

func (t *Tracker) Snapshot() map[int]HealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	cfg := t.getConfig()
	now := t.now()
	out := make(map[int]HealthSnapshot, len(t.store))
	for id, e := range t.store {
		var wins int
		winSize := cfg.WindowSize
		for i := 0; i < winSize; i++ {
			if e.window[i] {
				wins++
			}
		}
		sr := 0.0
		if e.filled > 0 {
			count := e.filled
			if count > winSize {
				count = winSize
			}
			sr = float64(wins) / float64(count)
		}
		inCooldown := now.Before(e.coolUntil)
		out[id] = HealthSnapshot{
			ChannelID:   id,
			SuccessRate: sr,
			EWMATTFTMs:  e.ewmaTTFT,
			HasTTFT:     e.hasTTFT,
			InCooldown:  inCooldown,
			CoolUntil:   e.coolUntil,
			SampleCount: e.filled,
			Multiplier:  multiplierFromEntry(e, now, cfg),
		}
	}
	return out
}

// ---- internal helpers -------------------------------------------------------

func (t *Tracker) getOrCreate(id int) *entry {
	e, ok := t.store[id]
	if !ok {
		e = &entry{}
		t.store[id] = e
	}
	return e
}
