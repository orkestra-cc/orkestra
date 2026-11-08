package syncdelta

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"go.mongodb.org/mongo-driver/bson"
)

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC), ID: "abc"}
	s := EncodeCursor(c)
	got, err := DecodeCursor(s)
	if err != nil || got == nil {
		t.Fatalf("decode: %v %v", got, err)
	}
	if !got.UpdatedAt.Equal(c.UpdatedAt) || got.ID != c.ID {
		t.Fatalf("got %+v want %+v", got, c)
	}
	if got, err := DecodeCursor(""); got != nil || err != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	for _, s := range []string{"!!!", "e30", "bm90anNvbg", "eyJ0Ijoibm9wZSIsImlkIjoieCJ9"} {
		if _, err := DecodeCursor(s); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("%q: err=%v", s, err)
		}
	}
}

func TestFilterShape(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	h := now.Add(-StableHorizon)
	if got, want := Filter(nil, now, "_id"), (bson.M{"updatedAt": bson.M{"$lte": h}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("nil: %v", got)
	}
	cur := &Cursor{UpdatedAt: now.Add(-time.Hour), ID: "x"}
	want := bson.M{"$and": bson.A{
		bson.M{"updatedAt": bson.M{"$lte": h}},
		bson.M{"$or": bson.A{
			bson.M{"updatedAt": bson.M{"$gt": cur.UpdatedAt}},
			bson.M{"updatedAt": cur.UpdatedAt, "_id": bson.M{"$gt": "x"}},
		}},
	}}
	if got := Filter(cur, now, "_id"); !reflect.DeepEqual(got, want) {
		t.Fatalf("cur: %v", got)
	}
	if !reflect.DeepEqual(Sort("_id"), bson.D{{Key: "updatedAt", Value: 1}, {Key: "_id", Value: 1}}) {
		t.Fatal("sort")
	}
}

func TestClampLimit(t *testing.T) {
	for in, want := range map[int]int{0: 200, -1: 200, 1: 1, 500: 500, 501: 500} {
		if got := ClampLimit(in); got != want {
			t.Errorf("%d: %d", in, got)
		}
	}
}

type row struct {
	id  string
	t   time.Time
	del bool
}

func (r row) SyncCursor() Cursor { return Cursor{UpdatedAt: r.t, ID: r.id} }
func (r row) IsDeleted() bool    { return r.del }
func (r row) SyncID() string     { return r.id }

func TestBuildPage(t *testing.T) {
	now0 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []row{{"a", base, false}, {"b", base.Add(time.Second), true}, {"c", base.Add(2 * time.Second), false}}
	p := BuildPage(rows, 2, nil, now0)
	if !p.HasMore || len(p.Items) != 1 || p.Items[0].id != "a" || !reflect.DeepEqual(p.Deleted, []string{"b"}) {
		t.Fatalf("%+v", p)
	}
	cur, _ := DecodeCursor(p.Cursor)
	if cur == nil || cur.ID != "b" {
		t.Fatalf("cursor %+v", cur)
	}
	prev := &Cursor{UpdatedAt: base, ID: "z"}
	p = BuildPage[row](nil, 2, prev, now0)
	want := *prev
	want.IssuedAt = Horizon(now0)
	if p.HasMore || p.Cursor != EncodeCursor(want) || p.Items == nil || p.Deleted == nil {
		t.Fatalf("%+v", p)
	}
	if b, _ := json.Marshal(p); string(b) != `{"items":[],"deleted":[],"cursor":"`+p.Cursor+`","hasMore":false}` {
		t.Fatal(string(b))
	}
	if p = BuildPage[row](nil, 2, nil, now0); p.Cursor != "" {
		t.Fatal("empty cursor expected")
	}
}

func TestCheckExpired(t *testing.T) {
	now := time.Now()
	if CheckExpired(nil, now, time.Hour) != nil {
		t.Fatal("nil")
	}
	if err := CheckExpired(&Cursor{UpdatedAt: now.Add(-2 * time.Hour)}, now, time.Hour); !errors.Is(err, ErrCursorExpired) {
		t.Fatal(err)
	}
	if CheckExpired(&Cursor{UpdatedAt: now.Add(-time.Minute)}, now, time.Hour) != nil {
		t.Fatal("fresh")
	}
	if CheckExpired(&Cursor{UpdatedAt: now.Add(-1000 * time.Hour)}, now, 0) == nil {
		t.Fatal("zero retention should default")
	}
}

func TestValidateClientTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		d   time.Duration
		bad bool
	}{{4 * time.Minute, false}, {6 * time.Minute, true}, {-29 * 24 * time.Hour, false}, {-31 * 24 * time.Hour, true}}
	for _, c := range cases {
		err := ValidateClientTime(now.Add(c.d), now)
		if c.bad != errors.Is(err, ErrClientTimeOutOfRange) {
			t.Errorf("%v: %v", c.d, err)
		}
	}
}

