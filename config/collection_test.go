package config

import (
	"strings"
	"testing"
)

type collectionFixture struct{ Name, Value string }

func nameOfFixture(e collectionFixture) string { return e.Name }

const fixtureKind = "fixture"

func TestAppendEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   []collectionFixture
		item    collectionFixture
		wantErr bool
		wantLen int
	}{
		{
			name:    "appends new entry",
			input:   []collectionFixture{{Name: "a"}},
			item:    collectionFixture{Name: "b"},
			wantLen: 2,
		},
		{
			name:    "rejects duplicate",
			input:   []collectionFixture{{Name: "a"}},
			item:    collectionFixture{Name: "a"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := appendEntry(tt.input, tt.item, tt.item.Name, nameOfFixture, fixtureKind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), fixtureKind) {
					t.Errorf("error should mention duplicate and kind: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if len(out) != tt.wantLen || out[tt.wantLen-1].Name != tt.item.Name {
				t.Errorf("got %+v", out)
			}
		})
	}
}

func TestReplaceEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   []collectionFixture
		item    collectionFixture
		wantErr bool
		wantVal string
	}{
		{
			name:    "replaces existing entry",
			input:   []collectionFixture{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}},
			item:    collectionFixture{Name: "b", Value: "X"},
			wantVal: "X",
		},
		{
			name:    "not found returns error",
			input:   []collectionFixture{{Name: "a"}},
			item:    collectionFixture{Name: "b"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := replaceEntry(tt.input, tt.item, tt.item.Name, nameOfFixture, fixtureKind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected not-found error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if out[1].Value != tt.wantVal {
				t.Errorf("expected replacement, got %+v", out)
			}
			if tt.input[1].Value != "2" {
				t.Errorf("input slice mutated: got %+v", tt.input)
			}
		})
	}
}

func TestRemoveEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   []collectionFixture
		remove  string
		wantErr bool
		wantLen int
	}{
		{
			name:    "removes existing entry",
			input:   []collectionFixture{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			remove:  "b",
			wantLen: 2,
		},
		{
			name:    "not found returns error",
			input:   []collectionFixture{{Name: "a"}},
			remove:  "z",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := removeEntry(tt.input, tt.remove, nameOfFixture, fixtureKind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected not-found error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if len(out) != tt.wantLen || out[0].Name != "a" || out[1].Name != "c" {
				t.Errorf("got %+v", out)
			}
		})
	}
}

func TestFindEntry(t *testing.T) {
	tests := []struct {
		name    string
		input   []collectionFixture
		find    string
		wantErr bool
		wantVal string
	}{
		{
			name:    "finds existing entry",
			input:   []collectionFixture{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}},
			find:    "b",
			wantVal: "2",
		},
		{
			name:    "not found returns error",
			input:   []collectionFixture{{Name: "a"}},
			find:    "z",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findEntry(tt.input, tt.find, nameOfFixture, fixtureKind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected not-found error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if got.Value != tt.wantVal {
				t.Errorf("got %+v", got)
			}
		})
	}
}
