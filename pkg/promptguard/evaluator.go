package promptguard

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Evaluator calls the guard service and applies the decision matrix.
// It is safe for concurrent use.
type Evaluator struct {
	mu      sync.Mutex
	clients map[string]*http.Client // keyed by endpoint ID
	clock   clock

	// semMu guards sem/semCap so MaxConcurrency can be re-sized at runtime.
	semMu  sync.Mutex
	sem    *semaphore.Weighted
	semCap int64
}

func NewEvaluator() *Evaluator {
	return &Evaluator{
		clients: make(map[string]*http.Client),
		clock:   realClock{},
		sem:     semaphore.NewWeighted(DefaultMaxConcurrency),
		semCap:  DefaultMaxConcurrency,
	}
}

// acquireSlot returns the semaphore sized to the desired concurrency, resizing
// it when the configured limit changed. Callers Acquire(ctx, 1) on the returned
// semaphore so they WAIT for a slot within the request's timeout budget instead
// of failing closed immediately when the limit is momentarily saturated.
func (e *Evaluator) acquireSlot(desired int) *semaphore.Weighted {
	if desired <= 0 {
		desired = DefaultMaxConcurrency
	}
	e.semMu.Lock()
	defer e.semMu.Unlock()
	if int64(desired) != e.semCap {
		// Resize: a fresh semaphore. In-flight holders on the old semaphore
		// release against it harmlessly; new arrivals use the new one.
		e.sem = semaphore.NewWeighted(int64(desired))
		e.semCap = int64(desired)
	}
	return e.sem
}

// defaultEvaluator is the package-level singleton.
var defaultEvaluator = NewEvaluator()

// Evaluate runs the guard check. It must be called before channel selection,
// pre-consume billing and upstream dispatch. On block it returns a non-nil
// Decision with Kind==DecisionBlock. On unavailability (fail-closed) it returns
// Kind==DecisionUnavailable. On allow/flag it returns Kind==DecisionAllow or
// DecisionFlag.
//
// The caller MUST check Decision.Kind before proceeding with the request.
func Evaluate(ctx context.Context, cfg Config, snap Snapshot) (*Decision, error) {
	return defaultEvaluator.Evaluate(ctx, cfg, snap)
}

func (e *Evaluator) Evaluate(ctx context.Context, cfg Config, snap Snapshot) (*Decision, error) {
	start := e.clock.Now()

	if strings.TrimSpace(snap.ScanText) == "" {
		return &Decision{Kind: DecisionAllow}, nil
	}

	endpoints := cfg.EnabledEndpoints()
	if len(endpoints) == 0 {
		return nil, &GuardError{Code: ErrorCodeUnavailable}
	}

	// Global concurrency limit. Instead of failing closed the instant the limit
	// is hit, wait for a slot within the request's overall timeout budget (ctx).
	// Only when we cannot get a slot before the deadline do we fail closed —
	// that indicates sustained guard saturation, not a transient spike.
	sem := e.acquireSlot(cfg.MaxConcurrency)
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Timeout: true, Cause: err}
	}
	defer sem.Release(1)

	// Per-attempt timeout for the actual guard call, bounded by the remaining ctx.
	timeout := time.Duration(endpoints[0].TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultTimeoutMS * time.Millisecond
	}
	evalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := e.scanWithFailover(evalCtx, endpoints, snap.ScanText, cfg.SystemPrompt)
	if err != nil {
		return nil, err
	}

	decision := applyDecisionMatrix(result, cfg.Scanners)
	decision.LatencyMS = e.clock.Now().Sub(start).Milliseconds()
	return decision, nil
}

// scanWithFailover tries enabled endpoints in order; retryable errors advance
// to the next endpoint. Non-retryable errors (invalid response) propagate immediately.
func (e *Evaluator) scanWithFailover(ctx context.Context, endpoints []Endpoint, text string, systemPrompt string) (*guardResponse, error) {
	var lastErr error
	for _, ep := range endpoints {
		client := e.getClient(ep)
		result, err := callGuardEndpoint(ctx, client, ep, text, systemPrompt)
		if err == nil {
			return result, nil
		}
		lastErr = err
		var guardErr *GuardError
		if errors.As(err, &guardErr) && !guardErr.Retryable {
			return nil, err
		}
		// context cancelled/timed out — stop immediately
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, &GuardError{Code: ErrorCodeUnavailable, Retryable: false, Timeout: true, Cause: err}
		}
	}
	if lastErr == nil {
		lastErr = &GuardError{Code: ErrorCodeUnavailable}
	}
	return nil, lastErr
}

func (e *Evaluator) getClient(ep Endpoint) *http.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.clients[ep.ID]; ok {
		return c
	}
	c := newHTTPClient(ep.TimeoutMS)
	e.clients[ep.ID] = c
	return c
}

// InvalidateClient removes the cached HTTP client for an endpoint (called
// after config update so new timeouts take effect).
func (e *Evaluator) InvalidateClient(endpointID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.clients, endpointID)
}

// InvalidateAllClients clears the entire client cache.
func InvalidateAllClients() {
	defaultEvaluator.mu.Lock()
	defer defaultEvaluator.mu.Unlock()
	defaultEvaluator.clients = make(map[string]*http.Client)
}
