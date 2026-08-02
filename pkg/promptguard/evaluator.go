package promptguard

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Evaluator calls the guard service and applies the decision matrix.
// It is safe for concurrent use.
type Evaluator struct {
	mu       sync.Mutex
	clients  map[string]*http.Client // keyed by endpoint ID
	global   chan struct{}            // global concurrency limiter
	clock    clock
}

func NewEvaluator() *Evaluator {
	return &Evaluator{
		clients: make(map[string]*http.Client),
		global:  make(chan struct{}, 64),
		clock:   realClock{},
	}
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

	// Global concurrency limit — fail closed when full.
	select {
	case e.global <- struct{}{}:
		defer func() { <-e.global }()
	default:
		return nil, &GuardError{Code: ErrorCodeUnavailable}
	}

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
