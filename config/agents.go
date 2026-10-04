package config

import (
	"cmp"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	configutils "github.com/inference-gateway/cli/config/utils"
)

// AgentsConfig represents the agents.yaml configuration file
type AgentsConfig struct {
	Agents []AgentEntry `yaml:"agents" mapstructure:"agents"`

	path string
}

// AgentEntry represents a single A2A agent configuration
type AgentEntry struct {
	Name         string            `yaml:"name" mapstructure:"name"`
	URL          string            `yaml:"url" mapstructure:"url"`
	ArtifactsURL string            `yaml:"artifacts_url,omitempty" mapstructure:"artifacts_url,omitempty"`
	OCI          string            `yaml:"oci,omitempty" mapstructure:"oci,omitempty"`
	Run          bool              `yaml:"run" mapstructure:"run"`
	Model        string            `yaml:"model,omitempty" mapstructure:"model,omitempty"`
	Environment  map[string]string `yaml:"environment,omitempty" mapstructure:"environment,omitempty"`
	Auth         *AgentAuth        `yaml:"auth,omitempty" mapstructure:"auth,omitempty"`
}

// AgentAuth says how to authenticate to an agent: a static bearer token or an
// OIDC client-credentials grant. Secrets are named by environment variable and
// never stored, so agents.yaml stays safe to print and to rewrite.
type AgentAuth struct {
	TokenEnv string     `yaml:"token_env,omitempty" mapstructure:"token_env,omitempty"`
	OIDC     *AgentOIDC `yaml:"oidc,omitempty" mapstructure:"oidc,omitempty"`
}

// AgentOIDC is the client of a client-credentials grant. Where to get the token
// is declared by the agent card's security schemes. IssuerURL is optional: the
// card may then only point at that issuer, and it is used when the card declares none.
type AgentOIDC struct {
	ClientID        string `yaml:"client_id" mapstructure:"client_id"`
	ClientSecretEnv string `yaml:"client_secret_env" mapstructure:"client_secret_env"`
	Audience        string `yaml:"audience,omitempty" mapstructure:"audience,omitempty"`
	IssuerURL       string `yaml:"issuer_url,omitempty" mapstructure:"issuer_url,omitempty"`
}

// DefaultAgentsConfig returns a default agents configuration
func DefaultAgentsConfig() *AgentsConfig {
	return &AgentsConfig{
		Agents: []AgentEntry{},
	}
}

const (
	AgentsFileName    = "agents.yaml"
	DefaultAgentsPath = ConfigDirName + "/" + AgentsFileName
)

// ResolveAgentsPath returns the path agents.yaml should be loaded from,
// matching the A2A AgentCardClient's lookup order: project-level
// `.infer/agents.yaml` wins if present, otherwise the userspace
// `~/.infer/agents.yaml`, otherwise the project default (which may not
// exist yet - LoadAgents handles that gracefully).
func ResolveAgentsPath() string {
	if _, err := os.Stat(DefaultAgentsPath); err == nil {
		return DefaultAgentsPath
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		userspace := filepath.Join(homeDir, ConfigDirName, AgentsFileName)
		if _, err := os.Stat(userspace); err == nil {
			return userspace
		}
	}
	return DefaultAgentsPath
}

// ParseModel parses a model string in the format "provider/model" and returns
// the provider and model separately. If the format is invalid, returns empty strings.
func ParseModel(model string) (provider, modelName string) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

// GetEnvironmentWithModel returns the environment variables with model-related
// variables added if a model is specified.
func (a *AgentEntry) GetEnvironmentWithModel() map[string]string {
	env := make(map[string]string)

	for k, v := range a.Environment {
		env[k] = v
	}

	if a.Model != "" {
		provider, modelName := ParseModel(a.Model)
		if provider != "" && modelName != "" {
			env["A2A_AGENT_CLIENT_PROVIDER"] = provider
			env["A2A_AGENT_CLIENT_MODEL"] = modelName
		}
	}

	return env
}

// LoadAgents reads agents.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use
// defaults" without special-casing.
func LoadAgents(path string) (*AgentsConfig, error) {
	cfg, err := configutils.LoadYAML(path, "agents", DefaultAgentsConfig)
	if err != nil {
		return nil, err
	}
	cfg.path = path
	return cfg, nil
}

