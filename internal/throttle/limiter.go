package throttle

import (
	"sync"
	"time"
)

type Limiter struct {
	maxPerSecond int
	mu           sync.Mutex
	tokens       int
	lastRefill   time.Time
}

func New(maxPerSecond int) *Limiter {
	if maxPerSecond <= 0 {
		return nil
	}
	return &Limiter{
		maxPerSecond: maxPerSecond,
		tokens:       maxPerSecond,
		lastRefill:   time.Now(),
	}
}

func (l *Limiter) Wait(n int) {
	if l == nil || l.maxPerSecond <= 0 {
		return
	}
	for n > 0 {
		l.mu.Lock()
		l.refill()
		take := n
		if take > l.tokens {
			take = l.tokens
		}
		l.tokens -= take
		n -= take
		l.mu.Unlock()

		if n > 0 {
			sleepDuration := time.Second / time.Duration(l.maxPerSecond)
			if sleepDuration < time.Millisecond {
				sleepDuration = time.Millisecond
			}
			time.Sleep(sleepDuration)
		}
	}
}

func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefill)
	if elapsed <= 0 {
		return
	}
	add := int(elapsed.Seconds() * float64(l.maxPerSecond))
	if add > 0 {
		l.tokens += add
		if l.tokens > l.maxPerSecond {
			l.tokens = l.maxPerSecond
		}
		l.lastRefill = now
	}
}
