package circuit

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go-kafka-eda-demo/pkg/logger"
)

var (
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrTooManyRequests    = errors.New("too many requests")
)

type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

type Counts struct {
	Requests             uint32
	TotalSuccesses       uint32
	TotalFailures        uint32
	ConsecutiveSuccesses uint32
	ConsecutiveFailures  uint32
}

func (c *Counts) onRequest() {
	c.Requests++
}

func (c *Counts) onSuccess() {
	c.TotalSuccesses++
	c.ConsecutiveSuccesses++
	c.ConsecutiveFailures = 0
}

func (c *Counts) onFailure() {
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccesses = 0
}

func (c *Counts) clear() {
	c.Requests = 0
	c.TotalSuccesses = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccesses = 0
	c.ConsecutiveFailures = 0
}

type Settings struct {
	Name                string
	MaxRequests         uint32
	Interval            time.Duration
	Timeout             time.Duration
	ReadyToTrip         func(counts Counts) bool
	OnStateChange       func(name string, from State, to State)
	IsSuccessful        func(err error) bool
}

type CircuitBreaker struct {
	name          string
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(counts Counts) bool
	isSuccessful  func(err error) bool
	onStateChange func(name string, from State, to State)

	mutex      sync.Mutex
	state      State
	generation uint64
	counts     Counts
	expiry     time.Time
}

func NewCircuitBreaker(st Settings) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:        st.Name,
		maxRequests: st.MaxRequests,
		interval:    st.Interval,
		timeout:     st.Timeout,
		readyToTrip: st.ReadyToTrip,
		isSuccessful: st.IsSuccessful,
		onStateChange: st.OnStateChange,
	}

	if cb.readyToTrip == nil {
		cb.readyToTrip = defaultReadyToTrip
	}

	if cb.isSuccessful == nil {
		cb.isSuccessful = defaultIsSuccessful
	}

	cb.toNewGeneration(time.Now())
	return cb
}

func defaultReadyToTrip(counts Counts) bool {
	return counts.ConsecutiveFailures > 5
}

func defaultIsSuccessful(err error) bool {
	return err == nil
}

func (cb *CircuitBreaker) Name() string {
	return cb.name
}

func (cb *CircuitBreaker) State() State {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	return cb.counts
}

func (cb *CircuitBreaker) Execute(ctx context.Context, req func() (interface{}, error)) (interface{}, error) {
	generation, err := cb.beforeRequest(ctx)
	if err != nil {
		return nil, err
	}

	defer func() {
		e := recover()
		if e != nil {
			cb.afterRequest(generation, false)
			panic(e)
		}
	}()

	result, err := req()
	cb.afterRequest(generation, cb.isSuccessful(err))
	return result, err
}

func (cb *CircuitBreaker) beforeRequest(ctx context.Context) (uint64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		logger.Debugf(ctx, "Circuit breaker %s is open", cb.name)
		return generation, ErrCircuitBreakerOpen
	} else if state == StateHalfOpen && cb.counts.Requests >= cb.maxRequests {
		logger.Debugf(ctx, "Circuit breaker %s is half-open with too many requests", cb.name)
		return generation, ErrTooManyRequests
	}

	cb.counts.onRequest()
	return generation, nil
}

func (cb *CircuitBreaker) afterRequest(before uint64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)
	if generation != before {
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

func (cb *CircuitBreaker) onSuccess(state State, now time.Time) {
	cb.counts.onSuccess()

	if state == StateHalfOpen {
		cb.setState(StateClosed, now)
	}
}

func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
	cb.counts.onFailure()

	if cb.readyToTrip(cb.counts) {
		cb.setState(StateOpen, now)
	}
}

func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		if !cb.expiry.IsZero() && cb.expiry.Before(now) {
			cb.toNewGeneration(now)
		}
	case StateOpen:
		if cb.expiry.Before(now) {
			cb.setState(StateHalfOpen, now)
		}
	}
	return cb.state, cb.generation
}

func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state

	cb.toNewGeneration(now)

	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, prev, state)
	}
}

func (cb *CircuitBreaker) toNewGeneration(now time.Time) {
	cb.generation++
	cb.counts.clear()

	var zero time.Time
	switch cb.state {
	case StateClosed:
		if cb.interval == 0 {
			cb.expiry = zero
		} else {
			cb.expiry = now.Add(cb.interval)
		}
	case StateOpen:
		cb.expiry = now.Add(cb.timeout)
	default: // StateHalfOpen
		cb.expiry = zero
	}
}

// ExponentialBackoff implements exponential backoff with jitter
type ExponentialBackoff struct {
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	MaxRetries      int
	Jitter          bool
}

func NewExponentialBackoff(initial, max time.Duration, maxRetries int) *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialInterval: initial,
		MaxInterval:     max,
		Multiplier:      2.0,
		MaxRetries:      maxRetries,
		Jitter:          true,
	}
}

func (eb *ExponentialBackoff) NextBackoff(attempt int) time.Duration {
	if attempt >= eb.MaxRetries {
		return 0 // No more retries
	}

	interval := eb.InitialInterval
	for i := 0; i < attempt; i++ {
		interval = time.Duration(float64(interval) * eb.Multiplier)
		if interval > eb.MaxInterval {
			interval = eb.MaxInterval
			break
		}
	}

	if eb.Jitter {
		// Add jitter: random value between 0.5 and 1.5 times the interval
		jitter := 0.5 + rand.Float64()
		interval = time.Duration(float64(interval) * jitter)
	}

	return interval
}

func (eb *ExponentialBackoff) ShouldRetry(attempt int) bool {
	return attempt < eb.MaxRetries
}

// RetryWithBackoff executes a function with exponential backoff
func RetryWithBackoff(ctx context.Context, backoff *ExponentialBackoff, operation func() error) error {
	var lastErr error
	
	for attempt := 0; attempt < backoff.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err
		
		if attempt == backoff.MaxRetries-1 {
			break
		}

		waitTime := backoff.NextBackoff(attempt)
		logger.Warnf(ctx, "Operation failed (attempt %d/%d), retrying in %v: %v", 
			attempt+1, backoff.MaxRetries, waitTime, err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", backoff.MaxRetries, lastErr)
}

