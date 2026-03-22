package managed

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBroadcasterFanOut(t *testing.T) {
	b := NewBroadcaster()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Send("hello")

	select {
	case msg := <-ch1:
		if msg != "hello" {
			t.Errorf("ch1 got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch1 timed out")
	}

	select {
	case msg := <-ch2:
		if msg != "hello" {
			t.Errorf("ch2 got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch2 timed out")
	}

	b.Unsubscribe(ch1)
	b.Send("world")

	select {
	case msg := <-ch2:
		if msg != "world" {
			t.Errorf("ch2 got %q, want world", msg)
		}
	case <-time.After(time.Second):
		t.Error("ch2 timed out")
	}

	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should not receive after unsubscribe")
		}
		// channel closed — expected
	case <-time.After(100 * time.Millisecond):
	}

	b.Close()
}

func TestBroadcasterClose(t *testing.T) {
	b := NewBroadcaster()
	ch := b.Subscribe()
	b.Close()

	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after broadcaster close")
	}
}

func TestStreamNDJSON(t *testing.T) {
	input := `{"type":"system","subtype":"init"}
{"type":"assistant","content":"hello"}
{"type":"result","cost":0.01}
`
	b := NewBroadcaster()

	var persisted []string
	onLine := func(line string) {
		persisted = append(persisted, line)
	}

	StreamNDJSON(strings.NewReader(input), b, onLine)
	b.Close()

	if len(persisted) != 3 {
		t.Errorf("got %d persisted, want 3", len(persisted))
	}
	if !strings.Contains(persisted[0], "system") {
		t.Errorf("first line should contain 'system', got %s", persisted[0])
	}
}

func TestStreamNDJSONCountsAssistantTurns(t *testing.T) {
	input := `{"type":"assistant","content":"turn 1"}
{"type":"tool_use","name":"Read"}
{"type":"tool_result","output":"file contents"}
{"type":"assistant","content":"turn 2"}
{"type":"assistant","content":"turn 3"}
`
	b := NewBroadcaster()

	var turnCount int
	onLine := func(line string) {
		if strings.Contains(line, `"type":"assistant"`) {
			turnCount++
		}
	}

	StreamNDJSON(strings.NewReader(input), b, onLine)
	b.Close()

	if turnCount != 3 {
		t.Errorf("turnCount=%d, want 3", turnCount)
	}
}

func TestStreamNDJSON_SendsHeartbeats(t *testing.T) {
	oldInterval := HeartbeatInterval
	HeartbeatInterval = 500 * time.Millisecond
	defer func() { HeartbeatInterval = oldInterval }()

	pr, pw := io.Pipe()
	b := NewBroadcaster()
	ch := b.Subscribe()

	var (
		mu         sync.Mutex
		heartbeats []map[string]interface{}
	)

	// Collect messages from the broadcaster in a goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range ch {
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(msg), &obj); err == nil {
				if obj["type"] == "heartbeat" {
					mu.Lock()
					heartbeats = append(heartbeats, obj)
					mu.Unlock()
				}
			}
		}
	}()

	// Run StreamNDJSON in a goroutine; close the pipe after 1s.
	go func() {
		time.Sleep(1 * time.Second)
		pw.Close()
	}()

	StreamNDJSON(pr, b, nil)
	b.Close()
	<-done

	mu.Lock()
	count := len(heartbeats)
	var firstHB map[string]interface{}
	if count > 0 {
		firstHB = heartbeats[0]
	}
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected at least one heartbeat, got none")
	}
	if _, ok := firstHB["ts"]; !ok {
		t.Error("heartbeat missing 'ts' field")
	}
}

func TestStreamNDJSON_OnLineNotCalledForHeartbeats(t *testing.T) {
	oldInterval := HeartbeatInterval
	HeartbeatInterval = 200 * time.Millisecond
	defer func() { HeartbeatInterval = oldInterval }()

	pr, pw := io.Pipe()
	b := NewBroadcaster()

	var (
		mu        sync.Mutex
		callCount int
	)
	onLine := func(line string) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	// Write one real line, then close after 500ms to allow heartbeats to fire.
	go func() {
		pw.Write([]byte("{\"type\":\"result\"}\n"))
		time.Sleep(500 * time.Millisecond)
		pw.Close()
	}()

	StreamNDJSON(pr, b, onLine)
	b.Close()

	mu.Lock()
	got := callCount
	mu.Unlock()

	if got != 1 {
		t.Errorf("onLine called %d times, want exactly 1 (heartbeats must not trigger onLine)", got)
	}
}

func TestStreamNDJSON_BroadcastsLines(t *testing.T) {
	input := `{"type":"result","cost":0.05}` + "\n"
	b := NewBroadcaster()
	ch := b.Subscribe()

	received := make(chan string, 1)
	go func() {
		for msg := range ch {
			received <- msg
			return
		}
	}()

	StreamNDJSON(strings.NewReader(input), b, nil)
	b.Close()

	select {
	case msg := <-received:
		if !strings.Contains(msg, "result") {
			t.Errorf("broadcast message missing expected content, got: %s", msg)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for broadcast message")
	}
}
