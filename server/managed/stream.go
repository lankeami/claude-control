package managed

import (
	"bufio"
	"io"
	"log"
	"sync"
)

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
// Returns all lines read. Blocks until r is closed/EOF.
func StreamNDJSON(r io.Reader, b *Broadcaster, onLine func(string)) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lines = append(lines, line)
		b.Send(line)
		if onLine != nil {
			onLine(line)
		}
	}
	return lines
}
