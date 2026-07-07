package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	skills "github.com/inference-gateway/cli/internal/services/skills"
)

const (
	githubAPIBase    = "https://api.github.com"
	githubRawBase    = "https://raw.githubusercontent.com"
	installerTimeout = 30 * time.Second
	installerUA      = "inference-gateway-cli"
)

// mappedPrefixes is the content subset a plugin install materializes on disk.
var mappedPrefixes = []string{
	config.PluginManifestPath,
	config.PluginAgentsMDName,
	config.HooksFileName,
	"skills/",
}

// detectPrefixes are plugin components infer does not execute or install;
// they are counted so the install summary can report what was ignored.
var detectPrefixes = map[string]string{
	"hooks/":    "hooks",
	"commands/": "commands",
	"agents/":   "agents",
}

// InstallResult describes an inspected (staged or installed) plugin.
type InstallResult struct {
	Name            string
	Version         string
	Description     string
	Skills          []domain.Skill
	SkillErrors     []domain.SkillLoadError
	HasInstructions bool
	InstructionsLen int
	HasHooks        bool
	Hooks           []config.HookCommandConfig
	Unsupported     map[string]int
}

// Installer downloads the mapped subset of a plugin repo from GitHub.
// Tests substitute APIBase / RawBase with httptest servers.
type Installer struct {
	Client  *http.Client
	APIBase string
	RawBase string
	Token   string
}

// NewInstaller returns an Installer pointed at github.com, authenticated via
// GITHUB_TOKEN / GH_TOKEN when set.
func NewInstaller() *Installer {
	return &Installer{
		Client:  &http.Client{Timeout: installerTimeout},
		APIBase: githubAPIBase,
		RawBase: githubRawBase,
		Token:   githubToken(),
	}
}

func githubToken() string {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

func (i *Installer) setAuth(req *http.Request) {
	if i.Token != "" {
		req.Header.Set("Authorization", "Bearer "+i.Token)
	}
}

// isMapped reports whether a repo-relative slash path belongs to the installed subset.
func isMapped(repoPath string) bool {
	for _, p := range mappedPrefixes {
		if repoPath == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(repoPath, p)) {
			return true
		}
	}
	return false
}

// isSafeRelPath rejects repo paths that could escape the staging dir when
// joined: absolute paths, backslashes, and ".." segments.
func isSafeRelPath(repoPath string) bool {
	if repoPath == "" || strings.HasPrefix(repoPath, "/") || strings.Contains(repoPath, "\\") {
		return false
	}
	for _, seg := range strings.Split(repoPath, "/") {
		if seg == ".." || seg == "" {
			return false
		}
	}
	return true
}

// countUnsupported attributes a repo path to a detected-but-ignored component bucket.
func countUnsupported(unsupported map[string]int, repoPath string) {
	for prefix, label := range detectPrefixes {
		if strings.HasPrefix(repoPath, prefix) {
			unsupported[label]++
			return
		}
	}
}

// Stage materializes the mapped subset of src into stagingDir and returns
// the counts of unsupported component files. On error the caller removes stagingDir.
func (i *Installer) Stage(ctx context.Context, src Source, stagingDir string) (map[string]int, error) {
	if src.Kind == SourceLocal {
		return stageLocal(src.Path, stagingDir)
	}
	return i.stageGitHub(ctx, src, stagingDir)
}

func (i *Installer) stageGitHub(ctx context.Context, src Source, stagingDir string) (map[string]int, error) {
	tree, err := i.fetchTree(ctx, src)
	if err != nil {
		return nil, err
	}

	unsupported := map[string]int{}
	var files []string
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		if !isSafeRelPath(e.Path) {
			return nil, fmt.Errorf("repository contains an unsafe path %q - refusing to install", e.Path)
		}
		countUnsupported(unsupported, e.Path)
		if isMapped(e.Path) {
			files = append(files, e.Path)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("nothing to install from %s/%s @ %s: no skills/ or %s found", src.Owner, src.Repo, src.EffectiveRef(), config.PluginAgentsMDName)
	}

	for _, repoPath := range files {
		outPath := filepath.Join(stagingDir, filepath.FromSlash(repoPath))
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create dir for %s: %w", repoPath, err)
		}
		if err := i.downloadFile(ctx, src, repoPath, outPath); err != nil {
			return nil, err
		}
	}
	return unsupported, nil
}

// stageLocal copies the mapped subset of a local plugin directory,
// skipping non-regular files.
func stageLocal(srcDir, stagingDir string) (map[string]int, error) {
	unsupported := map[string]int{}
	found := false
	err := filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		repoPath := filepath.ToSlash(rel)
		countUnsupported(unsupported, repoPath)
		if !isMapped(repoPath) || !isSafeRelPath(repoPath) {
			return nil
		}
		found = true
		outPath := filepath.Join(stagingDir, rel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, data, 0644)
	})
	if err != nil {
		return nil, fmt.Errorf("copying local plugin: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("nothing to install from %s: no skills/ or %s found", srcDir, config.PluginAgentsMDName)
	}
	return unsupported, nil
}

