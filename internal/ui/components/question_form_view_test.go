package components

import (
	"strings"
	"testing"

	ui "github.com/inference-gateway/cli/internal/ui"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func questionStateForTest(questions ...agentdomain.UserQuestion) *agentdomain.UserQuestionUIState {
	if len(questions) == 0 {
		questions = []agentdomain.UserQuestion{
			{
				Header:      "Format",
				Question:    "Which output format?",
				MultiSelect: false,
				Options: []agentdomain.UserQuestionOption{
					{Label: "JSON", Description: "machine readable"},
					{Label: "YAML", Description: "human readable"},
				},
			},
		}
	}
	return &agentdomain.UserQuestionUIState{
		Questions:    questions,
		ResponseChan: make(chan []agentdomain.UserQuestionAnswer, 1),
	}
}

// pumpQuestionForm forwards a message and keeps executing any returned
// commands, feeding their messages back into the form, until it settles.
func pumpQuestionForm(v *QuestionFormView, msg tea.Msg) {
	drainQuestionForm(v, v.Forward(msg))
}

func drainQuestionForm(v *QuestionFormView, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			drainQuestionForm(v, c)
		}
		return
	}
	drainQuestionForm(v, v.Forward(msg))
}

func newQuestionFormForTest(state *agentdomain.UserQuestionUIState) (*QuestionFormView, *ui.ApplicationState) {
	sm := ui.NewApplicationState()
	sm.SetupUserQuestionUIState(state.Questions, state.ResponseChan)

	v := NewQuestionFormView(createMockStyleProvider(), sm)
	v.SetWidth(80)
	drainQuestionForm(v, v.Begin())
	return v, sm
}

func TestQuestionFormView_RenderNilState(t *testing.T) {
	sm := ui.NewApplicationState()

	v := NewQuestionFormView(createMockStyleProvider(), sm)
	if got := v.Render(); got != "" {
		t.Errorf("expected empty render with nil state, got %q", got)
	}
}

func TestQuestionFormView_RendersQuestion(t *testing.T) {
	v, _ := newQuestionFormForTest(questionStateForTest())

	out := v.Render()
	for _, want := range []string{"Format", "(1/1)", "Which output format?", "JSON", "YAML", "Other"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected render to contain %q, got:\n%s", want, out)
		}
	}
}

func TestQuestionFormView_SingleSelectDefaultSubmit(t *testing.T) {
	state := questionStateForTest()
	v, _ := newQuestionFormForTest(state)

	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case answers := <-state.ResponseChan:
		if len(answers) != 1 || len(answers[0].SelectedLabels) != 1 || answers[0].SelectedLabels[0] != "JSON" {
			t.Errorf("expected [JSON] answer, got %+v", answers)
		}
	default:
		t.Fatal("expected answers on the response channel")
	}
}

func TestQuestionFormView_RecommendedPreselected(t *testing.T) {
	state := questionStateForTest(agentdomain.UserQuestion{
		Header:   "Fmt",
		Question: "q",
		Options: []agentdomain.UserQuestionOption{
			{Label: "JSON"}, {Label: "YAML (Recommended)"},
		},
	})
	v, _ := newQuestionFormForTest(state)

	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case answers := <-state.ResponseChan:
		if len(answers) != 1 || len(answers[0].SelectedLabels) != 1 || answers[0].SelectedLabels[0] != "YAML (Recommended)" {
			t.Errorf("expected the recommended option, got %+v", answers)
		}
	default:
		t.Fatal("expected answers on the response channel")
	}
}

func TestQuestionFormView_MultiSelectToggleAndSubmit(t *testing.T) {
	state := questionStateForTest(agentdomain.UserQuestion{
		Header:      "Scope",
		Question:    "scope?",
		MultiSelect: true,
		Options: []agentdomain.UserQuestionOption{
			{Label: "A"}, {Label: "B"}, {Label: "C"},
		},
	})
	v, _ := newQuestionFormForTest(state)

	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeyDown})
	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case answers := <-state.ResponseChan:
		if len(answers) != 1 || len(answers[0].SelectedLabels) != 2 ||
			answers[0].SelectedLabels[0] != "A" || answers[0].SelectedLabels[1] != "B" {
			t.Errorf("expected [A B], got %+v", answers)
		}
	default:
		t.Fatal("expected answers on the response channel")
	}
}

func TestQuestionFormView_EscCancels(t *testing.T) {
	state := questionStateForTest()
	v, sm := newQuestionFormForTest(state)

	pumpQuestionForm(v, tea.KeyPressMsg{Code: tea.KeyEscape})

	if sm.GetUserQuestionUIState() != nil {
		t.Error("expected esc to clear the question state")
	}
	if got := v.Render(); got != "" {
		t.Errorf("expected empty render after cancel, got %q", got)
	}
}