// SaveAgents writes the agents configuration to disk, creating any
// missing parent directories.
func SaveAgents(path string, cfg *AgentsConfig) error {
	return configutils.SaveYAML(path, "agents", cfg)
}

func agentName(e AgentEntry) string { return e.Name }

const agentKind = "agent"

// CreateEntry implements CollectionConfig.
func (c *AgentsConfig) CreateEntry(entry AgentEntry) error {
	next, err := appendEntry(c.Agents, entry, entry.Name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// ReadEntry implements CollectionConfig.
func (c *AgentsConfig) ReadEntry(name string) (*AgentEntry, error) {
	return findEntry(c.Agents, name, agentName, agentKind)
}

// UpdateEntry implements CollectionConfig.
func (c *AgentsConfig) UpdateEntry(entry AgentEntry) error {
	next, err := replaceEntry(c.Agents, entry, entry.Name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// DeleteEntry implements CollectionConfig.
func (c *AgentsConfig) DeleteEntry(name string) error {
	next, err := removeEntry(c.Agents, name, agentName, agentKind)
	if err != nil {
		return err
	}
	c.Agents = next
	return SaveAgents(c.path, c)
}

// ListEntries implements CollectionConfig.
func (c *AgentsConfig) ListEntries() []AgentEntry { return c.Agents }

// EntryForURL returns the configured agent served from the origin of rawURL.
// The whole origin is compared, so an agent's credentials never reach another
// service on the same host.
func (c *AgentsConfig) EntryForURL(rawURL string) (AgentEntry, bool) {
	origin := URLOrigin(rawURL)
	if origin == "" {
		return AgentEntry{}, false
	}
	i := slices.IndexFunc(c.Agents, func(agent AgentEntry) bool { return URLOrigin(agent.URL) == origin })
	if i < 0 {
		return AgentEntry{}, false
	}
	return c.Agents[i], true
}

// URLOrigin returns the scheme, host and port of rawURL in a comparable form,
// with the scheme's default port filled in. It returns "" for a URL without a host.
func URLOrigin(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	port := cmp.Or(u.Port(), map[string]string{"http": "80", "https": "443"}[scheme])
	return scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// GetAgentURLs returns URLs of all configured agents.
func GetAgentURLs(path string) ([]string, error) {
	cfg, err := LoadAgents(path)
	if err != nil {
		return nil, err
	}
	agents := cfg.ListEntries()
	urls := make([]string, 0, len(agents))
	for _, agent := range agents {
		urls = append(urls, agent.URL)
	}
	return urls, nil
}

// IsA2AAgentHost reports whether url points at the host of a configured
// A2A agent (its endpoint or artifacts server). Registering an agent is the
// trust decision: the A2A tools instruct the model to WebFetch artifact
// Download URLs, so those hosts must stay fetchable regardless of how
// tools.web_fetch.allowed_domains is overridden. Ports are ignored because
// locally-run agents get their host ports reassigned on collision.
func (c *Config) IsA2AAgentHost(rawURL string) bool {
	if !c.A2A.Enabled {
		return false
	}

	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return false
	}

	bases := append([]string{}, c.A2A.Agents...)
	if agents, err := LoadAgents(ResolveAgentsPath()); err == nil {
		for _, agent := range agents.ListEntries() {
			bases = append(bases, agent.URL, agent.ArtifactsURL)
		}
	}

	for _, base := range bases {
		if base == "" {
			continue
		}
		if u, err := url.Parse(base); err == nil && u.Hostname() != "" &&
			strings.EqualFold(u.Hostname(), target.Hostname()) {
			return true
		}
	}

	return false
}

// IsLocalA2AAgent reports whether url points at an agent this CLI runs itself,
// a run: true entry of agents.yaml. INFER_A2A_AGENTS overrides agents.yaml, so
// every agent is external then. Only the host is compared, as in
// IsA2AAgentHost, so an external agent sharing a local agent's host reads local.
func (c *Config) IsLocalA2AAgent(rawURL string) bool {
	if len(c.A2A.Agents) > 0 {
		return false
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return false
	}
	agents, err := LoadAgents(ResolveAgentsPath())
	if err != nil {
		return false
	}
	return slices.ContainsFunc(agents.ListEntries(), func(agent AgentEntry) bool {
		local, err := url.Parse(agent.URL)
		return agent.Run && err == nil && strings.EqualFold(local.Hostname(), target.Hostname())
	})
}
