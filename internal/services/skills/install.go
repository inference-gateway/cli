package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

const (
	githubAPIBase     = "https://api.github.com"
	githubRawBase     = "https://raw.githubusercontent.com"
	installerTimeout  = 30 * time.Second
	installerUA       = "inference-gateway-cli"
	treePartsExpected = 5

	defaultSkillsOrg    = "inference-gateway"
	defaultSkillsRepo   = "skills"
	defaultSkillsRef    = "main"
	defaultSkillsSubdir = "skills"
)

// ExpandShorthand turns shorthand install targets into full GitHub
// tree URLs. Accepted forms:
//
//   - "<skill>"          → https://github.com/inference-gateway/skills/tree/main/skills/<skill>
//   - "<org>/<skill>"    → https://github.com/<org>/skills/tree/main/skills/<skill>
//   - any http(s):// URL → returned unchanged
//
// Anything else (3+ slash-separated segments, empty segments, etc.) is
// returned unchanged so ParseGitHubTreeURL produces its existing error.
func ExpandShorthand(input string) string {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return input
	}
	trimmed := strings.Trim(input, "/")
	if trimmed == "" {
		return input
	}
	parts := strings.Split(trimmed, "/")
	if slices.Contains(parts, "") {
		return input
	}
	switch len(parts) {
	case 1:
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s/%s",
			defaultSkillsOrg, defaultSkillsRepo, defaultSkillsRef, defaultSkillsSubdir, parts[0])
	case 2:
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s/%s",
			parts[0], defaultSkillsRepo, defaultSkillsRef, defaultSkillsSubdir, parts[1])
	default:
		return input
	}
}

// GitHubLocation identifies a directory inside a public GitHub repository,
// parsed out of a /tree/<ref>/<path> URL.
type GitHubLocation struct {
	Owner string
	Repo  string
	Ref   string
	Path  string
}

// ParseGitHubTreeURL accepts URLs of the form
//
//	https://github.com/<owner>/<repo>/tree/<ref>/<path-to-skill>
//
// Refs containing a literal "/" (e.g. "feature/foo" branches) are not
// supported; pass the URL of a tag or single-segment branch instead.
func ParseGitHubTreeURL(rawURL string) (*GitHubLocation, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("URL must use http(s): got %q", u.Scheme)
	}
	if u.Host != "github.com" {
		return nil, fmt.Errorf("only github.com URLs are supported, got %q", u.Host)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[2] == "blob" {
		return nil, fmt.Errorf("URL points to a file (/blob/) - pass the directory URL (/tree/) instead: %s", rawURL)
	}
	if len(parts) < treePartsExpected || parts[2] != "tree" {
		return nil, fmt.Errorf("URL must be of the form https://github.com/<owner>/<repo>/tree/<ref>/<path>: got %s", rawURL)
	}

	return &GitHubLocation{
		Owner: parts[0],
		Repo:  parts[1],
		Ref:   parts[3],
		Path:  strings.Join(parts[4:], "/"),
	}, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type treeResponse struct {
	Tree      []treeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

// Installer downloads a skill folder from a public GitHub repo into a local
// destination directory.
type Installer struct {
	Client  *http.Client
	APIBase string
	RawBase string
}

// NewInstaller returns an Installer pointed at github.com with a 30s HTTP
// timeout. Tests substitute APIBase / RawBase to point at httptest.Server.
func NewInstaller() *Installer {
	return &Installer{
		Client:  &http.Client{Timeout: installerTimeout},
		APIBase: githubAPIBase,
		RawBase: githubRawBase,
	}
}

// InstallFromGitHub downloads the skill folder at rawURL into
// <destBase>/<dirname>/, where dirname is the last path segment of the
// repo URL. Existing folders are rejected unless overwrite is true.
//
// The downloaded folder is post-validated with loadSkill - if frontmatter
// fails the spec checks, the folder is removed and the validation error is
// returned. There is never a half-installed state.
//
// Returns the absolute path of the installed skill on success.
func (i *Installer) InstallFromGitHub(ctx context.Context, rawURL, destBase string, overwrite bool) (string, error) {
	rawURL = ExpandShorthand(rawURL)
	loc, err := ParseGitHubTreeURL(rawURL)
	if err != nil {
		return "", err
	}

	skillDirName := path.Base(loc.Path)
	if skillDirName == "" || skillDirName == "." || skillDirName == "/" {
		return "", fmt.Errorf("could not derive skill directory name from URL path %q", loc.Path)
	}

	destDir := filepath.Join(destBase, skillDirName)
	if _, statErr := os.Stat(destDir); statErr == nil {
		if !overwrite {
			return "", fmt.Errorf("skill already exists at %s (use --overwrite to replace)", destDir)
		}
		if rmErr := os.RemoveAll(destDir); rmErr != nil {
			return "", fmt.Errorf("failed to remove existing skill: %w", rmErr)
		}
	}

	tree, err := i.fetchTree(ctx, loc)
	if err != nil {
		return "", err
	}

	prefix := loc.Path + "/"
	var files []treeEntry
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		if !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		files = append(files, e)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no files found under %s/%s/%s @ %s - check the URL", loc.Owner, loc.Repo, loc.Path, loc.Ref)
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination dir: %w", err)
	}

	for _, f := range files {
		rel := strings.TrimPrefix(f.Path, prefix)
		outPath := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			_ = os.RemoveAll(destDir)
			return "", fmt.Errorf("failed to create dir for %s: %w", rel, err)
		}
		if err := i.downloadFile(ctx, loc, f.Path, outPath); err != nil {
			_ = os.RemoveAll(destDir)
			return "", err
		}
	}

	skill, loadErr := loadSkill(destDir, skillDirName, domain.SkillScopeProject)
	if loadErr != nil {
		_ = os.RemoveAll(destDir)
		return "", fmt.Errorf("downloaded skill failed validation: %s", loadErr.Reason)
	}
	if skill == nil {
		_ = os.RemoveAll(destDir)
		return "", fmt.Errorf("downloaded folder has no SKILL.md at its root")
	}

	abs, err := filepath.Abs(destDir)
	if err != nil {
		return destDir, nil
	}
	return abs, nil
}

