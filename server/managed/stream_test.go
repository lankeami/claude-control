package managed

import (
	"strings"
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

	lines := StreamNDJSON(strings.NewReader(input), b, onLine)
	b.Close()

	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
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
