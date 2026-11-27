package tools

import (
	"bytes"
	"cmp"
	"embed"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	yaml "gopkg.in/yaml.v3"

	skills "github.com/inference-gateway/cli/internal/skills"
)

// agentTemplates holds the role templates infer agents create writes an agent
// from, one per role, default.md for every other role.
//
//go:embed agent_templates
var agentTemplates embed.FS

const (
	agentTemplatesRoot   = "agent_templates"
	defaultAgentTemplate = "default"
)

// ErrAgentExists is returned when an agent file would overwrite one.
var ErrAgentExists = errors.New("agent file exists")

// AgentSpec is an agent to write: its name and role, and the frontmatter the
// user chose. An empty Description takes the role template's.
type AgentSpec struct {
	Name        string
	Role        string
	Description string
	Emoji       string
	Model       string
	Workspace   string
	Tools       []string
}

// AgentTemplateRoles names the roles that have a template of their own.
func AgentTemplateRoles() []string {
	entries, _ := agentTemplates.ReadDir(agentTemplatesRoot)
	roles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if role := strings.TrimSuffix(entry.Name(), ".md"); role != defaultAgentTemplate {
			roles = append(roles, role)
		}
	}
	return roles
}

// RenderAgent renders spec's agent file from its role's template: the
// frontmatter in a fixed order, then the body with the name and role filled in.
func RenderAgent(spec AgentSpec) ([]byte, error) {
	if !ValidAgentName(spec.Name) {
		return nil, fmt.Errorf("agent name %q: lowercase letters, digits, hyphens and underscores, at most %d", spec.Name, markdownAgentNameMaxLen)
	}
	if strings.TrimSpace(spec.Role) == "" {
		return nil, errors.New("an agent needs a role, such as tester")
	}
	if len(strings.TrimSpace(spec.Role)) > markdownAgentRoleMaxLen {
		return nil, fmt.Errorf("agent role %q is longer than %d characters", spec.Role, markdownAgentRoleMaxLen)
	}
	description, body, err := renderAgentTemplate(spec)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("---\n")
	writeFrontmatterLine(&out, "name", spec.Name)
	writeFrontmatterLine(&out, "description", cmp.Or(strings.TrimSpace(spec.Description), description))
	writeFrontmatterLine(&out, "role", strings.TrimSpace(spec.Role))
	writeFrontmatterLine(&out, "emoji", strings.TrimSpace(spec.Emoji))
	writeFrontmatterLine(&out, "model", strings.TrimSpace(spec.Model))
	writeFrontmatterLine(&out, "workspace", strings.TrimSpace(spec.Workspace))
	writeFrontmatterList(&out, "tools", spec.Tools)
	out.WriteString("---\n\n")
	out.WriteString(strings.TrimSpace(body))
	out.WriteString("\n")
	return out.Bytes(), nil
}

// writeFrontmatterLine writes key: value, quoted the YAML way when the value
// needs it, and nothing for an empty value. An emoji is single-quoted as typed,
// since yaml escapes it to \U0001F9EA, which nobody edits by hand.
func writeFrontmatterLine(out *bytes.Buffer, key, value string) {
	if value == "" {
		return
	}
	if key == "emoji" {
		fmt.Fprintf(out, "%s: '%s'\n", key, strings.ReplaceAll(value, "'", "''"))
		return
	}
	scalar, _ := yaml.Marshal(value)
	fmt.Fprintf(out, "%s: %s", key, scalar)
}

// writeFrontmatterList writes key with one "- item" line per value, nothing for
// an empty list.
func writeFrontmatterList(out *bytes.Buffer, key string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n", key)
	for _, value := range values {
		fmt.Fprintf(out, "  - %s\n", strings.TrimSpace(value))
	}
}

// WriteAgent writes spec's agent as dir/<name>.md and returns the path. An
// existing file is kept unless overwrite is set.
func WriteAgent(dir string, spec AgentSpec, overwrite bool) (string, error) {
	data, err := RenderAgent(spec)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, spec.Name+".md")
	if _, err := os.Stat(target); err == nil && !overwrite {
		return target, fmt.Errorf("%w: %s", ErrAgentExists, target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return target, os.WriteFile(target, data, 0o644)
}

// renderAgentTemplate fills the role's template with spec and returns its
// default description and body.
func renderAgentTemplate(spec AgentSpec) (string, string, error) {
	raw, err := agentTemplates.ReadFile(path.Join(agentTemplatesRoot, agentTemplateName(spec.Role)+".md"))
	if err != nil {
		return "", "", err
	}
	tmpl, err := template.New(spec.Role).Parse(string(raw))
	if err != nil {
		return "", "", err
	}
	var filled bytes.Buffer
	if err := tmpl.Execute(&filled, map[string]string{"Name": spec.Name, "Role": strings.TrimSpace(spec.Role)}); err != nil {
		return "", "", err
	}
	front, body, err := skills.SplitFrontmatter(filled.Bytes())
	if err != nil {
		return "", "", err
	}
	var fm struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return "", "", err
	}
	return fm.Description, body, nil
}

// agentTemplateName picks the template for role: the role as a slug, "tester",
// then its last word, so "code reviewer" takes reviewer.md, else default.
func agentTemplateName(role string) string {
	words := strings.Fields(strings.ToLower(role))
	candidates := []string{strings.Join(words, "-")}
	if len(words) > 1 {
		candidates = append(candidates, words[len(words)-1])
	}
	for _, candidate := range candidates {
		if _, err := agentTemplates.ReadFile(path.Join(agentTemplatesRoot, candidate+".md")); err == nil {
			return candidate
		}
	}
	return defaultAgentTemplate
}
