package db

import (
	"testing"
	"time"
)

func TestFiveHourWindowBoundary(t *testing.T) {
	// Test that we correctly identify which 5-hour window a timestamp falls in
	// Example: 2026-05-19 12:30:00 UTC should be in the 10:00-15:00 window
	ts := time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC)
	windowStart, windowEnd := FiveHourWindow(ts)

	expectedStart := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 5, 19, 15, 0, 0, 0, time.UTC)

	if !windowStart.Equal(expectedStart) {
		t.Errorf("expected window start %v, got %v", expectedStart, windowStart)
	}
	if !windowEnd.Equal(expectedEnd) {
		t.Errorf("expected window end %v, got %v", expectedEnd, windowEnd)
	}
}

func TestSevenDayWindowBoundary(t *testing.T) {
	// Test that we correctly identify which Sunday-to-Sunday window a timestamp falls in
	// 2026-05-19 is a Tuesday; should be in Sunday 2026-05-17 to Sunday 2026-05-24 window
	ts := time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC)
	windowStart, windowEnd := SevenDayWindow(ts)

	expectedStart := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	if !windowStart.Equal(expectedStart) {
		t.Errorf("expected window start %v, got %v", expectedStart, windowStart)
	}
	if !windowEnd.Equal(expectedEnd) {
		t.Errorf("expected window end %v, got %v", expectedEnd, windowEnd)
	}
}
