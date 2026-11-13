package infrastructure

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	gatewaydomain "github.com/inference-gateway/cli/internal/gateway/domain"
	github "github.com/inference-gateway/cli/internal/github"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	process "github.com/inference-gateway/cli/internal/platform/process"
)

const (
	gatewayVersionFlag       = "--version"
	gatewayProbeTimeout      = 5 * time.Second
	gatewayReleaseAssetURL   = "https://github.com/inference-gateway/inference-gateway/releases/download/%s/inference-gateway_%s_%s.%s"
	gatewayArchiveExt        = "tar.gz"
	gatewayWindowsArchiveExt = "zip"
	gatewayBinaryDirMode     = 0o755
	windowsOperatingSystem   = "windows"
	gatewayBinaryDirName     = "bin"
	gatewayBinaryName        = "inference-gateway"
	gatewayWindowsBinaryName = "inference-gateway.exe"
)

// BinaryInstaller keeps the gateway binary in ~/.infer/bin the release the
// configuration asks for. It is the one place the CLI downloads the gateway,
// and it writes what it does to notices.
type BinaryInstaller struct {
	notices io.Writer
}

func NewBinaryInstaller(notices io.Writer) *BinaryInstaller {
	return &BinaryInstaller{notices: notices}
}

// Path returns where the managed gateway binary lives.
func (i *BinaryInstaller) Path() string {
	return gatewayBinaryPath()
}

// Probe asks the cached gateway binary what it is.
func (i *BinaryInstaller) Probe(ctx context.Context) gatewaydomain.CachedBinary {
	return probeCachedBinary(ctx, gatewayBinaryPath())
}

// Ensure makes the cached gateway binary the wanted release and returns its
// path. It downloads from GitHub only when the cached binary is not that
// release.
func (i *BinaryInstaller) Ensure(ctx context.Context, wanted gatewaydomain.WantedRelease) (string, error) {
	binaryPath := gatewayBinaryPath()
	if err := os.MkdirAll(filepath.Dir(binaryPath), gatewayBinaryDirMode); err != nil {
		return "", fmt.Errorf("failed to create binary directory: %w", err)
	}

	cached := probeCachedBinary(ctx, binaryPath)
	tag, err := wanted.ToInstall(ctx, cached, latestReleaseFor(cached))
	if err != nil {
		return "", err
	}
	if tag == gatewaydomain.KeepCachedBinary {
		return binaryPath, nil
	}

	if cached.Present {
		_, _ = fmt.Fprintf(i.notices, "• Replacing gateway binary with %s...\n", tag)
	} else {
		_, _ = fmt.Fprintf(i.notices, "• Downloading gateway binary %s...\n", tag)
	}

	assetURL, err := gatewayAssetURL(tag)
	if err != nil {
		return "", err
	}
	if err := downloadAndExtractGatewayBinary(ctx, assetURL, binaryPath); err != nil {
		return "", err
	}

	_, _ = fmt.Fprintln(i.notices, "• Gateway binary downloaded successfully")
	logger.Info("gateway binary installed successfully", "path", binaryPath, "version", tag, "replaced", cached.Version)
	return binaryPath, nil
}

// gatewayAssetURL returns where the release tagged tag publishes the gateway
// binary for this platform.
func gatewayAssetURL(tag string) (string, error) {
	assetOS, assetArch, err := gatewayAssetPlatform()
	if err != nil {
		return "", err
	}
	assetExt := gatewayArchiveExt
	if runtime.GOOS == windowsOperatingSystem {
		assetExt = gatewayWindowsArchiveExt
	}
	return fmt.Sprintf(gatewayReleaseAssetURL, tag, assetOS, assetArch, assetExt), nil
}

// latestReleaseFor returns the GitHub lookup of the latest release. With a
// usable cached binary the lookup gets the probe's short budget, so a slow
// GitHub never delays a start that already has a gateway to run.
func latestReleaseFor(cached gatewaydomain.CachedBinary) gatewaydomain.LatestRelease {
	if !cached.Usable() {
		return latestGatewayTag
	}
	return func(ctx context.Context) (string, error) {
		lookupCtx, cancel := context.WithTimeout(ctx, gatewayProbeTimeout)
		defer cancel()
		return latestGatewayTag(lookupCtx)
	}
}

// probeCachedBinary asks the gateway binary at binaryPath for its version. A
// binary the system ends with a signal is reported as killed at start, and
// one that fails or prints nothing as present without a version.
func probeCachedBinary(ctx context.Context, binaryPath string) gatewaydomain.CachedBinary {
	if _, err := os.Stat(binaryPath); err != nil {
		return gatewaydomain.CachedBinary{}
	}

	probeCtx, cancel := context.WithTimeout(ctx, gatewayProbeTimeout)
	defer cancel()

	out, err := process.CommandContext(probeCtx, binaryPath, gatewayVersionFlag).Output()
	if probeCtx.Err() == nil && killedBySignal(err) {
		return gatewaydomain.CachedBinary{Present: true, KilledAtStart: true}
	}
	fields := strings.Fields(string(out))
	if err != nil || len(fields) == 0 {
		return gatewaydomain.CachedBinary{Present: true}
	}
	return gatewaydomain.CachedBinary{Present: true, Version: gatewaydomain.CleanVersion(fields[len(fields)-1])}
}

