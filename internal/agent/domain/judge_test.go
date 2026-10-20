package domain

import (
	"testing"
)

func TestParseJudgeVerdicts(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		ids     []string
		want    map[string]JudgeDecision
		wantErr bool
	}{
		{"array one per id", `[{"id":"a","decision":"approved","reason":"ok"},{"id":"b","decision":"rejected","reason":"no"}]`, []string{"a", "b"}, map[string]JudgeDecision{"a": JudgeDecisionApproved, "b": JudgeDecisionRejected}, false},
		{"fenced array", "```json\n[{\"id\":\"a\",\"decision\":\"approved\"}]\n```", []string{"a"}, map[string]JudgeDecision{"a": JudgeDecisionApproved}, false},
		{"prose around array", "Verdicts:\n[{\"id\":\"a\",\"decision\":\"rejected\",\"reason\":\"x\"}]\nDone.", []string{"a"}, map[string]JudgeDecision{"a": JudgeDecisionRejected}, false},
		{"legacy object single call", `{"decision": "approved", "reason": "serves the request"}`, []string{"a"}, map[string]JudgeDecision{"a": JudgeDecisionApproved}, false},
		{"legacy object with id", `{"id":"b","decision":"rejected"}`, []string{"a", "b"}, map[string]JudgeDecision{"b": JudgeDecisionRejected}, false},
		{"legacy object ambiguous for two calls", `{"decision": "approved"}`, []string{"a", "b"}, map[string]JudgeDecision{}, false},
		{"omitted id", `[{"id":"a","decision":"approved"}]`, []string{"a", "b"}, map[string]JudgeDecision{"a": JudgeDecisionApproved}, false},
		{"unknown id dropped", `[{"id":"zzz","decision":"approved"},{"id":"a","decision":"approved"}]`, []string{"a"}, map[string]JudgeDecision{"a": JudgeDecisionApproved}, false},
		{"invalid decision dropped", `[{"id":"a","decision":"maybe"},{"id":"b","decision":"rejected"}]`, []string{"a", "b"}, map[string]JudgeDecision{"b": JudgeDecisionRejected}, false},
		{"duplicate keeps first", `[{"id":"a","decision":"approved"},{"id":"a","decision":"rejected"}]`, []string{"a"}, map[string]JudgeDecision{"a": JudgeDecisionApproved}, false},
		{"no json", "no verdict here", []string{"a"}, nil, true},
		{"invalid json", `[{"decision":`, []string{"a"}, nil, true},
		{"empty output", "", []string{"a"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJudgeVerdicts(tt.raw, tt.ids)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseJudgeVerdicts(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseJudgeVerdicts(%q) = %+v, want %v", tt.raw, got, tt.want)
			}
			for id, decision := range tt.want {
				v, ok := got[id]
				if !ok || v.Decision != decision || v.ID != id {
					t.Errorf("verdict for %q = %+v, want decision %q", id, v, decision)
				}
				if v.Approved() != (decision == JudgeDecisionApproved) {
					t.Errorf("Approved() inconsistent with decision %q", decision)
				}
			}
		})
	}
}
