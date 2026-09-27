package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	binariesdomain "github.com/inference-gateway/cli/internal/binaries/domain"
	download "github.com/inference-gateway/cli/internal/platform/download"
)

var _ binariesdomain.Store = (*Store)(nil)

// binariesBase is the release hosting the prebuilt tools (whisper-cli, ffmpeg,
// llama-tts), published as <base>/<name>-<GOOS>-<GOARCH>[.exe] plus a
// checksums.txt with sha256 sums. It tracks the latest release, matching the
// gateway's own downloader so both fill the same ~/.infer/bin/tools cache.
const binariesBase = "https://github.com/inference-gateway/binaries/releases/latest/download"

// installerURL is the POSIX sh installer published by the binaries repo. It
// picks the host's asset, verifies against checksums.txt, downloads through
// gh when authenticated (curl otherwise) and replaces any binary whose sha256
// differs from the release.
const installerURL = "https://raw.githubusercontent.com/inference-gateway/binaries/main/install.sh"

// verified records binaries confirmed current (or freshly installed) in this
// process, so Ensure checks the release at most once per binary. It is reset
// in tests.
var verified sync.Map

// Store installs the prebuilt tools published by the binaries release into
// ~/.infer/bin/tools on demand, mirroring ModelStore for GGML models.
type Store struct {
	autoDownload bool

	// baseURL, installerURL, client and shLookup are overridable in tests.
	baseURL      string
	installerURL string
	client       *http.Client
	shLookup     func() (string, error)
}

// NewStore creates a Store; autoDownload gates Ensure's release check and
// install, while Install and Status always run.
func NewStore(autoDownload bool) *Store {
	return &Store{
		autoDownload: autoDownload,
		baseURL:      binariesBase,
		installerURL: installerURL,
		client:       http.DefaultClient,
		shLookup:     func() (string, error) { return exec.LookPath("sh") },
	}
}

// toolsBinDir returns the shared prebuilt-tools directory ~/.infer/bin/tools,
// the install.sh default that Desktop and opentask also use.
func toolsBinDir() (string, error) {
	return binDir("tools")
}

// legacyBinDir returns the former download location ~/.infer/bin, which kept
// a second copy of the tools until they were migrated into the tools dir.
func legacyBinDir() (string, error) {
	return binDir("")
}

// binDir returns a directory under ~/.infer/bin.
func binDir(sub string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "bin", sub), nil
}

// assetName returns the release asset name for a binary on this platform.
func assetName(name binariesdomain.Name) string {
	return fmt.Sprintf("%s-%s-%s%s", string(name), runtime.GOOS, runtime.GOARCH, exeSuffix())
}

// exeSuffix returns ".exe" on Windows, matching both the release asset names
// and the gateway's ~/.infer/bin cache layout.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Ensure returns the local path to the named binary under ~/.infer/bin/tools,
// installing it via install.sh when it is missing or its sha256 no longer
// matches the latest release (release checked at most once per process per
// binary). An unreachable release (offline) keeps an existing binary, and
// auto_download disabled never installs or downloads.
func (s *Store) Ensure(ctx context.Context, name binariesdomain.Name) (string, error) {
	dir, err := toolsBinDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, string(name)+exeSuffix())

	needsInstall, err := s.needsInstall(ctx, name, path)
	if err != nil {
		return "", err
	}
	if needsInstall {
		if err := s.runInstaller(ctx, "", []binariesdomain.Name{name}); err != nil {
			return "", err
		}
		markCurrent(name)
	}
	return path, nil
}

// needsInstall reports whether the binary at path must be (re)installed, and
// marks it current for this process when it is confirmed to match the release.
// auto_download disabled never checks the release: it errors on a missing
// binary and keeps an existing one whatever its content, and an unreachable
// release (offline) keeps an existing binary too.
func (s *Store) needsInstall(ctx context.Context, name binariesdomain.Name, path string) (bool, error) {
	fi, statErr := os.Stat(path)
	if statErr != nil || fi.IsDir() {
		if !s.autoDownload {
			return false, fmt.Errorf("%s not found at %s and auto_download is disabled", name, path)
		}
		return true, nil
	}
	if !s.autoDownload {
		return false, nil
	}
	if _, checked := verified.Load(name); checked {
		return false, nil
	}
	want, err := s.fetchChecksum(ctx, s.baseURL, assetName(name))
	if err != nil {
		return false, nil
	}
	got, err := fileChecksum(path)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if strings.EqualFold(got, want) {
		markCurrent(name)
		return false, nil
	}
	return true, nil
}

// Install runs install.sh for the named binaries (default: all) into
// ~/.infer/bin/tools, upgrading any binary whose sha256 differs from the
// release. version pins a release tag ("" = latest).
func (s *Store) Install(ctx context.Context, version string, names []binariesdomain.Name) error {
	if len(names) == 0 {
		names = binariesdomain.DefaultNames
	}
	if err := s.runInstaller(ctx, version, names); err != nil {
		return err
	}
	for _, name := range names {
		markCurrent(name)
	}
	return nil
}

