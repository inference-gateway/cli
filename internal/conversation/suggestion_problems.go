package conversation

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	// problemLookback is how far back the digest reads when the conversation
	// has no user message to start from.
	problemLookback = 10 * time.Minute
	// problemLineChars caps one digest line, so a long error cannot crowd out the rest.
	problemLineChars = 200
	// ownLogPrefix marks the suggestion's own log lines, which must not feed the next one.
	ownLogPrefix = "suggestion:"
	// problemsPreamble tells the model what the digest is and what it may suggest for it.
	problemsPreamble = "Errors the session logged since the last message. They are not part of the conversation. " +
		"If the next message is about one of them, it must ask to file a GitHub issue for it as a follow-up " +
		"and never ask to fix it:"
)

// processLog is the ProblemLog of this process: what its logger recorded.
type processLog struct{}

// ProblemsSince reads the warnings and errors this process logged from moment on.
func (processLog) ProblemsSince(moment time.Time) []convdomain.LoggedProblem {
	logged := logger.ProblemsSince(moment)
	problems := make([]convdomain.LoggedProblem, 0, len(logged))
	for _, problem := range logged {
		problems = append(problems, convdomain.LoggedProblem(problem))
	}
	return problems
}

// ProcessLog is the ProblemLog a session reads its own process's warnings and
// errors from.
func ProcessLog() convdomain.ProblemLog {
	return processLog{}
}

// repeatedProblem is one distinct problem and how often it was logged.
type repeatedProblem struct {
	line   string
	times  int
	latest time.Time
}

// problemDigest reduces a session's logged problems to the few lines the
// suggestion model reads: only this conversation's, repeats collapsed into a
// count, secrets masked, and never more than the limits allow.
func problemDigest(problems []convdomain.LoggedProblem, conversationID string, limits config.ChatSuggestionsLogsConfig, redact func(string) string) string {
	distinct := collapseRepeats(problemsOf(problems, conversationID), redact)
	slices.SortFunc(distinct, func(a, b repeatedProblem) int {
		return cmp.Or(cmp.Compare(b.times, a.times), b.latest.Compare(a.latest))
	})

	var digest strings.Builder
	for _, problem := range distinct[:min(len(distinct), limits.MaxLines)] {
		line := problem.digestLine()
		if digest.Len()+len(line) > limits.MaxChars {
			break
		}
		digest.WriteString(line)
	}
	return strings.TrimRight(digest.String(), "\n")
}

// problemsOf keeps the problems of one conversation. A problem logged without
// a conversation belongs to the process the session runs in, and stays.
func problemsOf(problems []convdomain.LoggedProblem, conversationID string) []convdomain.LoggedProblem {
	return slices.DeleteFunc(slices.Clone(problems), func(problem convdomain.LoggedProblem) bool {
		fromAnotherSession := problem.ConversationID != "" && problem.ConversationID != conversationID
		return fromAnotherSession || strings.HasPrefix(problem.Message, ownLogPrefix)
	})
}

func collapseRepeats(problems []convdomain.LoggedProblem, redact func(string) string) []repeatedProblem {
	var distinct []repeatedProblem
	seen := map[string]int{}
	for _, problem := range problems {
		line := problemLine(problem, redact)
		at, repeated := seen[line]
		if !repeated {
			seen[line] = len(distinct)
			distinct = append(distinct, repeatedProblem{line: line, times: 1, latest: problem.Time})
			continue
		}
		distinct[at].times++
		distinct[at].latest = problem.Time
	}
	return distinct
}

// problemLine is one problem as a person would read it: its severity, what
// was logged and the error behind it, on one capped line.
func problemLine(problem convdomain.LoggedProblem, redact func(string) string) string {
	text := problem.Message
	if problem.Detail != "" {
		text += ": " + problem.Detail
	}
	text = strings.Join(strings.Fields(redact(text)), " ")
	return truncateSuggestionLine(problem.Severity+": "+text, problemLineChars)
}

func (p repeatedProblem) digestLine() string {
	if p.times == 1 {
		return "- " + p.line + "\n"
	}
	return fmt.Sprintf("- %s (%d times)\n", p.line, p.times)
}
