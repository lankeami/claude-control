package managed

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

// HeartbeatInterval controls how often a heartbeat message is broadcast.
// Override in tests to speed things up.
var HeartbeatInterval = 15 * time.Second

type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan string]struct{}
	closed      bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan string]struct{}),
	}
}

func (b *Broadcaster) Subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[ch] = struct{}{}
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscribers, ch)
	close(ch)
}

func (b *Broadcaster) Send(msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			log.Printf("warning: dropping message for slow subscriber")
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
}

// StreamNDJSON reads NDJSON lines from r, broadcasts each via b, and calls onLine for each.
// onLine is called synchronously per line (use for persistence/turn counting).
// A heartbeat JSON message is broadcast at HeartbeatInterval; onLine is NOT called for heartbeats.
// Blocks until r is closed/EOF.
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string)) {
	type result struct {
		line string
		err  error
	}

	lineCh := make(chan result)

	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			lineCh <- result{line: line}
		}
		if err := scanner.Err(); err != nil {
			lineCh <- result{err: err}
		}
		close(lineCh)
	}()

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-lineCh:
			if !ok {
				return
			}
			if res.err != nil {
				log.Printf("StreamNDJSON scanner error: %v", res.err)
				return
			}
			b.Send(res.line)
			if onLine != nil {
				onLine(res.line)
			}
		case <-ticker.C:
			hb := fmt.Sprintf(`{"type":"heartbeat","ts":%d}`, time.Now().UnixMilli())
			b.Send(hb)
		}
	}
}