func (i *Installer) fetchTree(ctx context.Context, loc *GitHubLocation) (*treeResponse, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", i.APIBase, loc.Owner, loc.Repo, loc.Ref)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tree request: %w", err)
	}
	req.Header.Set("User-Agent", installerUA)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := i.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository or ref not found: %s/%s @ %s", loc.Owner, loc.Repo, loc.Ref)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GitHub API rate limit exceeded (60 req/hour for unauthenticated requests); try again later")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tree treeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to parse tree response: %w", err)
	}
	if tree.Truncated {
		return nil, fmt.Errorf("repository tree was truncated by GitHub (repo too large) - cannot reliably install")
	}
	return &tree, nil
}

// Uninstall removes the skill folder named `name` from `destBase`. The name
// must match the on-disk skill-name regex (lowercase letters, digits and
// hyphens) so callers can't smuggle in path-traversal payloads like `../`.
// Returns an error if the folder doesn't exist or isn't a directory.
func Uninstall(name, destBase string) (string, error) {
	if !nameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid skill name %q (must match %s)", name, nameRegex.String())
	}

	skillDir := filepath.Join(destBase, name)
	info, err := os.Stat(skillDir)
	if err != nil {
		if os.IsNotExist(err) {
			return skillDir, fmt.Errorf("skill %q not found at %s", name, skillDir)
		}
		return skillDir, fmt.Errorf("failed to stat %s: %w", skillDir, err)
	}
	if !info.IsDir() {
		return skillDir, fmt.Errorf("%s exists but is not a directory", skillDir)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return skillDir, fmt.Errorf("failed to remove %s: %w", skillDir, err)
	}

	abs, absErr := filepath.Abs(skillDir)
	if absErr != nil {
		return skillDir, nil
	}
	return abs, nil
}

func (i *Installer) downloadFile(ctx context.Context, loc *GitHubLocation, repoPath, outPath string) error {
	rawURL := fmt.Sprintf("%s/%s/%s/%s/%s", i.RawBase, loc.Owner, loc.Repo, loc.Ref, repoPath)
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", repoPath, err)
	}
	req.Header.Set("User-Agent", installerUA)

	resp, err := i.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", repoPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status %d", repoPath, resp.StatusCode)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", outPath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("failed to write %s: %w", outPath, err)
	}
	return nil
}
