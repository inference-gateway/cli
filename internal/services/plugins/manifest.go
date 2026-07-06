package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// pluginNameRegex mirrors the skills name rules so plugin names are safe as
// directory names.
var pluginNameRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

const pluginNameMaxLen = 64

// Manifest is the Claude Code plugin manifest (.claude-plugin/plugin.json).
// The hooks/commands/mcpServers keys are parsed only to detect presence.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
	Hooks    any `json:"hooks,omitempty"`
	Commands any `json:"commands,omitempty"`
	MCP      any `json:"mcpServers,omitempty"`
}

// parseManifest reads <dir>/.claude-plugin/plugin.json. Returns (nil, nil)
// when the manifest is absent - callers fall back to layout inference.
func parseManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(config.PluginManifestPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading plugin manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", config.PluginManifestPath, err)
	}
	return &m, nil
}

// sanitizePluginName normalizes raw into a valid plugin name (lowercase
// [a-z0-9-], max 64 chars). Errors when nothing valid remains.
func sanitizePluginName(raw string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	prevDash := true // trims leading dashes
	for _, r := range lower {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "", fmt.Errorf("cannot derive a valid plugin name from %q", raw)
	}
	if len(name) > pluginNameMaxLen {
		return "", fmt.Errorf("plugin name %q exceeds %d characters", name, pluginNameMaxLen)
	}
	if !pluginNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid plugin name %q (must match %s)", name, pluginNameRegex.String())
	}
	return name, nil
}
