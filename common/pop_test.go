package common

import "testing"

func TestGenPopReturnsEmptyForInvalidHexInputs(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GenPop panicked for invalid hex inputs: %v", r)
		}
	}()

	pop, zeroBits := GenPop("pinid", "none", "none")
	if pop != "" {
		t.Fatalf("expected empty pop for invalid hex inputs, got %q", pop)
	}
	if zeroBits != 0 {
		t.Fatalf("expected zero leading bits for invalid inputs, got %d", zeroBits)
	}
}

func TestGenPopReturnsEmptyForShortHexInputs(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GenPop panicked for short hex inputs: %v", r)
		}
	}()

	pop, zeroBits := GenPop("pinid", "00", "00")
	if pop != "" {
		t.Fatalf("expected empty pop for short hex inputs, got %q", pop)
	}
	if zeroBits != 0 {
		t.Fatalf("expected zero leading bits for short inputs, got %d", zeroBits)
	}
}
