package services

import (
	"context"
	"testing"
)

func TestNoopDriver_RequiresNothingAndNeverFails(t *testing.T) {
	d := NewNoopDriver(nil)
	if d.Name() != "noop" || len(d.Requires()) != 0 {
		t.Fatalf("noop must be named noop and require nothing: %v", d.Requires())
	}
	if err := ValidateProfile(d, SenderProfile{}, RuntimeView); err != nil {
		t.Fatalf("a noop profile with nothing but a slug must validate, got %v", err)
	}
	if err := d.Send(context.Background(), SenderProfile{}, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"}); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
