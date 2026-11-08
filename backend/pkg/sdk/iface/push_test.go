package iface

import (
	"errors"
	"strings"
	"testing"
)

func validPush() PushRequest {
	return PushRequest{
		Tier:      "operator",
		UserUUIDs: []string{"u1"},
		Category:  "orders",
		Type:      "transactional",
		Title:     "Hello",
		Body:      "World",
		Data:      map[string]string{"orderId": "42"},
	}
}

func TestPushRequestValidate(t *testing.T) {
	if err := validPush().Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	for _, p := range []string{"", "high", "normal"} {
		r := validPush()
		r.Priority = p
		if err := r.Validate(); err != nil {
			t.Errorf("priority %q rejected: %v", p, err)
		}
	}

	many := make([]string, 501)
	for i := range many {
		many[i] = "u"
	}
	atLimit := make([]string, 500)
	for i := range atLimit {
		atLimit[i] = "u"
	}
	r := validPush()
	r.UserUUIDs = atLimit
	r.Title = strings.Repeat("é", 200)
	r.Body = strings.Repeat("é", 1000)
	if err := r.Validate(); err != nil {
		t.Errorf("limits are inclusive and counted in runes: %v", err)
	}

	cases := map[string]func(*PushRequest){
		"501 users":        func(r *PushRequest) { r.UserUUIDs = many },
		"empty users":      func(r *PushRequest) { r.UserUUIDs = nil },
		"empty user id":    func(r *PushRequest) { r.UserUUIDs = []string{""} },
		"title 201":        func(r *PushRequest) { r.Title = strings.Repeat("é", 201) },
		"body 1001":        func(r *PushRequest) { r.Body = strings.Repeat("é", 1001) },
		"data 3KiB+1":      func(r *PushRequest) { r.Data = map[string]string{"k": strings.Repeat("x", 3*1024)} },
		"key from":         func(r *PushRequest) { r.Data = map[string]string{"from": "x"} },
		"key notification": func(r *PushRequest) { r.Data = map[string]string{"notification": "x"} },
		"key message_type": func(r *PushRequest) { r.Data = map[string]string{"message_type": "x"} },
		"key google.x":     func(r *PushRequest) { r.Data = map[string]string{"google.x": "x"} },
		"key gcm.y":        func(r *PushRequest) { r.Data = map[string]string{"gcm.y": "x"} },
		"empty data key":   func(r *PushRequest) { r.Data = map[string]string{"": "x"} },
		"bad type":         func(r *PushRequest) { r.Type = "spam" },
		"bad tier":         func(r *PushRequest) { r.Tier = "admin" },
		"bad priority":     func(r *PushRequest) { r.Priority = "urgent" },
		"negative ttl":     func(r *PushRequest) { r.TTL = -1 },
		"empty title":      func(r *PushRequest) { r.Title = "" },
		"empty category":   func(r *PushRequest) { r.Category = "" },
	}
	for name, mutate := range cases {
		r := validPush()
		mutate(&r)
		err := r.Validate()
		if err == nil || !errors.Is(err, ErrPushInvalidRequest) {
			t.Errorf("%s: err = %v, want wrapping ErrPushInvalidRequest", name, err)
		}
	}

	// exactly 3 KiB (key+value bytes) passes
	r = validPush()
	r.Data = map[string]string{"k": strings.Repeat("x", 3*1024-1)}
	if err := r.Validate(); err != nil {
		t.Errorf("3 KiB exactly must pass: %v", err)
	}
}
