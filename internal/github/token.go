package github

import (
	"errors"
	"os"
	"strings"
)

// ErrRateLimited is returned when the GitHub API answers 403 or 429 because
// the unauthenticated 60 requests/hour quota is exhausted.
var ErrRateLimited = errors.New("GitHub API rate limit exceeded (60 req/hour for unauthenticated requests) - set GITHUB_TOKEN (or GH_TOKEN) to raise the limit to 5,000/hour, or try again later")

// Token returns the GitHub token from the environment, preferring GITHUB_TOKEN
// and falling back to GH_TOKEN (matching the gh CLI). It returns "" when
// neither is set.
func Token() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}
