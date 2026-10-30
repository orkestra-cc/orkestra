package bench

import (
	"os"
	"strings"
	"testing"
)

const (
	okItem  = `{"kind":"item","id":"i1","lang":"en","title":"T","text":"body","visibleTo":["tenant:*"]}` + "\n"
	okItem2 = `{"kind":"item","id":"i2","lang":"en","title":"T","text":"body","visibleTo":["user:u-2"]}` + "\n"
)

func TestLoadFixture_Errors(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"unknown kind", okItem + `{"kind":"doc","id":"x"}` + "\n", "line 2"},
		{"duplicate id", okItem + okItem, "line 2"},
		{"empty text", `{"kind":"item","id":"i1","lang":"en","title":"T","text":" ","visibleTo":["tenant:*"]}` + "\n", "line 1"},
		{"no principals", okItem + `{"kind":"query","id":"q1","lang":"en","text":"x","principals":[],"expected":["i1"]}` + "\n", "line 2"},
		{"unknown expected", okItem + `{"kind":"query","id":"q1","lang":"en","text":"x","principals":["tenant:*"],"expected":["nope"]}` + "\n", "line 2"},
		{"invisible expected", okItem + okItem2 + `{"kind":"query","id":"q1","lang":"en","text":"x","principals":["tenant:*"],"expected":["i2"]}` + "\n", "line 3"},
		{"unknown hidden", okItem + `{"kind":"query","id":"q1","lang":"en","text":"x","principals":["tenant:*"],"expected":["i1"],"hidden":["nope"]}` + "\n", "line 2"},
		{"visible hidden", okItem + `{"kind":"query","id":"q1","lang":"en","text":"x","principals":["tenant:*"],"expected":["i1"],"hidden":["i1"]}` + "\n", "line 2"},
		{"malformed json", okItem + "{oops\n", "line 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadFixture(strings.NewReader(c.in))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestLoadFixture_QueryBeforeItem(t *testing.T) {
	in := `{"kind":"query","id":"q1","lang":"en","text":"x","principals":["tenant:*"],"expected":["i1"],"hidden":["i2"]}` + "\n" + okItem + okItem2
	f, err := LoadFixture(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if !f.VisibleTo("i1", []string{"tenant:*"}) || f.VisibleTo("i2", []string{"tenant:*"}) || f.VisibleTo("zz", []string{"tenant:*"}) {
		t.Error("VisibleTo mismatch")
	}
}

func TestSmokeFixture_Composition(t *testing.T) {
	file, err := os.Open("testdata/smoke-it-en.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	f, err := LoadFixture(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Items) != 40 || len(f.Queries) != 12 {
		t.Fatalf("want 40 items and 12 queries, got %d and %d", len(f.Items), len(f.Queries))
	}
	lang := map[string]int{}
	vis := map[string]int{}
	langOf := map[string]string{}
	for _, it := range f.Items {
		lang[it.Lang]++
		vis[strings.Join(it.VisibleTo, ",")]++
		langOf[it.ID] = it.Lang
	}
	if lang["it"] != 20 || lang["en"] != 20 {
		t.Errorf("item languages: %v", lang)
	}
	if vis["tenant:*"] != 24 || vis["role:r-sales"] != 8 || vis["user:u-2"] != 8 {
		t.Errorf("visibility: %v", vis)
	}
	qlang := map[string]int{}
	sets := map[string]int{}
	cross, hidden := 0, 0
	for _, q := range f.Queries {
		qlang[q.Lang]++
		sets[strings.Join(q.Principals, ",")]++
		if len(q.Expected) == 0 {
			t.Errorf("%s: no expected item", q.ID)
		}
		if len(q.Hidden) > 0 {
			hidden++
		}
		for _, id := range q.Expected {
			if langOf[id] != q.Lang {
				cross++
				break
			}
		}
	}
	if qlang["it"] != 6 || qlang["en"] != 6 {
		t.Errorf("query languages: %v", qlang)
	}
	if len(sets) != 3 {
		t.Errorf("want 3 principal sets, got %v", sets)
	}
	for s, n := range sets {
		if n != 4 {
			t.Errorf("principal set %s has %d queries, want 4", s, n)
		}
	}
	if cross < 4 {
		t.Errorf("cross-language queries: %d, want >= 4", cross)
	}
	if hidden < 3 {
		t.Errorf("queries with hidden: %d, want >= 3", hidden)
	}
}