// Inspect validates a staged or installed plugin dir: manifest (optional,
// falling back to fallbackName), skills, and instruction-file presence.
// Errors when the plugin has no valid skill and no AGENTS.md.
func Inspect(dir, fallbackName string) (*InstallResult, error) {
	manifest, err := parseManifest(dir)
	if err != nil {
		return nil, err
	}

	res := &InstallResult{Unsupported: map[string]int{}}
	rawName := fallbackName
	if manifest != nil {
		if manifest.Name != "" {
			rawName = manifest.Name
		}
		res.Version = manifest.Version
		res.Description = manifest.Description
	}
	res.Name, err = sanitizePluginName(rawName)
	if err != nil {
		return nil, err
	}

	if data, err := os.ReadFile(filepath.Join(dir, config.PluginAgentsMDName)); err == nil {
		content := strings.TrimSpace(string(data))
		res.HasInstructions = content != ""
		res.InstructionsLen = len(content)
	}

	hasHooks, hooks, err := inspectPluginHooks(dir, res.Name)
	if err != nil {
		return nil, err
	}
	res.HasHooks = hasHooks
	res.Hooks = hooks

	skillsDir := filepath.Join(dir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sk, loadErr := skills.LoadSkillMetadata(filepath.Join(skillsDir, entry.Name()), entry.Name(), domain.SkillScopePlugin)
			if loadErr != nil {
				res.SkillErrors = append(res.SkillErrors, *loadErr)
				continue
			}
			if sk != nil {
				res.Skills = append(res.Skills, *sk)
			}
		}
	}

	if len(res.Skills) == 0 && !res.HasInstructions {
		return nil, fmt.Errorf("plugin %q has no installable content: no valid skills and no %s", res.Name, config.PluginAgentsMDName)
	}
	return res, nil
}

// inspectPluginHooks reads and validates a plugin's hooks.yaml.
// A missing or empty file is not an error - it simply means no hooks.
// ponytail: extracted to reduce nestif complexity in Inspect.
func inspectPluginHooks(dir, pluginName string) (bool, []config.HookCommandConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, config.HooksFileName))
	if err != nil {
		return false, nil, nil // hooks.yaml is optional
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return false, nil, nil
	}
	var hooksCfg config.HooksConfig
	if err := config.ParseHooksYAML(data, &hooksCfg); err != nil {
		return false, nil, fmt.Errorf("plugin %q has invalid hooks.yaml: %w", pluginName, err)
	}
	if err := hooksCfg.Validate(); err != nil {
		return false, nil, fmt.Errorf("plugin %q has invalid hooks.yaml: %w", pluginName, err)
	}
	return len(hooksCfg.Hooks) > 0, hooksCfg.Hooks, nil
}

// Commit atomically promotes stagingDir to finalDir. On overwrite the old
// dir is trash-renamed first and restored if the promotion fails. Staging and
// final must share a filesystem so os.Rename is atomic.
func Commit(stagingDir, finalDir string, overwrite bool) error {
	if _, err := os.Stat(finalDir); err == nil {
		if !overwrite {
			return fmt.Errorf("plugin already exists at %s (use --overwrite to replace)", finalDir)
		}
		trash := finalDir + ".trash"
		_ = os.RemoveAll(trash)
		if err := os.Rename(finalDir, trash); err != nil {
			return fmt.Errorf("failed to move aside existing plugin: %w", err)
		}
		if err := os.Rename(stagingDir, finalDir); err != nil {
			_ = os.Rename(trash, finalDir)
			return fmt.Errorf("failed to install plugin: %w", err)
		}
		_ = os.RemoveAll(trash)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(finalDir), 0755); err != nil {
		return fmt.Errorf("failed to create plugins dir: %w", err)
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}
	return nil
}

// Uninstall removes the plugin folder named name from root. The name must
// match the plugin-name regex; missing folders are not an error.
func Uninstall(name, root string) (string, error) {
	if !pluginNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid plugin name %q (must match %s)", name, pluginNameRegex.String())
	}
	dir := filepath.Join(root, name)
	if err := os.RemoveAll(dir); err != nil {
		return dir, fmt.Errorf("failed to remove %s: %w", dir, err)
	}
	return dir, nil
}

type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type treeResponse struct {
	Tree      []treeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

func (i *Installer) fetchTree(ctx context.Context, src Source) (*treeResponse, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", i.APIBase, src.Owner, src.Repo, src.EffectiveRef())
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tree request: %w", err)
	}
	req.Header.Set("User-Agent", installerUA)
	req.Header.Set("Accept", "application/vnd.github+json")
	i.setAuth(req)

	resp, err := i.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tree: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("repository or ref not found: %s/%s @ %s", src.Owner, src.Repo, src.EffectiveRef())
	case http.StatusForbidden:
		if i.Token == "" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded (60 req/hour for unauthenticated requests) - set GITHUB_TOKEN (or GH_TOKEN) to raise the limit to 5,000/hour, or try again later")
		}
		return nil, fmt.Errorf("GitHub API request forbidden (403) for %s/%s - the token may lack access or a secondary rate limit was hit; try again later", src.Owner, src.Repo)
	default:
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

func (i *Installer) downloadFile(ctx context.Context, src Source, repoPath, outPath string) error {
	rawURL := fmt.Sprintf("%s/%s/%s/%s/%s", i.RawBase, src.Owner, src.Repo, src.EffectiveRef(), repoPath)
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", repoPath, err)
	}
	req.Header.Set("User-Agent", installerUA)
	i.setAuth(req)

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
