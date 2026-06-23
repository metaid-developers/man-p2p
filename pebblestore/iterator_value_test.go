package pebblestore

import "testing"

func TestCloneBytesReturnsIndependentSlice(t *testing.T) {
	src := []byte("abc")
	got := CloneBytes(src)
	if string(got) != "abc" {
		t.Fatalf("unexpected clone: %q", string(got))
	}
	src[0] = 'z'
	if string(got) != "abc" {
		t.Fatalf("clone changed after source mutation: %q", string(got))
	}
}

func TestCloneBytesNil(t *testing.T) {
	if CloneBytes(nil) != nil {
		t.Fatalf("expected nil clone for nil input")
	}
}
