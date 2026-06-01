// Package githubissues provides cached, gh-CLI-backed access to the current
// repository's GitHub issues for the chat input's "#" autocomplete and "#N"
// inline expansion features. It shells out to the user's existing gh
// installation so authentication is inherited automatically; when gh is
// missing, the repo has no remote, or auth has expired, the service returns
// empty results without error so the chat features become silent no-ops.
package githubissues

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	maxResults  = 100
	maxComments = 20
	cacheTTL    = 60 * time.Second
	cmdTimeout  = 5 * time.Second
)

// runnerFunc shells out to gh with the given args. Stubbed in tests.
type runnerFunc func(ctx context.Context, args ...string) ([]byte, error)

// Service implements domain.GitHubIssueService.
type Service struct {
	runner runnerFunc

	mu        sync.Mutex
	cachedAt  time.Time
	cached    []domain.GitHubIssue
	available *bool
}

// New constructs a Service that shells out to the real gh CLI.
func New() *Service {
	return &Service{runner: defaultRunner}
}

func defaultRunner(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	return cmd.Output()
}

// rawIssue / rawComment mirror the gh CLI JSON shape so we can decode it
// without depending on an external github SDK.
type rawIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Comments []rawComment `json:"comments"`
}

type rawComment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListIssues returns recent open issues for the current repo, newest first,
// capped at maxResults. Cached for cacheTTL. Returns ([], nil) on any
// environment failure (no gh, not a repo, not authed) so the autocomplete UI
// can treat absence as "show nothing" rather than as an error.
func (s *Service) ListIssues(ctx context.Context) ([]domain.GitHubIssue, error) {
	s.mu.Lock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < cacheTTL {
		issues := s.cached
		s.mu.Unlock()
		return issues, nil
	}
	s.mu.Unlock()

	cmdCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	out, err := s.runner(cmdCtx,
		"issue", "list",
		"--state", "open",
		"--json", "number,title,state,updatedAt,author",
		"--limit", strconv.Itoa(maxResults),
	)
	if err != nil {
		logger.Debug("githubissues: gh issue list failed - returning empty", "err", err)
		return []domain.GitHubIssue{}, nil
	}

	var raw []rawIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		logger.Debug("githubissues: gh issue list JSON decode failed", "err", err)
		return []domain.GitHubIssue{}, nil
	}

	issues := make([]domain.GitHubIssue, 0, len(raw))
	for _, r := range raw {
		issues = append(issues, domain.GitHubIssue{
			Number:    r.Number,
			Title:     r.Title,
			State:     r.State,
			URL:       r.URL,
			UpdatedAt: r.UpdatedAt,
			Author:    r.Author.Login,
		})
	}
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
	})

	s.mu.Lock()
	s.cached = issues
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return issues, nil
}

// GetIssue fetches a single issue with body and the last maxComments comments.
// Uncached; called once per "#N" token at message-submit time.
func (s *Service) GetIssue(ctx context.Context, number int) (*domain.GitHubIssue, error) {
	if number <= 0 {
		return nil, errors.New("invalid issue number")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	out, err := s.runner(cmdCtx,
		"issue", "view", strconv.Itoa(number),
		"--json", "number,title,body,state,url,updatedAt,author,comments",
	)
	if err != nil {
		return nil, err
	}

	var r rawIssue
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}

	comments := make([]domain.GitHubIssueComment, 0, len(r.Comments))
	for _, c := range r.Comments {
		comments = append(comments, domain.GitHubIssueComment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: c.CreatedAt,
		})
	}
	if len(comments) > maxComments {
		comments = comments[len(comments)-maxComments:]
	}

	return &domain.GitHubIssue{
		Number:    r.Number,
		Title:     r.Title,
		Body:      r.Body,
		State:     r.State,
		URL:       r.URL,
		UpdatedAt: r.UpdatedAt,
		Author:    r.Author.Login,
		Comments:  comments,
	}, nil
}

// IsAvailable reports whether the gh CLI is on PATH and a repo is configured.
// The result is computed lazily on first call and cached for the process
// lifetime - a missing gh today is missing for the rest of the session, and
// the autocomplete UI should not block on `gh repo view` per keystroke.
func (s *Service) IsAvailable() bool {
	s.mu.Lock()
	if s.available != nil {
		v := *s.available
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()

	available := s.probeAvailable()

	s.mu.Lock()
	s.available = &available
	s.mu.Unlock()

	return available
}

func (s *Service) probeAvailable() bool {
	if _, err := exec.LookPath("gh"); err != nil {
		logger.Debug("githubissues: gh not on PATH")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	if _, err := s.runner(ctx, "repo", "view", "--json", "nameWithOwner"); err != nil {
		logger.Debug("githubissues: gh repo view failed - not in a repo or not authed", "err", err)
		return false
	}
	return true
}

// Compile-time interface satisfaction check.
var _ domain.GitHubIssueService = (*Service)(nil)
