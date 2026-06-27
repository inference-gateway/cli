package domain

import (
	"context"
	"testing"
)

type fakeBroker struct{}

func (fakeBroker) AskUserQuestions(context.Context, []UserQuestion) ([]UserQuestionAnswer, bool, error) {
	return nil, false, nil
}

func TestWithUserQuestionBroker(t *testing.T) {
	ctx := context.Background()
	if GetUserQuestionBroker(ctx) != nil {
		t.Fatal("expected nil broker on empty context")
	}
	if HasUserQuestionBroker(ctx) {
		t.Fatal("expected HasUserQuestionBroker=false on empty context")
	}

	ctx = WithUserQuestionBroker(ctx, fakeBroker{})
	if GetUserQuestionBroker(ctx) == nil {
		t.Fatal("expected a broker after WithUserQuestionBroker")
	}
	if !HasUserQuestionBroker(ctx) {
		t.Fatal("expected HasUserQuestionBroker=true")
	}
}

func sampleQuestions() []UserQuestion {
	return []UserQuestion{
		{Header: "Format", Question: "fmt?", MultiSelect: false, Options: []UserQuestionOption{{Label: "JSON"}, {Label: "YAML"}}},
		{Header: "Scope", Question: "scope?", MultiSelect: true, Options: []UserQuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}},
	}
}

func TestUserQuestionUIState_SingleSelectDefaultAndFollowsCursor(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []UserQuestionAnswer, 1))

	st := s.GetUserQuestionUIState()
	if !st.Selected[0][0] {
		t.Error("expected the first option to be pre-selected by default")
	}

	s.SetUserQuestionOptionCursor(1)
	if st.Selected[0][0] || !st.Selected[0][1] {
		t.Errorf("expected selection to follow the cursor to option 1, got %v", st.Selected[0])
	}
}

func TestUserQuestionUIState_RecommendedDefault(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState([]UserQuestion{
		{Header: "Fmt", Question: "q", MultiSelect: false, Options: []UserQuestionOption{
			{Label: "JSON"}, {Label: "YAML (Recommended)"},
		}},
	}, make(chan []UserQuestionAnswer, 1))

	st := s.GetUserQuestionUIState()
	if st.Selected[0][0] || !st.Selected[0][1] {
		t.Errorf("expected the (Recommended) option pre-selected, got %v", st.Selected[0])
	}
	if st.OptionCursor != 1 {
		t.Errorf("expected the cursor on the recommended option, got %d", st.OptionCursor)
	}
}

func TestUserQuestionUIState_MultiSelectAccumulates(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []UserQuestionAnswer, 1))

	if s.AdvanceUserQuestion() {
		t.Fatal("did not expect to be done after advancing from q0")
	}
	s.ToggleUserQuestionOption(0)
	s.ToggleUserQuestionOption(2)

	st := s.GetUserQuestionUIState()
	if !st.Selected[1][0] || !st.Selected[1][2] {
		t.Errorf("expected options 0 and 2 selected, got %v", st.Selected[1])
	}

	s.ToggleUserQuestionOption(0)
	if st.Selected[1][0] {
		t.Error("expected option 0 toggled off")
	}
}

func TestUserQuestionUIState_BuildAnswersAndAdvance(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []UserQuestionAnswer, 1))

	if s.AdvanceUserQuestion() {
		t.Fatal("expected not done after q0")
	}
	s.ToggleUserQuestionOption(1)
	s.SetUserQuestionOtherActive(true)
	s.AppendUserQuestionOtherText("custom")
	if !s.AdvanceUserQuestion() {
		t.Fatal("expected done after the last question")
	}

	answers := s.BuildUserQuestionAnswers()
	if len(answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(answers))
	}
	if len(answers[0].SelectedLabels) != 1 || answers[0].SelectedLabels[0] != "JSON" {
		t.Errorf("q0 expected [JSON] (default), got %v", answers[0].SelectedLabels)
	}
	if answers[1].OtherText != "custom" {
		t.Errorf("q1 expected OtherText=custom, got %q", answers[1].OtherText)
	}
}

func TestUserQuestionUIState_BackspaceOtherText(t *testing.T) {
	s := NewApplicationState()
	s.SetupUserQuestionUIState(sampleQuestions(), make(chan []UserQuestionAnswer, 1))

	s.SetUserQuestionOtherActive(true)
	s.AppendUserQuestionOtherText("ab")
	s.BackspaceUserQuestionOtherText()
	if got := s.GetUserQuestionUIState().OtherText[0]; got != "a" {
		t.Errorf("expected Other text 'a', got %q", got)
	}
}

func TestUserQuestionUIState_ClearClosesChannel(t *testing.T) {
	s := NewApplicationState()
	ch := make(chan []UserQuestionAnswer, 1)
	s.SetupUserQuestionUIState(sampleQuestions(), ch)

	s.ClearUserQuestionUIState()
	if s.GetUserQuestionUIState() != nil {
		t.Fatal("expected nil state after clear")
	}
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected the response channel to be closed")
		}
	default:
		t.Fatal("expected a closed channel to be immediately readable")
	}

	s.ClearUserQuestionUIState()
}
