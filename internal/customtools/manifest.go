package customtools

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const defaultTimeout = 30 * time.Second

// manifest is a custom tool's manifest: a built-in tool manifest plus the
// command that runs the tool, its timeout in seconds and whether it loads.
type manifest struct {
	agentdomain.ToolManifest `yaml:",inline"`
	Command                  []string `yaml:"command"`
	Timeout                  int      `yaml:"timeout,omitempty"`
	Enabled                  *bool    `yaml:"enabled,omitempty"`
}

// NewTools loads the user's custom tools from tools.custom_dir (default
// ~/.infer/tools), then the project's from .agents/tools and .infer/tools. A
// project tool replaces a user tool of the same name, and .infer/tools wins.
func NewTools(cfg *config.Config, builtins []string) map[string]agentdomain.Tool {
	userDir := absDir(cfg.CustomToolsDir())
	tools := make(map[string]agentdomain.Tool)
	for name, tool := range loadDir(userDir, builtins) {
		tools[name] = tool
	}
	for _, dir := range slices.Backward(config.ProjectToolsDirs()) {
		if dir = absDir(dir); dir == userDir {
			continue
		}
		for name, tool := range loadDir(dir, builtins) {
			tools[name] = newProjectTool(tool)
		}
	}
	return tools
}

// loadDir loads every <Name>.yaml manifest in dir. A manifest that is
// invalid, takes one of the builtins' names or the MCP prefix is skipped with
// a warning.
func loadDir(dir string, builtins []string) map[string]*Tool {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	tools := make(map[string]*Tool, len(paths))
	for _, path := range paths {
		m, err := parseManifest(path, builtins)
		if err != nil {
			logger.Warn("skipping custom tool", "path", path, "error", err)
			continue
		}
		if m.Enabled != nil && !*m.Enabled {
			continue
		}
		tools[m.Name] = newTool(m, dir)
	}
	return tools
}

func absDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func parseManifest(path string, builtins []string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := agentdomain.DecodeToolManifest(data, &m, &m.ToolManifest); err != nil {
		return m, err
	}
	if fileName := strings.TrimSuffix(filepath.Base(path), ".yaml"); m.Name != fileName {
		return m, fmt.Errorf("tool name %q must match the file name", m.Name)
	}
	if isReserved(m.Name, builtins) {
		return m, fmt.Errorf("tool name %q is taken by a built-in or MCP tool", m.Name)
	}
	if len(m.Command) == 0 || m.Command[0] == "" {
		return m, fmt.Errorf("tool %s: command is required", m.Name)
	}
	if m.Timeout < 0 {
		return m, fmt.Errorf("tool %s: timeout must not be negative", m.Name)
	}
	return m, nil
}

// isReserved compares names case-insensitively, as infer tools execute
// resolves them.
func isReserved(name string, builtins []string) bool {
	if strings.HasPrefix(strings.ToUpper(name), mcpdomain.ToolNamePrefix) {
		return true
	}
	return slices.ContainsFunc(builtins, func(builtin string) bool {
		return strings.EqualFold(builtin, name)
	})
}

// resolveCommand resolves a relative program path against the manifest's
// directory and leaves a bare program name for exec to look up on PATH.
func resolveCommand(command []string, dir string) []string {
	resolved := slices.Clone(command)
	if program := resolved[0]; !filepath.IsAbs(program) && filepath.Base(program) != program {
		resolved[0] = filepath.Join(dir, program)
	}
	return resolved
}
