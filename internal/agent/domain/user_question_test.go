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
