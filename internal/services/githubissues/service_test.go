package githubissues

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"
)

// newWithRunner is the test entry point that bypasses the gh binary on PATH.
// It also force-sets availability so callers don't need to also stub the
// `gh repo view` probe.
func newWithRunner(runner runnerFunc, available bool) *Service {
	s := &Service{runner: runner}
	s.available = &available
	return s
}

// fakeRunner returns the same canned response for every invocation and
// records every call so tests can assert call-count for cache behaviour.
type fakeRunner struct {
	calls    int
	response []byte
	err      error
}

func (f *fakeRunner) run(_ context.Context, _ ...string) ([]byte, error) {
	f.calls++
	return f.response, f.err
}

func TestListIssues_HappyPath(t *testing.T) {
	f := &fakeRunner{response: []byte(`[
		{"number":1,"title":"alpha","state":"OPEN","updatedAt":"2024-01-01T00:00:00Z","author":{"login":"alice"}},
		{"number":2,"title":"beta","state":"OPEN","updatedAt":"2024-02-01T00:00:00Z","author":{"login":"bob"}}
	]`)}
	s := newWithRunner(f.run, true)

	issues, err := s.ListIssues(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 2)
	// Sort is by UpdatedAt desc - beta should come first.
	require.Equal(t, 2, issues[0].Number)
	require.Equal(t, "beta", issues[0].Title)
	require.Equal(t, "bob", issues[0].Author)
}

func TestListIssues_CachesWithinTTL(t *testing.T) {
	f := &fakeRunner{response: []byte(`[]`)}
	s := newWithRunner(f.run, true)

	_, err := s.ListIssues(context.Background())
	require.NoError(t, err)
	_, err = s.ListIssues(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, f.calls, "second call within TTL must hit cache")
}

func TestListIssues_GhFailureReturnsEmpty(t *testing.T) {
	f := &fakeRunner{err: errors.New("gh: command failed")}
	s := newWithRunner(f.run, true)

	issues, err := s.ListIssues(context.Background())
	require.NoError(t, err, "gh failures must degrade silently, not error")
	require.Empty(t, issues)
}

func TestListIssues_MalformedJSONReturnsEmpty(t *testing.T) {
	f := &fakeRunner{response: []byte(`not json`)}
	s := newWithRunner(f.run, true)

	issues, err := s.ListIssues(context.Background())
	require.NoError(t, err)
	require.Empty(t, issues)
}

func TestGetIssue_HappyPathWithCommentSlicing(t *testing.T) {
	body := `{"number":42,"title":"feat","body":"hello","state":"OPEN","url":"https://example.com/42","author":{"login":"alice"},"comments":[`
	for i := 0; i < 25; i++ {
		if i > 0 {
			body += ","
		}
		body += `{"author":{"login":"u"},"body":"c` + strconv.Itoa(i) + `","createdAt":"2024-01-` + zeroPad(i+1) + `T00:00:00Z"}`
	}
	body += `]}`

	f := &fakeRunner{response: []byte(body)}
	s := newWithRunner(f.run, true)

	iss, err := s.GetIssue(context.Background(), 42)
	require.NoError(t, err)
	require.NotNil(t, iss)
	require.Equal(t, 42, iss.Number)
	require.Equal(t, "feat", iss.Title)
	require.Len(t, iss.Comments, maxComments, "comments must be tail-sliced to maxComments")
	require.Equal(t, "c5", iss.Comments[0].Body)
	require.Equal(t, "c24", iss.Comments[len(iss.Comments)-1].Body)
}

func TestGetIssue_InvalidNumber(t *testing.T) {
	s := newWithRunner(func(_ context.Context, _ ...string) ([]byte, error) {
		t.Fatal("runner should not be called for invalid issue numbers")
		return nil, nil
	}, true)
	_, err := s.GetIssue(context.Background(), 0)
	require.Error(t, err)
}

func TestGetIssue_GhFailure(t *testing.T) {
	f := &fakeRunner{err: errors.New("gh: not found")}
	s := newWithRunner(f.run, true)
	iss, err := s.GetIssue(context.Background(), 999)
	require.Error(t, err)
	require.Nil(t, iss)
}

func TestIsAvailable_CachedAfterFirstProbe(t *testing.T) {
	s := newWithRunner(nil, false)
	require.False(t, s.IsAvailable())
}

// zeroPad pads a 1- or 2-digit integer to two digits for ISO date format.
// Defined here to keep the test file self-contained.
func zeroPad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// TestCacheTTL_IsHonored exercises the expiry path so we don't regress to
// always caching. It manipulates cachedAt directly because waiting 60s in a
// unit test is unacceptable.
func TestCacheTTL_IsHonored(t *testing.T) {
	f := &fakeRunner{response: []byte(`[]`)}
	s := newWithRunner(f.run, true)

	_, _ = s.ListIssues(context.Background())
	require.Equal(t, 1, f.calls)

	s.mu.Lock()
	s.cachedAt = time.Now().Add(-cacheTTL - time.Second)
	s.mu.Unlock()

	_, _ = s.ListIssues(context.Background())
	require.Equal(t, 2, f.calls, "expired cache must re-fetch")
}
