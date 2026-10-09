package iface

import "testing"

func TestPaperSpecNormalized_Defaults(t *testing.T) {
	got := PaperSpec{}.Normalized()
	want := PaperSpec{Size: "A4", MarginTopMM: 15, MarginRightMM: 15, MarginBottomMM: 15, MarginLeftMM: 15}
	if got != want {
		t.Fatalf("Normalized() = %+v, want %+v", got, want)
	}
}

func TestPaperSpecNormalized_KeepsExplicit(t *testing.T) {
	in := PaperSpec{Size: "Letter", Landscape: true, MarginTopMM: 5, MarginRightMM: 6, MarginBottomMM: 7, MarginLeftMM: 8}
	if got := in.Normalized(); got != in {
		t.Fatalf("Normalized() = %+v, want unchanged %+v", got, in)
	}
}

func TestPaperSpecDimensionsInches(t *testing.T) {
	w, h := PaperSpec{Size: "A4"}.Normalized().DimensionsInches()
	if w < 8.26 || w > 8.28 || h < 11.68 || h > 11.70 {
		t.Fatalf("A4 = %.3fx%.3f in", w, h)
	}
	w, h = PaperSpec{Size: "A4", Landscape: true}.Normalized().DimensionsInches()
	if w < h {
		t.Fatalf("landscape must swap: %.3fx%.3f", w, h)
	}
}
