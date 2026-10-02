package config

import (
	"fmt"
	"os"
	"path/filepath"

	configutils "github.com/inference-gateway/cli/config/utils"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

const ToolsFileName = "tools.yaml"

// DefaultToolsConfig seeds the tools policy: every tool enabled, the per-tool
// approval defaults (mutating tools ask, read-only ones do not) and the
// per-mode bash allow-list baseline. It holds the settings that decide
// whether a tool needs approval, so they live in the userspace tools.yaml
// alone with no project copy, the way the sandbox policy keeps its own
// sandbox.yaml.
func DefaultToolsConfig() *ToolsConfig {
	return &ToolsConfig{
		Enabled:        true,
		MaxResultBytes: 250000,
		Sandbox:        *DefaultSandboxConfig(),
		Bash: BashToolConfig{
			Enabled: true,
			Timeout: 120,
			Mode: BashModesConfig{
				All: BashModeAllowConfig{Allow: []string{
					`echo( .*)?`, `ls( .*)?`, `pwd( .*)?`, `tree( .*)?`,
					`wc( .*)?`, `sort( .*)?`, `uniq( .*)?`, `head( .*)?`, `tail( .*)?`,
					`find( .*)?`, `sleep( .*)?`,
					`mkdir( .*)?`, `ln -s( [^ -][^ ]*)+`,
					`git status( .*)?`,
					`git branch( --show-current)?( -[alrvd])?`,
					`git log( .*)?`, `git diff( .*)?`, `git remote( -v)?`, `git show( .*)?`,
					`gh (issue|pr|repo|release|run|workflow) (list|view|status|diff|checks)( .*)?`,
					`gh auth status( .*)?`,
					`gh search (issues|code|prs|repos|commits)( .*)?`,
					`gh project (list|view|item-list|field-list)( .*)?`,
					`gh api repos/[^ ]+/contents/[^ ]+`,
					`gh api '?user/repos[^ ]*'?( --paginate)?( --jq [^ ]+)?`,
					`infer binaries status( .*)?`,
				}},
				Plan:     BashModeAllowConfig{Allow: []string{}},
				Standard: BashModeAllowConfig{Allow: []string{}},
				Auto:     BashModeAllowConfig{Allow: []string{`.*`}},
			},
			BackgroundShells: BackgroundShellsConfig{
				Enabled:            true,
				MaxConcurrent:      5,
				RetentionMinutes:   60,
				CompletedRetention: 5,
			},
		},
		Read: ReadToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{false}[0],
		},
		Write: WriteToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{true}[0],
		},
		Edit: EditToolConfig{
			Enabled:          true,
			RequireApproval:  &[]bool{true}[0],
			StrictWhitespace: false,
		},
		MultiEdit: MultiEditToolConfig{
			RequireApproval: &[]bool{true}[0],
		},
		Delete: DeleteToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{true}[0],
		},
		Grep: GrepToolConfig{
			Enabled:         true,
			Backend:         "auto",
			RequireApproval: &[]bool{false}[0],
		},
		Tree: TreeToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{false}[0],
		},
		WebFetch: WebFetchToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{false}[0],
			AllowedDomains:  []string{"golang.org", "localhost", "github.com", "raw.githubusercontent.com", "patch-diff.githubusercontent.com", "agents.md"},
			Safety: FetchSafetyConfig{
				MaxSize: 10485760,
				Timeout: 30,
			},
			Cache: FetchCacheConfig{
				Enabled: true,
				TTL:     3600,
				MaxSize: 52428800,
			},
		},
		WebSearch: WebSearchToolConfig{
			Enabled:       true,
			DefaultEngine: "duckduckgo",
			MaxResults:    10,
			Engines:       []string{"duckduckgo", "google"},
			Timeout:       10,
		},
		TodoWrite: TodoWriteToolConfig{
			Enabled:         true,
			RequireApproval: &[]bool{false}[0],
		},
		Schedule: ScheduleToolConfig{
			Enabled:         false,
			RequireApproval: &[]bool{true}[0],
			MaxJobs:         100,
		},
		AskUserQuestion: AskUserQuestionToolConfig{
			Enabled: true,
		},
		Wait: WaitToolConfig{
			Enabled:               true,
			MaxTimeoutSeconds:     600,
			CommandPollIntervalMs: 2000,
		},
		ImageGeneration: ImageGenerationToolConfig{
			Enabled: true,
			Model:   "openai/gpt-image-2",
		},
		ImageEdit: ImageEditToolConfig{
			Enabled: true,
			Model:   "openai/gpt-image-2",
		},
		ImageVariation: ImageVariationToolConfig{
			Enabled: true,
			Model:   "openai/gpt-image-2",
		},
		Agent: AgentToolConfig{
			Enabled:            true,
			RequireApproval:    &[]bool{true}[0],
			Mode:               scheddomain.SubagentModeHeadless,
			MaxParallel:        10,
			MaxDepth:           1,
			InheritMock:        true,
			IdleTimeout:        300,
			CompletedRetention: 5,
		},
		Safety: SafetyConfig{
			RequireApproval:   true,
			ApprovalBehaviour: ApprovalBehaviourPrompt,
		},
	}
}

// LoadTools reads tools.yaml over the defaults, so a missing file or a missing
// key keeps the default value.
func LoadTools(path string) (*ToolsConfig, error) {
	return configutils.LoadYAMLMerged(path, "tools", DefaultToolsConfig)
}

// SaveTools writes the tools config to disk, creating parent directories.
func SaveTools(path string, cfg *ToolsConfig) error {
	return configutils.SaveYAML(path, "tools", cfg)
}

// UserToolsPath is ~/.infer/tools.yaml, the only file the tools config is read
// from. The file tools refuse to write it.
func UserToolsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, ToolsFileName), nil
}
