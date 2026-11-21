package headless

import (
	"bytes"
	"context"
	"strings"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func TestTeamChannelPostsWhatTheLeadReads(t *testing.T) {
	tests := []struct {
		name  string
		serve bool
		want  string
	}{
		{"a subprocess prints a line", false, `{"type":"agent_message","to":"reviewer","text":"check main.go"}` + "\n"},
		{"a daemon worker emits a CUSTOM event", true, `"name":"agent_message","value":{"to":"reviewer","text":"check main.go"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(scheddomain.EnvSubagentParentSession, "lead-session")
			var out bytes.Buffer
			lead := scheddomain.TeamChannelFrom(withTeamChannel(context.Background(), &out, tt.serve))
			if lead == nil {
				t.Fatal("a subagent's run needs a channel to its lead")
			}
			if err := lead.Post(scheddomain.TeamMessage{To: "reviewer", Text: "check main.go"}); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); !strings.Contains(got, tt.want) || strings.Count(got, "\n") != 1 {
				t.Fatalf("wrote %q, want one line carrying %s", got, tt.want)
			}
		})
	}
}

func TestATopLevelRunHasNoTeamChannel(t *testing.T) {
	t.Setenv(scheddomain.EnvSubagentParentSession, "")
	if scheddomain.TeamChannelFrom(withTeamChannel(context.Background(), &bytes.Buffer{}, true)) != nil {
		t.Fatal("a run no lead spawned has nobody to post to")
	}
}
