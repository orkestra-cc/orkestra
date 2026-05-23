package importers

import "testing"

func TestComputeIdempotencyKey_Deterministic(t *testing.T) {
	body := []byte("name,email\nJane,jane@a.example\n")
	m := ColumnMapping{
		Columns: map[string]string{"name": "person.firstName", "email": "person.email"},
		Options: map[string]string{"delimiter": ","},
	}
	a := ComputeIdempotencyKey(body, m)
	b := ComputeIdempotencyKey(body, m)
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("wrong key length: %d", len(a))
	}
}

func TestComputeIdempotencyKey_KeyOrderInsensitive(t *testing.T) {
	body := []byte("data")
	m1 := ColumnMapping{Columns: map[string]string{"a": "X", "b": "Y"}}
	m2 := ColumnMapping{Columns: map[string]string{"b": "Y", "a": "X"}}
	if ComputeIdempotencyKey(body, m1) != ComputeIdempotencyKey(body, m2) {
		t.Fatal("key changed across mapping key reorder — canonicalisation broken")
	}
}

func TestComputeIdempotencyKey_DistinguishesInputs(t *testing.T) {
	body1 := []byte("payload-1")
	body2 := []byte("payload-2")
	m := ColumnMapping{Columns: map[string]string{"a": "X"}}
	if ComputeIdempotencyKey(body1, m) == ComputeIdempotencyKey(body2, m) {
		t.Fatal("different bodies hashed identically")
	}
	m2 := ColumnMapping{Columns: map[string]string{"a": "Y"}}
	if ComputeIdempotencyKey(body1, m) == ComputeIdempotencyKey(body1, m2) {
		t.Fatal("different mappings hashed identically")
	}
}
