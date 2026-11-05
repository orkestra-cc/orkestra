package csvsafe

import "testing"

func TestCellNeutralizesFormulaLeaders(t *testing.T) {
	for _, in := range []string{"=1+1", "+39 333", "-x", "@cmd", "\tx", "\rx", "  =late"} {
		if got := Cell(in); got != "'"+in {
			t.Errorf("Cell(%q) = %q", in, got)
		}
	}
	for _, in := range []string{"", "   ", "Alice", "10", "teacher; researcher"} {
		if got := Cell(in); got != in {
			t.Errorf("Cell(%q) must be unchanged, got %q", in, got)
		}
	}
	src := []string{"=a", "b"}
	out := Row(src)
	if out[0] != "'=a" || src[0] != "=a" {
		t.Fatal("Row must copy, not mutate")
	}
}
