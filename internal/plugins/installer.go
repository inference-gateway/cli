package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	skills "github.com/inference-gateway/cli/internal/skills"
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
	Skills          []agentdomain.Skill
	SkillErrors     []agentdomain.SkillLoadError
	HasInstructions bool
	InstructionsLen int
	HasHooks        bool
	Hooks           []config.HookCommandConfig
	Unsupported     map[string]int
}

// Installer downloads the mapped subset of a plugin repo from GitHub, reusing
// the skills installer's tree listing and raw download. Tests substitute
// APIBase / RawBase with httptest servers.
type Installer struct {
	skills.Installer
}

// NewInstaller returns an Installer pointed at github.com, authenticated via
// GITHUB_TOKEN / GH_TOKEN when set.
func NewInstaller() *Installer {
	return &Installer{Installer: *skills.NewInstaller("")}
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
	loc := &skills.GitHubLocation{Owner: src.Owner, Repo: src.Repo, Ref: src.EffectiveRef()}
	tree, err := i.FetchTree(ctx, loc)
	if err != nil {
		return nil, err
	}

	unsupported := map[string]int{}
	var files []string
	for _, e := range tree {
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
		if err := i.DownloadFile(ctx, loc, repoPath, outPath); err != nil {
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
			sk, loadErr := skills.LoadSkillMetadata(filepath.Join(skillsDir, entry.Name()), entry.Name(), agentdomain.SkillScopePlugin, res.Name)
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
// ceiling: none - permanent extraction for readability.
// upgrade: re-inline if Inspect is ever rewritten to avoid the extraction.
func inspectPluginHooks(dir, pluginName string) (bool, []config.HookCommandConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, config.HooksFileName))
	if err != nil {
		return false, nil, nil
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
