package domain

import (
	"context"
	"strings"
)

// KeepCachedBinary is the release to install when the cached binary stays.
const KeepCachedBinary = ""

const (
	versionTagPrefix = "v"
	maxVersionLength = 32
)

// LatestRelease looks up the tag of the newest gateway release.
type LatestRelease func(ctx context.Context) (string, error)

// CachedBinary is what the gateway binary in the CLI's cache says about
// itself: whether it is there, whether it still starts, and its version.
type CachedBinary struct {
	Present       bool
	KilledAtStart bool
	Version       string
}

// Usable reports whether the cached binary can serve at all.
func (b CachedBinary) Usable() bool {
	return b.Present && !b.KilledAtStart
}

// Is reports whether the cached binary is the release tagged tag. A binary
// that cannot say its version is not known to be any release.
func (b CachedBinary) Is(tag string) bool {
	return b.Usable() && b.Version != "" && b.Version == CleanVersion(tag)
}

// WantedRelease is the gateway release the configuration asks for: the one
// tagged PinnedTag, or the latest release when no tag is pinned.
type WantedRelease struct {
	PinnedTag string
}

// ToInstall names the release to put in place of the cached binary, or
// KeepCachedBinary. A pinned release is never updated past and needs no
// lookup. Following the latest keeps a usable binary when the lookup fails or
// the binary cannot say its version, so a start never breaks on GitHub.
func (w WantedRelease) ToInstall(ctx context.Context, cached CachedBinary, latest LatestRelease) (string, error) {
	if w.PinnedTag != "" {
		if cached.Is(w.PinnedTag) {
			return KeepCachedBinary, nil
		}
		return w.PinnedTag, nil
	}

	tag, err := latest(ctx)
	if err != nil {
		if cached.Usable() {
			return KeepCachedBinary, nil
		}
		return "", err
	}
	if cached.Usable() && (cached.Version == "" || cached.Is(tag)) {
		return KeepCachedBinary, nil
	}
	return tag, nil
}

// CleanVersion normalizes a version ("v0.50.0" -> "0.50.0") and rejects values
// that do not look like one (HTML bodies, "latest", "sha256:...").
func CleanVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), versionTagPrefix)
	if v == "" || len(v) > maxVersionLength || v[0] < '0' || v[0] > '9' {
		return ""
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '+':
		default:
			return ""
		}
	}
	return v
}
