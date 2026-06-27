package components

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func questionStateForTest() *domain.UserQuestionUIState {
	return &domain.UserQuestionUIState{
		Questions: []domain.UserQuestion{
			{
				Header:      "Format",
				Question:    "Which output format?",
				MultiSelect: false,
				Options: []domain.UserQuestionOption{
					{Label: "JSON", Description: "machine readable"},
					{Label: "YAML", Description: "human readable"},
				},
			},
		},
		CurrentIndex: 0,
		OptionCursor: 0,
		Selected:     []map[int]bool{{}},
		OtherText:    []string{""},
		OtherActive:  []bool{false},
	}
}

func TestQuestionFormView_RenderNilState(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetUserQuestionUIStateReturns(nil)

	v := NewQuestionFormView(createMockStyleProvider(), sm)
	if got := v.Render(); got != "" {
		t.Errorf("expected empty render with nil state, got %q", got)
	}
}

func TestQuestionFormView_RendersQuestion(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	sm.GetUserQuestionUIStateReturns(questionStateForTest())

	v := NewQuestionFormView(createMockStyleProvider(), sm)
	v.SetWidth(80)

	out := v.Render()
	for _, want := range []string{"Format", "(1/1)", "Which output format?", "JSON", "YAML", "Other"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected render to contain %q, got:\n%s", want, out)
		}
	}
}

func TestQuestionFormView_MultiSelectCheckboxes(t *testing.T) {
	state := questionStateForTest()
	state.Questions[0].MultiSelect = true
	state.Selected[0][0] = true // JSON selected

	sm := &domainmocks.FakeStateManager{}
	sm.GetUserQuestionUIStateReturns(state)

	v := NewQuestionFormView(createMockStyleProvider(), sm)
	v.SetWidth(80)

	out := v.Render()
	if !strings.Contains(out, "[x]") {
		t.Errorf("expected a checked checkbox for the selected option, got:\n%s", out)
	}
	if !strings.Contains(out, "[ ]") {
		t.Errorf("expected an unchecked checkbox for the unselected option, got:\n%s", out)
	}
}
