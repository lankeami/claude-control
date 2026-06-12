package managed

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func collectLines(t *testing.T, path string, offset int64, dur time.Duration) []string {
	t.Helper()
	old := TranscriptPollInterval
	TranscriptPollInterval = 20 * time.Millisecond
	defer func() { TranscriptPollInterval = old }()

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		TailTranscript(ctx, path, offset, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		})
		close(done)
	}()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return got
}

func TestTailTranscriptReadsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	os.WriteFile(path, []byte("{\"a\":1}\n"), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("{\"b\":2}\n")
		f.Close()
	}()
	got := collectLines(t, path, 0, 500*time.Millisecond)
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != `{"b":2}` {
		t.Fatalf("got %v", got)
	}
}

func TestTailTranscriptWaitsForFileToExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "later.jsonl")
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(path, []byte("{\"x\":1}\n"), 0644)
	}()
	got := collectLines(t, path, 0, 500*time.Millisecond)
	if len(got) != 1 || got[0] != `{"x":1}` {
		t.Fatalf("expected 1 line, got %v", got)
	}
}

func TestTailTranscriptRespectsOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	content := "{\"old\":1}\n"
	os.WriteFile(path, []byte(content), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("{\"new\":2}\n")
		f.Close()
	}()
	got := collectLines(t, path, int64(len(content)), 500*time.Millisecond)
	if len(got) != 1 || got[0] != `{"new":2}` {
		t.Fatalf("got %v", got)
	}
}

func TestTailTranscriptHandlesPartialLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	os.WriteFile(path, []byte(`{"par`), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		f.WriteString("tial\":1}\n")
		f.Close()
	}()
	got := collectLines(t, path, 0, 500*time.Millisecond)
	if len(got) != 1 || got[0] != `{"partial":1}` {
		t.Fatalf("got %v", got)
	}
}

func TestTailTranscriptResetsOnTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	os.WriteFile(path, []byte("{\"a\":1}\n{\"b\":2}\n"), 0644)
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(path, []byte("{\"c\":3}\n"), 0644)
	}()
	got := collectLines(t, path, 0, 500*time.Millisecond)
	want := []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}
	if len(got) != 3 {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