func TestParseIfMatch(t *testing.T) {
	for in, want := range map[string]int64{`"12"`: 12, `12`: 12, ` "3" `: 3, `W/"4"`: 4} {
		if v, ok := ParseIfMatch(in); !ok || v != want {
			t.Errorf("%q: %d %v", in, v, ok)
		}
	}
	for _, in := range []string{"", "*", `"x"`, `"-1"`} {
		if _, ok := ParseIfMatch(in); ok {
			t.Errorf("%q should fail", in)
		}
	}
}

func TestErrorConstructorsHuma(t *testing.T) {
	_, api := humatest.New(t)
	for path, c := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"/gone":   {CursorExpiredError(), 410, CodeCursorExpired},
		"/badcur": {InvalidCursorError(), 400, CodeInvalidCursor},
		"/badt":   {ClientTimeOutOfRangeError(), 422, CodeClientTimeOutOfRange},
	} {
		c := c
		huma.Get(api, path, func(ctx context.Context, _ *struct{}) (*struct{}, error) { return nil, c.err })
		resp := api.Get(path)
		if resp.Code != c.status {
			t.Fatalf("%s: %d", path, resp.Code)
		}
		var b map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &b)
		if b["code"] != c.code || int(b["status"].(float64)) != c.status || b["title"] == "" || b["detail"] == "" {
			t.Fatalf("%s: %v", path, b)
		}
	}
}

func TestConflictErrorHuma(t *testing.T) {
	_, api := humatest.New(t)
	huma.Put(api, "/x", func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, &ConflictError{CurrentVersion: 7, Current: map[string]any{"name": "n"}}
	})
	resp := api.Put("/x")
	if resp.Code != http.StatusConflict {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var b struct {
		Status         int `json:"status"`
		Title, Detail  string
		Code           string         `json:"code"`
		CurrentVersion int64          `json:"currentVersion"`
		Current        map[string]any `json:"current"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Status != 409 || b.Code != "sync.conflict" || b.CurrentVersion != 7 || b.Current["name"] != "n" || b.Detail == "" {
		t.Fatalf("%s", resp.Body.String())
	}
}

func TestEmptyPageReissuesCursor(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	prev := &Cursor{UpdatedAt: now.Add(-40 * 24 * time.Hour), ID: "z", IssuedAt: now.Add(-24 * time.Hour)}
	p := BuildPage[row](nil, 10, prev, now)
	c, err := DecodeCursor(p.Cursor)
	if err != nil || c == nil {
		t.Fatal(err)
	}
	if !c.IssuedAt.Equal(Horizon(now)) || !c.UpdatedAt.Equal(prev.UpdatedAt) || c.ID != "z" {
		t.Fatalf("%+v", c)
	}
}

func TestCheckExpiredUsesIssuedAt(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	quiet := &Cursor{UpdatedAt: now.Add(-40 * 24 * time.Hour), ID: "a", IssuedAt: now.Add(-24 * time.Hour)}
	if err := CheckExpired(quiet, now, DefaultTombstoneRetention); err != nil {
		t.Fatalf("quiet collection must not expire: %v", err)
	}
	old := &Cursor{UpdatedAt: now, ID: "a", IssuedAt: now.Add(-31 * 24 * time.Hour)}
	if !errors.Is(CheckExpired(old, now, DefaultTombstoneRetention), ErrCursorExpired) {
		t.Fatal("old issuance must expire")
	}
	legacy := &Cursor{UpdatedAt: now.Add(-40 * 24 * time.Hour), ID: "a"}
	if !errors.Is(CheckExpired(legacy, now, DefaultTombstoneRetention), ErrCursorExpired) {
		t.Fatal("zero IssuedAt falls back to UpdatedAt")
	}
	rt, _ := DecodeCursor(EncodeCursor(*quiet))
	if !rt.IssuedAt.Equal(quiet.IssuedAt) {
		t.Fatal("IssuedAt round trip")
	}
}
