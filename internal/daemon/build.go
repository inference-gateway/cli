package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"

	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
)

// AckAttrBuild is the field of its hello ack a daemon declares its Build in.
const AckAttrBuild = agentinfra.AttrBuild

// Build identifies the running binary by the SHA-256 of its executable, so a
// chat tells a daemon of another build apart even when both are dev builds of
// one version. It is "" when the executable cannot be read.
var Build = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return fileSHA256(exe)
})

func fileSHA256(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// BuildNotice says that a daemon declaring daemonBuild runs another build than
// this binary and names it to stop, and is "" for the same build. A daemon
// that declares none predates the declaration, so it is another build too.
func BuildNotice(daemonBuild string) string {
	own := Build()
	if own == "" || daemonBuild == own {
		return ""
	}
	return "infer daemon runs another build than this chat, so what this build changed there is missing - stop it" +
		RunningPIDHint() + " and run again"
}

// RunningPIDHint names the running daemon's pid in a sentence, " (pid 4242)",
// and is "" when it is unknown.
func RunningPIDHint() string {
	pid := RunningPID()
	if pid == "" {
		return ""
	}
	return " (pid " + pid + ")"
}
