package services

import (
	"reflect"
	"strings"
	"testing"
)

// The module has no assertion library; assert and require are the few
// helpers the services tests need (require stops the test, assert does not).
type checker struct{ fatal bool }

var (
	assert  = checker{}
	require = checker{fatal: true}
)

func (c checker) fail(t *testing.T, format string, args ...any) {
	t.Helper()
	if c.fatal {
		t.Fatalf(format, args...)
	}
	t.Errorf(format, args...)
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

func isEmpty(v any) bool {
	if isNil(v) {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.String, reflect.Array:
		return rv.Len() == 0
	}
	return rv.IsZero()
}

func (c checker) Equal(t *testing.T, want, got any, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		c.fail(t, "not equal %v\nwant: %#v\ngot:  %#v", msg, want, got)
	}
}

func (c checker) NotEqual(t *testing.T, a, b any, msg ...any) {
	t.Helper()
	if reflect.DeepEqual(a, b) {
		c.fail(t, "unexpectedly equal %v: %#v", msg, a)
	}
}

func (c checker) Empty(t *testing.T, v any) {
	t.Helper()
	if !isEmpty(v) {
		c.fail(t, "want empty, got %#v", v)
	}
}

func (c checker) NotEmpty(t *testing.T, v any) {
	t.Helper()
	if isEmpty(v) {
		c.fail(t, "want not empty")
	}
}

func (c checker) Nil(t *testing.T, v any) {
	t.Helper()
	if !isNil(v) {
		c.fail(t, "want nil, got %#v", v)
	}
}

func (c checker) NotNil(t *testing.T, v any) {
	t.Helper()
	if isNil(v) {
		c.fail(t, "want not nil")
	}
}

func (c checker) NoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		c.fail(t, "unexpected error: %v", err)
	}
}

func (c checker) Contains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		c.fail(t, "%q does not contain %q", s, sub)
	}
}

func (c checker) Len(t *testing.T, v any, n int) {
	t.Helper()
	if l := reflect.ValueOf(v).Len(); l != n {
		c.fail(t, "len = %d, want %d", l, n)
	}
}