// Status reports each named binary (default: all) as missing, stale or current
// by comparing its sha256 against the latest release's checksums.txt. It never
// downloads.
func (s *Store) Status(ctx context.Context, names []binariesdomain.Name) ([]binariesdomain.Status, error) {
	if len(names) == 0 {
		names = binariesdomain.DefaultNames
	}
	sums, err := s.checksums(ctx, s.baseURL)
	if err != nil {
		return nil, err
	}
	dir, err := toolsBinDir()
	if err != nil {
		return nil, err
	}

	statuses := make([]binariesdomain.Status, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, string(name)+exeSuffix())
		st := binariesdomain.Status{Name: name, Path: path, State: binariesdomain.Missing}
		if want, ok := sums[assetName(name)]; !ok {
			st.Detail = fmt.Sprintf("no prebuilt %s in the release for %s/%s", assetName(name), runtime.GOOS, runtime.GOARCH)
		} else if got, err := fileChecksum(path); err == nil {
			if strings.EqualFold(got, want) {
				st.State = binariesdomain.Current
			} else {
				st.State = binariesdomain.Stale
			}
		}
		statuses = append(statuses, st)
	}
	return statuses, nil
}

// runInstaller fetches install.sh and runs it with INSTALL_DIR pointed at the
// tools dir and VERSION pinned (empty = latest). Without a POSIX shell it
// falls back to the native Go downloader.
func (s *Store) runInstaller(ctx context.Context, version string, names []binariesdomain.Name) error {
	sh, err := s.shLookup()
	if err != nil {
		return s.installNative(ctx, version, names)
	}

	script, err := s.fetchInstaller(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(script) }()

	dir, err := toolsBinDir()
	if err != nil {
		return err
	}

	cmd := exec.Command(sh, append([]string{script}, namesToStrings(names)...)...) //nolint:gosec // pinned shell running our fetched installer
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+dir, "VERSION="+version)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install.sh failed for %s: %w: %s", strings.Join(namesToStrings(names), ","), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// fetchInstaller downloads install.sh into a temp file and returns its path,
// so a failed fetch fails the caller instead of piping an empty script into sh.
func (s *Store) fetchInstaller(ctx context.Context) (string, error) {
	body, err := s.get(ctx, s.installerURL)
	if err != nil {
		return "", fmt.Errorf("fetching install.sh: %w", err)
	}
	defer func() { _ = body.Close() }()

	tmp, err := os.CreateTemp("", "infer-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	name := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", fmt.Errorf("saving install.sh: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("saving install.sh: %w", err)
	}
	return name, nil
}

// installNative is the fallback when no POSIX shell is available (Windows
// without sh): the Go downloader applies install.sh's rule - keep a binary
// whose sha256 matches the release, replace anything else.
func (s *Store) installNative(ctx context.Context, version string, names []binariesdomain.Name) error {
	base := s.baseURL
	if version != "" {
		base = strings.Replace(s.baseURL, "/releases/latest/download", "/releases/download/"+version, 1)
	}
	dir, err := toolsBinDir()
	if err != nil {
		return err
	}

	for _, name := range names {
		asset := assetName(name)
		want, err := s.fetchChecksum(ctx, base, asset)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, string(name)+exeSuffix())
		if got, err := fileChecksum(path); err == nil && strings.EqualFold(got, want) {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating bin directory: %w", err)
		}
		if err := s.download(ctx, base+"/"+asset, path, want); err != nil {
			return err
		}
	}
	return nil
}

// markCurrent records a binary as current for this process and drops any
// legacy ~/.infer/bin/<name> copy now that the tools dir holds a current one.
func markCurrent(name binariesdomain.Name) {
	verified.Store(name, struct{}{})
	if legacy, err := legacyBinDir(); err == nil {
		_ = os.Remove(filepath.Join(legacy, string(name)+exeSuffix()))
	}
}

// fetchChecksum returns the expected sha256 for asset from base's checksums.txt.
func (s *Store) fetchChecksum(ctx context.Context, base, asset string) (string, error) {
	sums, err := s.checksums(ctx, base)
	if err != nil {
		return "", err
	}
	sum, ok := sums[asset]
	if !ok {
		return "", fmt.Errorf("no prebuilt %s available for %s/%s: install it manually or set the binary path in config", asset, runtime.GOOS, runtime.GOARCH)
	}
	return sum, nil
}

// checksums fetches and parses base's checksums.txt ("<hex>  <asset>" per line).
func (s *Store) checksums(ctx context.Context, base string) (map[string]string, error) {
	body, err := s.get(ctx, base+"/checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("fetching binary checksums: %w", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading binary checksums: %w", err)
	}

	sums := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			sums[fields[1]] = fields[0]
		}
	}
	return sums, nil
}

// fileChecksum returns the sha256 of the file at path.
func fileChecksum(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// download fetches url into dstPath atomically (temp file + rename), verifying
// the sha256 checksum before the file becomes visible, and marks it executable.
func (s *Store) download(ctx context.Context, url, dstPath, wantSum string) error {
	body, err := s.get(ctx, url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", filepath.Base(dstPath), err)
	}
	defer func() { _ = body.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+"-*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	h := sha256.New()
	src := download.NewProgressReader(body, agentdomain.GetToolProgressCallback(ctx), filepath.Base(dstPath), 0)
	if _, err := io.Copy(io.MultiWriter(tmp, h), src); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", filepath.Base(dstPath), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", filepath.Base(dstPath), err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSum) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(dstPath), got, wantSum)
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("marking %s executable: %w", filepath.Base(dstPath), err)
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing %s: %w", filepath.Base(dstPath), err)
	}
	return nil
}

// get issues a GET and returns the body, following GitHub release redirects.
func (s *Store) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}

// namesToStrings adapts []binariesdomain.Name for exec's argv, which is []string.
func namesToStrings(names []binariesdomain.Name) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = string(name)
	}
	return out
}
