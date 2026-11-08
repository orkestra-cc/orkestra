package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunDetail(t *testing.T) {
	r := NewRun("m.j", "id1", TriggerManual, 2, time.Unix(0, 0))
	if r.Key() != "m.j" || r.ID() != "id1" || r.Trigger() != TriggerManual || r.Attempt() != 2 || !r.ScheduledFor().Equal(time.Unix(0, 0)) {
		t.Fatal("accessors wrong")
	}
	r.Detail("long", strings.Repeat("é", 300))
	r.Detail("n", uint8(7))
	r.Detail("d", 90*time.Second)
	r.Detail("struct", struct{}{})
	d, dropped := r.Details()
	if got := []rune(d["long"].(string)); len(got) != 256 {
		t.Errorf("long string = %d runes", len(got))
	}
	if d["n"] != int64(7) {
		t.Errorf("n = %#v", d["n"])
	}
	if d["d"] != "1m30s" {
		t.Errorf("d = %#v", d["d"])
	}
	if _, ok := d["struct"]; ok || dropped != 1 {
		t.Errorf("struct kept or dropped=%d", dropped)
	}
	// Fill to the 32 key limit; the 33rd is dropped, an existing key may be updated.
	for i := 0; i < 40; i++ {
		r.Detail(fmt.Sprintf("k%d", i), i)
	}
	d, _ = r.Details()
	if len(d) != 32 {
		t.Errorf("len = %d, want 32", len(d))
	}
	r.Detail("n", 8)
	d, _ = r.Details()
	if d["n"] != int64(8) {
		t.Error("existing key not updatable at the limit")
	}
}

func TestRunDetailConcurrent(t *testing.T) {
	r := NewRun("m.j", "id", TriggerSchedule, 1, time.Now())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Detail(fmt.Sprintf("k%d", i), i)
			r.Details()
		}(i)
	}
	wg.Wait()
}

func TestPermanent(t *testing.T) {
	base := errors.New("boom")
	p := Permanent(base)
	if !errors.Is(p, base) {
		t.Fatal("chain lost")
	}
	if !IsPermanent(fmt.Errorf("x: %w", p)) || IsPermanent(base) {
		t.Fatal("IsPermanent wrong")
	}
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must be nil")
	}
}

// A detail must survive JSON encoding (the runs API returns details as is):
// NaN and ±Inf floats, and unsigned values above MaxInt64 (which would wrap
// to a negative int64), are dropped and counted instead.
func TestRunDetailRejectsUnencodableNumbers(t *testing.T) {
	r := NewRun("m.j", "id", TriggerSchedule, 1, time.Now())
	r.Detail("nan", math.NaN())
	r.Detail("inf", math.Inf(1))
	r.Detail("ninf", float32(math.Inf(-1)))
	r.Detail("big", uint64(math.MaxInt64)+1)
	r.Detail("bigu", uint(math.MaxUint64))
	r.Detail("max", uint64(math.MaxInt64))
	r.Detail("f", 1.5)
	d, dropped := r.Details()
	for _, k := range []string{"nan", "inf", "ninf", "big", "bigu"} {
		if _, ok := d[k]; ok {
			t.Errorf("%s kept as %#v", k, d[k])
		}
	}
	if dropped != 5 {
		t.Errorf("dropped = %d, want 5", dropped)
	}
	if d["max"] != int64(math.MaxInt64) || d["f"] != 1.5 {
		t.Errorf("max = %#v, f = %#v", d["max"], d["f"])
	}
	if _, err := json.Marshal(d); err != nil {
		t.Errorf("details do not encode: %v", err)
	}
}
