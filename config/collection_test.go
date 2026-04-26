package config

import (
	"strings"
	"testing"
)

type collectionFixture struct{ Name, Value string }

func nameOfFixture(e collectionFixture) string { return e.Name }

const fixtureKind = "fixture"

func TestAppendEntry_AppendsNew(t *testing.T) {
	in := []collectionFixture{{Name: "a"}}
	out, err := appendEntry(in, collectionFixture{Name: "b"}, "b", nameOfFixture, fixtureKind)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 2 || out[1].Name != "b" {
		t.Errorf("got %+v", out)
	}
}

func TestAppendEntry_RejectsDuplicate(t *testing.T) {
	in := []collectionFixture{{Name: "a"}}
	_, err := appendEntry(in, collectionFixture{Name: "a"}, "a", nameOfFixture, fixtureKind)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), fixtureKind) {
		t.Errorf("error should mention duplicate and kind: %v", err)
	}
}

func TestReplaceEntry_ReplacesExisting(t *testing.T) {
	in := []collectionFixture{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}
	out, err := replaceEntry(in, collectionFixture{Name: "b", Value: "X"}, "b", nameOfFixture, fixtureKind)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out[1].Value != "X" {
		t.Errorf("expected replacement, got %+v", out)
	}
	if in[1].Value != "2" {
		t.Errorf("input slice mutated: got %+v", in)
	}
}

func TestReplaceEntry_NotFound(t *testing.T) {
	_, err := replaceEntry([]collectionFixture{{Name: "a"}}, collectionFixture{Name: "b"}, "b", nameOfFixture, fixtureKind)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestRemoveEntry_RemovesExisting(t *testing.T) {
	in := []collectionFixture{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	out, err := removeEntry(in, "b", nameOfFixture, fixtureKind)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 2 || out[0].Name != "a" || out[1].Name != "c" {
		t.Errorf("got %+v", out)
	}
}

func TestRemoveEntry_NotFound(t *testing.T) {
	_, err := removeEntry([]collectionFixture{{Name: "a"}}, "z", nameOfFixture, fixtureKind)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestFindEntry_FindsExisting(t *testing.T) {
	in := []collectionFixture{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}
	got, err := findEntry(in, "b", nameOfFixture, fixtureKind)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got.Value != "2" {
		t.Errorf("got %+v", got)
	}
}

func TestFindEntry_NotFound(t *testing.T) {
	_, err := findEntry([]collectionFixture{{Name: "a"}}, "z", nameOfFixture, fixtureKind)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}
