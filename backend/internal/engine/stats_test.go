package engine

import "testing"

func almostEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestMedianOddEven(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); !almostEqual(got, 2, 1e-9) {
		t.Fatalf("median odd: got %v", got)
	}
	if got := Median([]float64{1, 2, 3, 4}); !almostEqual(got, 2.5, 1e-9) {
		t.Fatalf("median even: got %v", got)
	}
}

func TestMADNormOnConstantSample(t *testing.T) {
	sample := []float64{10, 10, 10, 10}
	if got := MADNorm(sample); got != 0 {
		t.Fatalf("expected 0 MAD for constant sample, got %v", got)
	}
}

func TestRobustZHandlesZeroMADWithoutNaN(t *testing.T) {
	sample := []float64{10, 10, 10, 10}
	z := RobustZ(50, sample) // valor muy alejado, MAD=0
	if z == 0 {
		t.Fatal("expected non-zero z when value deviates from a constant sample")
	}
	if z != z { // NaN check
		t.Fatal("RobustZ produced NaN")
	}
}

func TestRobustZTypicalCase(t *testing.T) {
	sample := []float64{10, 11, 9, 10, 12, 8, 10, 11, 9, 10}
	z := RobustZ(10, sample)
	if !almostEqual(z, 0, 0.5) {
		t.Fatalf("z for the median itself should be ~0, got %v", z)
	}
}
