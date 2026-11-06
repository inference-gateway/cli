package domain

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHubIssue is a minimal projection of a GitHub issue, big enough for both
// the autocomplete dropdown (Number, Title, State) and inline expansion into a
// chat-message block (Body, URL, Comments, UpdatedAt). Comments is nil for the
// list variant and populated for the view variant.
type GitHubIssue struct {
	Number    int
	Title     string
	Body      string
	State     string
	URL       string
	UpdatedAt time.Time
	Author    string
	Comments  []GitHubIssueComment
}

// GitHubIssueComment is a single comment on a GitHub issue, sorted by
// CreatedAt ascending.
type GitHubIssueComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

// GitHubIssueService provides cached access to the current repository's GitHub
// issues via the gh CLI. Implementations gracefully degrade (return empty/nil
// with no error) when not in a git repo, when gh is not installed, or when the
// remote/auth is not configured - the chat input's "#" autocomplete and "#N"
// inline expansion simply become no-ops in those environments.
type GitHubIssueService interface {
	// ListIssues returns recent open issues for the current repo, newest first.
	// Results are cached for a short TTL so repeated autocomplete keystrokes
	// don't shell out per character. Returns ([], nil) on environment failures.
	ListIssues(ctx context.Context) ([]GitHubIssue, error)

	// GetIssue fetches an issue with body and the most-recent comments (capped
	// internally). Uncached. Returns (nil, err) on failure so the expansion
	// path can leave the raw token in place.
	GetIssue(ctx context.Context, number int) (*GitHubIssue, error)

	// IsAvailable reports whether the service can serve requests in the
	// current environment. Used by the autocomplete trigger to short-circuit
	// a slow first shell-out when gh / repo / auth are missing.
	IsAvailable() bool
}

// issueRefRe matches `#<digits>` only at start-of-line or after whitespace, so
// fragments like "phone-555#anchor" or "abc#1" don't get expanded. Word boundary
// at the tail prevents partial-number false matches inside longer strings.
var issueRefRe = regexp.MustCompile(`(^|\s)#([0-9]+)\b`)

// maxIssueRefsPerMessage caps the distinct issues one message fetches, so a
// pasted list of references cannot stall the caller on sequential fetches.
const maxIssueRefsPerMessage = 10

// ExpandIssueReferences replaces `#N` tokens in content with an inline block of
// the issue's title, body and (capped) recent comments, so the model gets the
// context without spending a turn on `gh issue view`. Duplicate tokens share one
// fetch. A failed fetch is reported by number and, like a token past the first
// maxIssueRefsPerMessage distinct ones, stays in place as the raw token.
func ExpandIssueReferences(ctx context.Context, svc GitHubIssueService, content string) (string, map[int]error) {
	if svc == nil {
		return content, nil
	}
	matches := issueRefRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	fetched := map[int]string{}
	var failures map[int]error
	for _, m := range matches {
		number, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if _, ok := fetched[number]; ok {
			continue
		}
		if len(fetched)+len(failures) >= maxIssueRefsPerMessage {
			break
		}
		issue, err := svc.GetIssue(ctx, number)
		if err != nil || issue == nil {
			if failures == nil {
				failures = map[int]error{}
			}
			failures[number] = err
			continue
		}
		fetched[number] = formatIssueBlock(issue)
	}

	if len(fetched) == 0 {
		return content, failures
	}

	return issueRefRe.ReplaceAllStringFunc(content, func(match string) string {
		sub := issueRefRe.FindStringSubmatch(match)
		number, err := strconv.Atoi(sub[2])
		if err != nil {
			return match
		}
		block, ok := fetched[number]
		if !ok {
			return match
		}
		return sub[1] + block
	}), failures
}

// formatIssueBlock renders one issue for the model: title, state and URL, then
// the body and the most recent comments.
func formatIssueBlock(issue *GitHubIssue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GitHub Issue #%d (%s): %s\nURL: %s\n\n%s\n",
		issue.Number, issue.State, issue.Title, issue.URL, issue.Body)
	if len(issue.Comments) > 0 {
		fmt.Fprintf(&b, "\n--- Comments (last %d) ---\n", len(issue.Comments))
		for _, comment := range issue.Comments {
			fmt.Fprintf(&b, "[@%s, %s]: %s\n",
				comment.Author, comment.CreatedAt.Format("2006-01-02"), comment.Body)
		}
	}
	return b.String()
}
