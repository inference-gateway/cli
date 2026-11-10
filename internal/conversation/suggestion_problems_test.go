package conversation

import (
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

var (
	roomyLimits = config.ChatSuggestionsLogsConfig{Enabled: true, MaxLines: 5, MaxChars: 600}
	unredacted  = func(s string) string { return s }
)

func problemAt(second int, severity, message, detail, conversationID string) convdomain.LoggedProblem {
	return convdomain.LoggedProblem{
		Time: time.Unix(int64(second), 0), Severity: severity, Message: message, Detail: detail, ConversationID: conversationID,
	}
}

func TestProblemDigest(t *testing.T) {
	tests := []struct {
		name     string
		problems []convdomain.LoggedProblem
		limits   config.ChatSuggestionsLogsConfig
		want     string
	}{
		{
			name:   "nothing logged is no digest",
			limits: roomyLimits,
			want:   "",
		},
		{
			name: "a problem reads as its severity, message and error",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "error", "teleport failed", "pod is in CrashLoopBackOff", "conv-1"),
			},
			limits: roomyLimits,
			want:   "- error: teleport failed: pod is in CrashLoopBackOff",
		},
		{
			name: "repeats collapse into one line with a count",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "warn", "retrying", "", "conv-1"),
				problemAt(2, "warn", "retrying", "", "conv-1"),
				problemAt(3, "warn", "retrying", "", "conv-1"),
			},
			limits: roomyLimits,
			want:   "- warn: retrying (3 times)",
		},
		{
			name: "the most repeated comes first, then the newest",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "warn", "old once", "", ""),
				problemAt(2, "error", "twice", "", ""),
				problemAt(3, "warn", "new once", "", ""),
				problemAt(4, "error", "twice", "", ""),
			},
			limits: roomyLimits,
			want:   "- error: twice (2 times)\n- warn: new once\n- warn: old once",
		},
		{
			name: "another session's problems never appear",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "error", "theirs", "", "conv-2"),
				problemAt(2, "error", "ours", "", "conv-1"),
				problemAt(3, "warn", "the process's own", "", ""),
			},
			limits: roomyLimits,
			want:   "- warn: the process's own\n- error: ours",
		},
		{
			name: "the suggestion's own log lines do not feed the next one",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "warn", "suggestion: request failed", "timeout", "conv-1"),
			},
			limits: roomyLimits,
			want:   "",
		},
		{
			name: "the line cap keeps the first ones",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "warn", "a", "", ""),
				problemAt(2, "warn", "b", "", ""),
				problemAt(3, "warn", "c", "", ""),
			},
			limits: config.ChatSuggestionsLogsConfig{Enabled: true, MaxLines: 2, MaxChars: 600},
			want:   "- warn: c\n- warn: b",
		},
		{
			name: "the character cap drops the lines that do not fit",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "warn", "first", "", ""),
				problemAt(2, "warn", "second", "", ""),
			},
			limits: config.ChatSuggestionsLogsConfig{Enabled: true, MaxLines: 5, MaxChars: 20},
			want:   "- warn: second",
		},
		{
			name: "an error spread over lines is read as one",
			problems: []convdomain.LoggedProblem{
				problemAt(1, "error", "tool failed", "exit status 1\n\tat main.go:12\n", ""),
			},
			limits: roomyLimits,
			want:   "- error: tool failed: exit status 1 at main.go:12",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := problemDigest(tt.problems, "conv-1", tt.limits, unredacted); got != tt.want {
				t.Fatalf("problemDigest() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestProblemDigestMasksSecretsAndCapsALine(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 40)
	maskSecret := func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") }
	problems := []convdomain.LoggedProblem{
		problemAt(1, "error", "push rejected", "token "+secret+" "+strings.Repeat("x", 400), ""),
	}

	digest := problemDigest(problems, "conv-1", roomyLimits, maskSecret)

	if strings.Contains(digest, secret) || !strings.Contains(digest, "[redacted]") {
		t.Fatalf("the digest must carry the mask and not the secret, got %q", digest)
	}
	if len(digest) > len("- ")+problemLineChars+len("...") {
		t.Fatalf("one line is %d characters, past its cap", len(digest))
	}
}
