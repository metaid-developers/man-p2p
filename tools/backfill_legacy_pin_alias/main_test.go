package main

import "testing"

func TestParseAliasPairs(t *testing.T) {
	pairs, err := parseAliasPairs([]string{
		"legacy-pini0=canonical-pini0",
		" legacy-pini1 = canonical-pini1 ",
		"legacy-pini0=canonical-pini0",
	})
	if err != nil {
		t.Fatalf("parseAliasPairs returned error: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 alias pairs, got %d", len(pairs))
	}
	if got := pairs["legacy-pini0"]; got != "canonical-pini0" {
		t.Fatalf("unexpected canonical pin id for legacy-pini0: %q", got)
	}
	if got := pairs["legacy-pini1"]; got != "canonical-pini1" {
		t.Fatalf("unexpected canonical pin id for legacy-pini1: %q", got)
	}
}

func TestParseAliasPairsRejectsConflicts(t *testing.T) {
	_, err := parseAliasPairs([]string{
		"legacy-pini0=canonical-pini0",
		"legacy-pini0=canonical-pini1",
	})
	if err == nil {
		t.Fatal("expected conflicting alias pair error")
	}
}

func TestSplitAliasPairRejectsInvalidInput(t *testing.T) {
	cases := []string{
		"",
		"legacy-only",
		"legacy=",
		"=canonical",
		"same=same",
	}
	for _, tc := range cases {
		if _, _, err := splitAliasPair(tc); err == nil {
			t.Fatalf("expected splitAliasPair(%q) to fail", tc)
		}
	}
}
