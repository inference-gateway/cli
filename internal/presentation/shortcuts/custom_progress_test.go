package shortcuts

import (
	"context"
	"strings"
	"testing"
)

// A long command shortcut (infer insights) must show its steps while it runs,
// and still hand back everything it printed once it is done.
func TestCustomShortcutReportsStderrProgress(t *testing.T) {
	c := NewCustomShortcut(CustomShortcutConfig{
		Name:    "steps",
		Command: "sh",
		Args:    []string{"-c", "echo 'Reading sessions' >&2; echo report; echo 'Asking model' >&2"},
	}, nil, nil, nil, nil)

	var steps []string
	ctx := WithProgress(context.Background(), func(line string) { steps = append(steps, line) })

	result, err := c.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if len(steps) == 0 || steps[len(steps)-1] != "Asking model" {
		t.Errorf("expected stderr lines reported as progress ending in the last step, got %q", steps)
	}
	for _, want := range []string{"Reading sessions", "report", "Asking model"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("Output lost %q: %q", want, result.Output)
		}
	}
	for _, step := range steps {
		if step == "report" {
			t.Error("stdout must not be reported as progress")
		}
	}
}
