package gateway

import (
	"context"
	"errors"
	"testing"
)

const (
	testPinnedTag     = "v0.64.0"
	testPinnedVersion = "0.64.0"
	testLatestTag     = "v0.65.0"
	testLatestVersion = "0.65.0"
)

var errGitHubUnreachable = errors.New("github is unreachable")

func latestIs(tag string) latestRelease {
	return func(context.Context) (string, error) { return tag, nil }
}

func latestUnreachable(context.Context) (string, error) {
	return "", errGitHubUnreachable
}

func TestWantedReleaseToInstall(t *testing.T) {
	missing := cachedBinary{}
	killed := cachedBinary{present: true, killedAtStart: true}
	silent := cachedBinary{present: true}
	pinnedCached := cachedBinary{present: true, version: testPinnedVersion}
	latestCached := cachedBinary{present: true, version: testLatestVersion}

	tests := []struct {
		name    string
		wanted  wantedRelease
		cached  cachedBinary
		latest  latestRelease
		want    string
		wantErr bool
	}{
		{"pinned and cached is that release", wantedRelease{testPinnedTag}, pinnedCached, latestUnreachable, keepCachedBinary, false},
		{"pinned and nothing is cached", wantedRelease{testPinnedTag}, missing, latestUnreachable, testPinnedTag, false},
		{"pinned and cached is newer", wantedRelease{testPinnedTag}, latestCached, latestUnreachable, testPinnedTag, false},
		{"pinned and cached is killed at start", wantedRelease{testPinnedTag}, killed, latestUnreachable, testPinnedTag, false},
		{"pinned and cached cannot say its version", wantedRelease{testPinnedTag}, silent, latestUnreachable, testPinnedTag, false},

		{"latest and cached is the latest", wantedRelease{}, latestCached, latestIs(testLatestTag), keepCachedBinary, false},
		{"latest and cached is older", wantedRelease{}, pinnedCached, latestIs(testLatestTag), testLatestTag, false},
		{"latest and nothing is cached", wantedRelease{}, missing, latestIs(testLatestTag), testLatestTag, false},
		{"latest and cached is killed at start", wantedRelease{}, killed, latestIs(testLatestTag), testLatestTag, false},
		{"latest and cached cannot say its version", wantedRelease{}, silent, latestIs(testLatestTag), keepCachedBinary, false},
		{"latest unreachable keeps a usable binary", wantedRelease{}, pinnedCached, latestUnreachable, keepCachedBinary, false},
		{"latest unreachable and nothing is cached", wantedRelease{}, missing, latestUnreachable, "", true},
		{"latest unreachable and cached is killed at start", wantedRelease{}, killed, latestUnreachable, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.wanted.toInstall(t.Context(), tt.cached, tt.latest)

			if (err != nil) != tt.wantErr {
				t.Fatalf("toInstall() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("toInstall() = %q, want %q", got, tt.want)
			}
		})
	}
}
