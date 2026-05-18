package api

import (
	"math"
	"testing"
)

func TestSelectModel_UserOverride(t *testing.T) {
	got := selectModel("short", false, "claude-opus-4-6")
	if got != "claude-opus-4-6" {
		t.Errorf("got %q, want claude-opus-4-6", got)
	}
}

func TestSelectModel_ImagesEscalate(t *testing.T) {
	got := selectModel("short", true, "")
	if got != ModelSonnet {
		t.Errorf("got %q, want %q", got, ModelSonnet)
	}
}

func TestSelectModel_LongMessageEscalates(t *testing.T) {
	long := make([]byte, EscalateAfterChars+1)
	for i := range long {
		long[i] = 'a'
	}
	got := selectModel(string(long), false, "")
	if got != ModelSonnet {
		t.Errorf("got %q, want %q", got, ModelSonnet)
	}
}

func TestSelectModel_DefaultIsHaiku(t *testing.T) {
	got := selectModel("short message", false, "")
	if got != ModelHaiku {
		t.Errorf("got %q, want %q", got, ModelHaiku)
	}
}

func TestCalcCost_Haiku(t *testing.T) {
	// 1M input tokens at $0.80 + 1M output at $4.00 = $4.80
	got := calcCost(ModelHaiku, 1_000_000, 1_000_000)
	want := 4.80
	if math.Abs(got-want) > 1e-10 {
		t.Errorf("calcCost Haiku 1M/1M = %.4f, want %.4f", got, want)
	}
}

func TestCalcCost_Sonnet(t *testing.T) {
	// 100k input at $3.00/M + 10k output at $15.00/M
	got := calcCost(ModelSonnet, 100_000, 10_000)
	want := 0.30 + 0.15
	if math.Abs(got-want) > 1e-10 {
		t.Errorf("calcCost Sonnet 100k/10k = %.4f, want %.4f", got, want)
	}
}

func TestCalcCost_UnknownModel(t *testing.T) {
	got := calcCost("unknown-model", 1_000_000, 1_000_000)
	if got != 0 {
		t.Errorf("unknown model should return 0, got %f", got)
	}
}

func TestCalcCost_ZeroTokens(t *testing.T) {
	got := calcCost(ModelHaiku, 0, 0)
	if got != 0 {
		t.Errorf("zero tokens should return 0, got %f", got)
	}
}
