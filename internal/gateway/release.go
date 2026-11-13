package gateway

import "context"

// keepCachedBinary is the release to install when the cached binary stays.
const keepCachedBinary = ""

// latestRelease looks up the tag of the newest gateway release.
type latestRelease func(ctx context.Context) (string, error)

// cachedBinary is what the gateway binary in the CLI's cache says about
// itself: whether it is there, whether it still starts, and its version.
type cachedBinary struct {
	present       bool
	killedAtStart bool
	version       string
}

// usable reports whether the cached binary can serve at all.
func (b cachedBinary) usable() bool {
	return b.present && !b.killedAtStart
}

// is reports whether the cached binary is the release tagged tag. A binary
// that cannot say its version is not known to be any release.
func (b cachedBinary) is(tag string) bool {
	return b.usable() && b.version != "" && b.version == cleanVersion(tag)
}

// wantedRelease is the gateway release the configuration asks for: the one
// tagged pinnedTag, or the latest release when no tag is pinned.
type wantedRelease struct {
	pinnedTag string
}

// toInstall names the release to put in place of the cached binary, or
// keepCachedBinary. A pinned release is never updated past and needs no
// lookup. Following the latest keeps a usable binary when the lookup fails or
// the binary cannot say its version, so a start never breaks on GitHub.
func (w wantedRelease) toInstall(ctx context.Context, cached cachedBinary, latest latestRelease) (string, error) {
	if w.pinnedTag != "" {
		if cached.is(w.pinnedTag) {
			return keepCachedBinary, nil
		}
		return w.pinnedTag, nil
	}

	tag, err := latest(ctx)
	if err != nil {
		if cached.usable() {
			return keepCachedBinary, nil
		}
		return "", err
	}
	if cached.usable() && (cached.version == "" || cached.is(tag)) {
		return keepCachedBinary, nil
	}
	return tag, nil
}