// killedBySignal reports whether a command was ended by a signal instead of
// exiting on its own. macOS ends a signed binary that way when its file was
// rewritten in place, and such a binary never starts again.
func killedBySignal(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && !exit.Exited()
}

// latestGatewayTag resolves the latest gateway release tag via the GitHub
// API, sending an Authorization header when a token is available
func latestGatewayTag(ctx context.Context) (string, error) {
	apiURL := "https://api.github.com/repos/inference-gateway/inference-gateway/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create release request: %w", err)
	}
	if t := github.Token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query latest gateway release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", github.ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to query latest gateway release: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode release response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest gateway release has no tag name")
	}
	return release.TagName, nil
}

// gatewayAssetPlatform maps the current OS/arch to the gateway release asset
// naming scheme (inference-gateway_<Os>_<arch>.tar.gz)
func gatewayAssetPlatform() (string, string, error) {
	var assetOS string
	switch runtime.GOOS {
	case "darwin":
		assetOS = "Darwin"
	case "linux":
		assetOS = "Linux"
	case windowsOperatingSystem:
		assetOS = "Windows"
	default:
		return "", "", fmt.Errorf("no gateway binary release for OS %q", runtime.GOOS)
	}

	var assetArch string
	switch runtime.GOARCH {
	case "amd64":
		assetArch = "x86_64"
	case "arm64":
		assetArch = "arm64"
	case "arm":
		assetArch = "armv7"
	default:
		return "", "", fmt.Errorf("no gateway binary release for architecture %q", runtime.GOARCH)
	}

	return assetOS, assetArch, nil
}

// downloadAndExtractGatewayBinary downloads a release archive and extracts
// the inference-gateway binary from it to destPath. Supports .tar.gz and .zip.
func downloadAndExtractGatewayBinary(ctx context.Context, url string, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download gateway release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download gateway release from %s: HTTP %d", url, resp.StatusCode)
	}

	if strings.HasSuffix(url, ".zip") {
		return extractGatewayZip(resp.Body, destPath)
	}

	return extractGatewayTarGz(resp.Body, destPath)
}

// extractGatewayTarGz extracts the inference-gateway binary from a gzipped tarball
func extractGatewayTarGz(r io.Reader, destPath string) error {
	gzReader, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to read gateway release archive: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read gateway release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "inference-gateway" {
			continue
		}

		return installGatewayBinary(destPath, tarReader)
	}

	return fmt.Errorf("inference-gateway binary not found in release archive")
}

// gatewayBinaryMode makes the installed gateway binary executable.
const gatewayBinaryMode = 0o755

// installGatewayBinary writes binary to a new file beside destPath and renames
// it into place. An update must never rewrite the old file: macOS ends a
// signed binary whose file was rewritten in place every time it starts.
func installGatewayBinary(destPath string, binary io.Reader) error {
	staged, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".*")
	if err != nil {
		return fmt.Errorf("failed to create gateway binary: %w", err)
	}
	stagedPath := staged.Name()
	defer func() { _ = os.Remove(stagedPath) }()

	if _, err := io.Copy(staged, binary); err != nil {
		_ = staged.Close()
		return fmt.Errorf("failed to write gateway binary: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("failed to write gateway binary: %w", err)
	}
	if err := os.Chmod(stagedPath, gatewayBinaryMode); err != nil {
		return fmt.Errorf("failed to make gateway binary executable: %w", err)
	}
	if err := os.Rename(stagedPath, destPath); err != nil {
		return fmt.Errorf("failed to install gateway binary: %w", err)
	}
	return nil
}

// extractGatewayZip extracts the inference-gateway.exe binary from a zip archive
func extractGatewayZip(r io.Reader, destPath string) error {
	tmpFile, err := os.CreateTemp("", "infer-gateway-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, r); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp archive: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp archive: %w", err)
	}

	zipReader, err := zip.OpenReader(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read gateway release zip: %w", err)
	}
	defer func() { _ = zipReader.Close() }()

	for _, f := range zipReader.File {
		if filepath.Base(f.Name) != "inference-gateway.exe" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open entry in zip: %w", err)
		}

		err = installGatewayBinary(destPath, rc)
		_ = rc.Close()
		return err
	}

	return fmt.Errorf("inference-gateway.exe binary not found in release archive")
}

// gatewayBinaryPath returns where the managed gateway binary lives.
func gatewayBinaryPath() string {
	home, _ := os.UserHomeDir()
	name := gatewayBinaryName
	if runtime.GOOS == windowsOperatingSystem {
		name = gatewayWindowsBinaryName
	}
	return filepath.Join(home, config.ConfigDirName, gatewayBinaryDirName, name)
}
