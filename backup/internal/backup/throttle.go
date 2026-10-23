package backup

import (
	"context"
	"io"
	"sync"
	"time"
)

// Limiter is a token bucket shared by every upload in a run, so the
// policy's bandwidth cap holds for the whole agent.
type Limiter struct {
	rate   float64 // bytes per second
	burst  float64
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// NewLimiter returns nil (no limit) when bytesPerSec <= 0.
func NewLimiter(bytesPerSec int64) *Limiter {
	if bytesPerSec <= 0 {
		return nil
	}
	r := float64(bytesPerSec)
	return &Limiter{rate: r, burst: max(r, 64<<10), tokens: max(r, 64<<10), last: time.Now()}
}

func (l *Limiter) wait(ctx context.Context, n int) error {
	l.mu.Lock()
	now := time.Now()
	l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	l.last = now
	l.tokens -= float64(n)
	debt := l.tokens
	l.mu.Unlock()
	if debt >= 0 {
		return nil
	}
	t := time.NewTimer(time.Duration(-debt / l.rate * float64(time.Second)))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Reader wraps r so reads are paced by the limiter.
func (l *Limiter) Reader(ctx context.Context, r io.Reader) io.Reader {
	if l == nil {
		return r
	}
	return &limitedReader{ctx: ctx, r: r, l: l}
}

type limitedReader struct {
	ctx context.Context
	r   io.Reader
	l   *Limiter
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if len(p) > 32<<10 {
		p = p[:32<<10]
	}
	n, err := lr.r.Read(p)
	if n > 0 {
		if werr := lr.l.wait(lr.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}
