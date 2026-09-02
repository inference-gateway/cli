package domain

import "testing"

func TestParseAgentMode_RoundTripsModeKey(t *testing.T) {
	for _, m := range []AgentMode{AgentModeStandard, AgentModePlan, AgentModeAutoAccept, AgentModeAutoWithJudge, AgentModeReadOnly} {
		got, ok := ParseAgentMode(m.ModeKey())
		if !ok || got != m {
			t.Fatalf("ParseAgentMode(%q) = (%v,%v), want (%v,true)", m.ModeKey(), got, ok, m)
		}
	}
	if got := AgentMode(99).ModeKey(); got != "standard" {
		t.Fatalf("AgentMode(99).ModeKey() = %q, want standard", got)
	}
}

func TestAgentModeReadOnly_StringDisplayName(t *testing.T) {
	if got := AgentModeReadOnly.String(); got != "ReadOnly" {
		t.Fatalf("AgentModeReadOnly.String() = %q, want ReadOnly", got)
	}
	if got := AgentModeReadOnly.DisplayName(); got != "Read-Only" {
		t.Fatalf("AgentModeReadOnly.DisplayName() = %q, want Read-Only", got)
	}
	if got := AgentModeReadOnly.ModeKey(); got != "readonly" {
		t.Fatalf("AgentModeReadOnly.ModeKey() = %q, want readonly", got)
	}
}

func TestAgentModeAutoWithJudge_Values(t *testing.T) {
	if got := AgentModeAutoWithJudge.String(); got != "AutoWithJudge" {
		t.Fatalf("AgentModeAutoWithJudge.String() = %q, want AutoWithJudge", got)
	}
	if got := AgentModeAutoWithJudge.DisplayName(); got != "Auto+Judge" {
		t.Fatalf("AgentModeAutoWithJudge.DisplayName() = %q, want Auto+Judge", got)
	}
	if got := AgentModeAutoWithJudge.ModeKey(); got != "auto-with-judge" {
		t.Fatalf("AgentModeAutoWithJudge.ModeKey() = %q, want auto-with-judge", got)
	}
	got, ok := ParseAgentMode("auto-with-judge")
	if !ok || got != AgentModeAutoWithJudge {
		t.Fatalf("ParseAgentMode(%q) = (%v,%v), want (AutoWithJudge,true)", "auto-with-judge", got, ok)
	}
}

func TestParseAgentMode_CaseWhitespaceAndUnknown(t *testing.T) {
	if got, ok := ParseAgentMode("  AUTO "); !ok || got != AgentModeAutoAccept {
		t.Fatalf(`ParseAgentMode("  AUTO ") = (%v,%v), want (AutoAccept,true)`, got, ok)
	}
	if got, ok := ParseAgentMode("bogus"); ok || got != AgentModeStandard {
		t.Fatalf(`ParseAgentMode("bogus") = (%v,%v), want (Standard,false)`, got, ok)
	}
	if got, ok := ParseAgentMode(""); ok || got != AgentModeStandard {
		t.Fatalf(`ParseAgentMode("") = (%v,%v), want (Standard,false)`, got, ok)
	}
}
