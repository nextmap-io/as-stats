// Package ratelog rate-limits log lines that a single remote can trigger once
// per packet. Flow listeners accept UDP from anywhere, so a misconfigured or
// hostile exporter could otherwise turn the collector log into a firehose.
package ratelog

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Limiter logs at most burst lines per window and reports how many lines were
// suppressed once logging resumes. It keeps no per-source state on purpose:
// keying on the sender's address would be an unbounded map fed by spoofable
// input.
type Limiter struct {
	mu          sync.Mutex
	burst       int
	window      time.Duration
	windowStart time.Time
	logged      int
	suppressed  int
}

// New returns a Limiter allowing burst lines per window.
func New(burst int, window time.Duration) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{burst: burst, window: window}
}

// Printf logs like log.Printf unless the window's budget is spent, in which
// case the line is only counted.
func (l *Limiter) Printf(format string, args ...any) {
	l.mu.Lock()
	now := time.Now()
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= l.window {
		l.windowStart, l.logged = now, 0
	}
	if l.logged >= l.burst {
		l.suppressed++
		l.mu.Unlock()
		return
	}
	l.logged++
	suppressed := l.suppressed
	l.suppressed = 0
	l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if suppressed > 0 {
		msg = fmt.Sprintf("%s (%d similar lines suppressed)", msg, suppressed)
	}
	log.Print(msg)
}
