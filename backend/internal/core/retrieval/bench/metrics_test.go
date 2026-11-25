package bench

import (
	"math"
	"testing"
	"time"
)

func TestRecallAtK(t *testing.T) {
	ranked := []string{"a", "b", "c", "d"}
	relevant := []string{"a", "d", "e"}
	cases := []struct {
		k    int
		want float64
	}{{3, 1.0 / 3}, {4, 2.0 / 3}, {10, 2.0 / 3}, {0, 0}}
	for _, c := range cases {
		if got := RecallAtK(ranked, relevant, c.k); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("k=%d: want %v, got %v", c.k, c.want, got)
		}
	}
}

func TestNDCGAtK(t *testing.T) {
	if got := NDCGAtK([]string{"a", "b", "c"}, []string{"a", "c"}, 3); math.Abs(got-0.9197) > 1e-4 {
		t.Errorf("want ~0.9197, got %v", got)
	}
	if got := NDCGAtK([]string{"x", "y"}, []string{"a"}, 2); got != 0 {
		t.Errorf("want 0, got %v", got)
	}
	if got := NDCGAtK([]string{"a"}, []string{"a"}, 1); math.Abs(got-1) > 1e-12 {
		t.Errorf("want 1, got %v", got)
	}
}

func TestP95(t *testing.T) {
	d := make([]time.Duration, 0, 100)
	for i := 100; i >= 1; i-- { // unsorted input: P95 must not depend on order
		d = append(d, time.Duration(i)*time.Millisecond)
	}
	if got := P95(d); got != 95*time.Millisecond {
		t.Errorf("want 95ms, got %v", got)
	}
	if d[0] != 100*time.Millisecond {
		t.Error("P95 must not reorder its input")
	}
	if got := P95([]time.Duration{5 * time.Millisecond}); got != 5*time.Millisecond {
		t.Errorf("want 5ms, got %v", got)
	}
	if got := P95(nil); got != 0 {
		t.Errorf("want 0, got %v", got)
	}
}
