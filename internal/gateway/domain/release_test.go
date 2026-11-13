package domain

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

func latestIs(tag string) LatestRelease {
	return func(context.Context) (string, error) { return tag, nil }
}

func latestUnreachable(context.Context) (string, error) {
	return "", errGitHubUnreachable
}

func TestWantedReleaseToInstall(t *testing.T) {
	missing := CachedBinary{}
	killed := CachedBinary{Present: true, KilledAtStart: true}
	silent := CachedBinary{Present: true}
	pinnedCached := CachedBinary{Present: true, Version: testPinnedVersion}
	latestCached := CachedBinary{Present: true, Version: testLatestVersion}

	tests := []struct {
		name    string
		wanted  WantedRelease
		cached  CachedBinary
		latest  LatestRelease
		want    string
		wantErr bool
	}{
		{"pinned and cached is that release", WantedRelease{testPinnedTag}, pinnedCached, latestUnreachable, KeepCachedBinary, false},
		{"pinned and nothing is cached", WantedRelease{testPinnedTag}, missing, latestUnreachable, testPinnedTag, false},
		{"pinned and cached is newer", WantedRelease{testPinnedTag}, latestCached, latestUnreachable, testPinnedTag, false},
		{"pinned and cached is killed at start", WantedRelease{testPinnedTag}, killed, latestUnreachable, testPinnedTag, false},
		{"pinned and cached cannot say its version", WantedRelease{testPinnedTag}, silent, latestUnreachable, testPinnedTag, false},

		{"latest and cached is the latest", WantedRelease{}, latestCached, latestIs(testLatestTag), KeepCachedBinary, false},
		{"latest and cached is older", WantedRelease{}, pinnedCached, latestIs(testLatestTag), testLatestTag, false},
		{"latest and nothing is cached", WantedRelease{}, missing, latestIs(testLatestTag), testLatestTag, false},
		{"latest and cached is killed at start", WantedRelease{}, killed, latestIs(testLatestTag), testLatestTag, false},
		{"latest and cached cannot say its version", WantedRelease{}, silent, latestIs(testLatestTag), KeepCachedBinary, false},
		{"latest unreachable keeps a usable binary", WantedRelease{}, pinnedCached, latestUnreachable, KeepCachedBinary, false},
		{"latest unreachable and nothing is cached", WantedRelease{}, missing, latestUnreachable, "", true},
		{"latest unreachable and cached is killed at start", WantedRelease{}, killed, latestUnreachable, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.wanted.ToInstall(t.Context(), tt.cached, tt.latest)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ToInstall() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ToInstall() = %q, want %q", got, tt.want)
			}
		})
	}
}
