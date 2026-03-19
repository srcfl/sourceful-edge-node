package gateway

import (
	"log"
	"sync"
	"time"
)

const defaultWatchdogTimeout = 60 * time.Second

// Watchdog monitors heartbeat liveness and triggers a callback on expiry.
type Watchdog struct {
	timeout  time.Duration
	onExpire func()
	mu       sync.Mutex
	lastFeed time.Time
}

// NewWatchdog creates a heartbeat watchdog with the given timeout and expiry callback.
func NewWatchdog(timeout time.Duration, onExpire func()) *Watchdog {
	if timeout <= 0 {
		timeout = defaultWatchdogTimeout
	}
	return &Watchdog{
		timeout:  timeout,
		onExpire: onExpire,
	}
}

// Feed resets the watchdog timer. Called on each heartbeat received.
func (w *Watchdog) Feed() {
	w.mu.Lock()
	w.lastFeed = time.Now()
	w.mu.Unlock()
}

// Run starts the watchdog check loop. Blocks until done is closed.
func (w *Watchdog) Run(done <-chan struct{}) {
	ticker := time.NewTicker(w.timeout / 4)
	defer ticker.Stop()

	expired := false

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			w.mu.Lock()
			lastFeed := w.lastFeed
			w.mu.Unlock()

			if lastFeed.IsZero() {
				continue // Never fed, nothing to watch
			}

			if time.Since(lastFeed) > w.timeout {
				if !expired {
					expired = true
					log.Printf("watchdog: heartbeat expired (timeout=%s, last_feed=%s)", w.timeout, lastFeed.Format(time.RFC3339))
					w.onExpire()
				}
			} else {
				expired = false
			}
		}
	}
}
