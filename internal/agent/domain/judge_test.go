package domain

import "testing"

func TestParseJudgeVerdict(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantDecision JudgeDecision
		wantReason   string
		wantErr      bool
	}{
		{"plain approved", `{"decision": "approved", "reason": "serves the request"}`, JudgeDecisionApproved, "serves the request", false},
		{"plain rejected", `{"decision": "rejected", "reason": "too risky"}`, JudgeDecisionRejected, "too risky", false},
		{"fenced json", "```json\n{\"decision\": \"approved\", \"reason\": \"ok\"}\n```", JudgeDecisionApproved, "ok", false},
		{"bare fence", "```\n{\"decision\": \"rejected\", \"reason\": \"no\"}\n```", JudgeDecisionRejected, "no", false},
		{"prose around json", "Verdict:\n{\"decision\": \"approved\", \"reason\": \"fine\"}\nDone.", JudgeDecisionApproved, "fine", false},
		{"empty reason ok", `{"decision": "approved"}`, JudgeDecisionApproved, "", false},
		{"no json object", "no verdict here", "", "", true},
		{"invalid decision", `{"decision": "maybe", "reason": "x"}`, "", "", true},
		{"invalid json", `{"decision":`, "", "", true},
		{"empty output", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJudgeVerdict(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseJudgeVerdict(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Decision != tt.wantDecision || got.Reason != tt.wantReason {
				t.Errorf("ParseJudgeVerdict(%q) = %+v, want decision %q reason %q", tt.raw, got, tt.wantDecision, tt.wantReason)
			}
			if got.Approved() != (got.Decision == JudgeDecisionApproved) {
				t.Errorf("Approved() inconsistent with decision %q", got.Decision)
			}
		})
	}
}
