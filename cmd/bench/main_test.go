package main

import (
	"math"
	"testing"
)

func TestComputeStats_Basic(t *testing.T) {
	durations := []float64{100, 200, 300, 400, 500}
	stats := computeStats(durations)

	if stats.MeanMs != 300 {
		t.Errorf("MeanMs = %v, want 300", stats.MeanMs)
	}
	if stats.MinMs != 100 {
		t.Errorf("MinMs = %v, want 100", stats.MinMs)
	}
	if stats.MaxMs != 500 {
		t.Errorf("MaxMs = %v, want 500", stats.MaxMs)
	}
	if stats.P50Ms != 300 {
		t.Errorf("P50Ms = %v, want 300", stats.P50Ms)
	}
}

func TestComputeStats_Empty(t *testing.T) {
	stats := computeStats([]float64{})
	if stats.MeanMs != 0 || stats.MinMs != 0 || stats.MaxMs != 0 || stats.P50Ms != 0 {
		t.Errorf("expected all zeros for empty input, got %+v", stats)
	}
}

func TestComputeStats_SingleElement(t *testing.T) {
	stats := computeStats([]float64{42.0})
	if stats.MeanMs != 42 {
		t.Errorf("MeanMs = %v, want 42", stats.MeanMs)
	}
	if stats.MinMs != 42 {
		t.Errorf("MinMs = %v, want 42", stats.MinMs)
	}
	if stats.MaxMs != 42 {
		t.Errorf("MaxMs = %v, want 42", stats.MaxMs)
	}
	if stats.P50Ms != 42 {
		t.Errorf("P50Ms = %v, want 42", stats.P50Ms)
	}
}

func TestComputeStats_EvenCount(t *testing.T) {
	// For even count, P50 is average of two middle elements.
	durations := []float64{10, 20, 30, 40}
	stats := computeStats(durations)

	expectedP50 := 25.0 // (20+30)/2
	if stats.P50Ms != expectedP50 {
		t.Errorf("P50Ms = %v, want %v", stats.P50Ms, expectedP50)
	}
}

func TestComputeStats_UnsortedInput(t *testing.T) {
	// Input is unsorted; computeStats should sort internally.
	durations := []float64{500, 100, 300, 200, 400}
	stats := computeStats(durations)

	if stats.MinMs != 100 {
		t.Errorf("MinMs = %v, want 100", stats.MinMs)
	}
	if stats.MaxMs != 500 {
		t.Errorf("MaxMs = %v, want 500", stats.MaxMs)
	}
	if stats.P50Ms != 300 {
		t.Errorf("P50Ms = %v, want 300 (median of sorted)", stats.P50Ms)
	}
}

func TestComputeStats_Rounding(t *testing.T) {
	durations := []float64{1.111, 2.222, 3.333}
	stats := computeStats(durations)

	// Mean = 6.666/3 = 2.222, rounded to 2 decimal places = 2.22
	if stats.MeanMs != 2.22 {
		t.Errorf("MeanMs = %v, want 2.22", stats.MeanMs)
	}
}

func TestComputeStats_DoesNotMutateInput(t *testing.T) {
	original := []float64{500, 100, 300}
	input := make([]float64, len(original))
	copy(input, original)

	computeStats(input)

	// Verify original slice is unchanged
	for i, v := range input {
		if v != original[i] {
			t.Errorf("input[%d] = %v, want %v (input was mutated)", i, v, original[i])
		}
	}
}

func TestSpeedupCalculation(t *testing.T) {
	tests := []struct {
		name          string
		baselineMean  float64
		candidateMean float64
		wantSpeedup   float64
		wantWinner    string
	}{
		{"candidate faster", 200, 100, 2.0, "candidate"},
		{"baseline faster", 100, 200, 0.5, "baseline"},
		{"tie", 100, 100, 1.0, "tie"},
		{"near tie high", 100, 96, 1.04, "tie"},
		{"near tie low", 100, 106, 0.94, "baseline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var speedup float64
			if tt.candidateMean > 0 {
				speedup = tt.baselineMean / tt.candidateMean
			}
			speedup = math.Round(speedup*100) / 100

			winner := "tie"
			if speedup > 1.05 {
				winner = "candidate"
			} else if speedup < 0.95 {
				winner = "baseline"
			}

			if speedup != tt.wantSpeedup {
				t.Errorf("speedup = %v, want %v", speedup, tt.wantSpeedup)
			}
			if winner != tt.wantWinner {
				t.Errorf("winner = %q, want %q", winner, tt.wantWinner)
			}
		})
	}
}
