package domain

import "context"

// Name identifies one prebuilt binary published by the
// github.com/inference-gateway/binaries release.
type Name string

const (
	WhisperCLI Name = "whisper-cli"
	FFmpeg     Name = "ffmpeg"
	LlamaTTS   Name = "llama-tts"
)

// DefaultNames is the install.sh default set, used when no names are given.
var DefaultNames = []Name{WhisperCLI, FFmpeg, LlamaTTS}

// State compares a local binary against the latest release's checksums.txt.
type State string

const (
	Missing State = "missing"
	Stale   State = "stale"
	Current State = "current"
)

// Status is the result of checking one local prebuilt binary.
type Status struct {
	Name   Name
	Path   string
	State  State
	Detail string
}

// Store is the CLI's single owner of the prebuilt tools on a local machine:
// it keeps the shared ~/.infer/bin/tools cache current with the binaries
// release. Speech, computer-use and the binaries commands consume it.
type Store interface {
	// Ensure returns the local path to the named binary under
	// ~/.infer/bin/tools, installing it via install.sh when it is missing or
	// its sha256 no longer matches the release. An unreachable release keeps
	// an existing binary.
	Ensure(ctx context.Context, name Name) (string, error)

	// Install runs install.sh for the named binaries (nil = the default set)
	// into ~/.infer/bin/tools with VERSION pinned ("" = latest), upgrading
	// anything whose sha256 differs from the release.
	Install(ctx context.Context, version string, names []Name) error

	// Status reports each named binary (nil = the default set) as missing,
	// stale or current against the latest release. It never downloads.
	Status(ctx context.Context, names []Name) ([]Status, error)
}
