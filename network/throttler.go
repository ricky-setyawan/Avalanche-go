package network

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"golang.org/x/time/rate"
)

var errConnAttemptCancelled = errors.New("connection attempt cancelled")

type backoffPolicy interface {
	backoff(attempt int)
}

type staticBackoffPolicy struct {
	backoffDuration time.Duration
}

func (p staticBackoffPolicy) getBackoffDuration() time.Duration {
	return p.backoffDuration
}

func (p staticBackoffPolicy) backoff(_ int) {
	time.Sleep(p.getBackoffDuration())
}

type incrementalBackoffPolicy struct {
	backoffDuration   time.Duration
	incrementDuration time.Duration
}

func (n incrementalBackoffPolicy) getBackoffDuration(attempt int) time.Duration {
	incrementDurationMillis := n.getIncrementDuration().Milliseconds()
	backoffDurationMillis := n.backoffDuration.Milliseconds()
	sleepMillis := backoffDurationMillis + (incrementDurationMillis * int64(attempt))
	return time.Duration(sleepMillis) * time.Millisecond
}

func (n incrementalBackoffPolicy) getIncrementDuration() time.Duration {
	return n.incrementDuration
}

func (n incrementalBackoffPolicy) backoff(attempt int) {
	time.Sleep(n.getBackoffDuration(attempt))
}

type randomisedBackoffPolicy struct {
	minDuration time.Duration
	maxDuration time.Duration
}

// getBackoffDuration If this function is called outside of the `backoff` method, its value
// (randomised) is not the one to be used when the actual Backoff happens since the Backoff method
// calls this internally.
func (r randomisedBackoffPolicy) getBackoffDuration() time.Duration {
	randMillis := rand.Float64() * float64(r.maxDuration-r.minDuration)
	return r.minDuration + time.Duration(randMillis)
}

func (r randomisedBackoffPolicy) backoff(_ int) {
	time.Sleep(r.getBackoffDuration())
}

type Throttler interface {
	// Block until the event associated with this Acquire can happen.
	// If [ctx] is cancelled, gives up and returns an error.
	Acquire(ctx context.Context) error
}

type waitingThrottler struct {
	limiter *rate.Limiter
}

type backoffThrottler struct {
	limiter       *rate.Limiter
	backoffPolicy backoffPolicy
}

type noThrottler struct{}

func (w waitingThrottler) Acquire(ctx context.Context) error {
	return w.limiter.Wait(ctx)
}

func (t backoffThrottler) Acquire(ctx context.Context) error {
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return errConnAttemptCancelled
		default:
		}
		if t.limiter.Allow() {
			break
		}

		// TODO: Stop sleeping if [ctx] is cancelled
		t.backoffPolicy.backoff(attempt)
		attempt += 1
	}

	return nil
}

func (t noThrottler) Acquire(context.Context) error {
	return nil
}

func NewBackoffThrottler(throttleLimit int, backoffPolicy backoffPolicy) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: backoffPolicy,
	}
}

func NewWaitingThrottler(throttleLimit int) Throttler {
	return waitingThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
	}
}

func NewNoThrottler() Throttler {
	return noThrottler{}
}

func NewStaticBackoffThrottler(throttleLimit int, backOffDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: staticBackoffPolicy{backoffDuration: backOffDuration},
	}
}

func NewIncrementalBackoffThrottler(throttleLimit int, backOffDuration time.Duration, incrementDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter:       rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: incrementalBackoffPolicy{backoffDuration: backOffDuration, incrementDuration: incrementDuration},
	}
}

func NewRandomisedBackoffThrottler(throttleLimit int, minDuration, maxDuration time.Duration) Throttler {
	return backoffThrottler{
		limiter: rate.NewLimiter(rate.Limit(throttleLimit), throttleLimit),
		backoffPolicy: randomisedBackoffPolicy{
			minDuration: minDuration,
			maxDuration: maxDuration,
		},
	}
}
