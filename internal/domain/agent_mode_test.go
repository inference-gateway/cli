package domain

import "testing"

func TestParseAgentMode_RoundTripsAllowedlistKey(t *testing.T) {
	for _, m := range []AgentMode{AgentModeStandard, AgentModePlan, AgentModeAutoAccept} {
		got, ok := ParseAgentMode(m.AllowedlistKey())
		if !ok || got != m {
			t.Fatalf("ParseAgentMode(%q) = (%v,%v), want (%v,true)", m.AllowedlistKey(), got, ok, m)
		}
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
