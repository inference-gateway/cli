package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
)

const (
	agentsCatalogRepo    = "inference-gateway/agents"
	agentsCatalogRef     = "main"
	agentsCatalogTimeout = 15 * time.Second
	agentsCatalogMaxByte = 8 << 20
	agentsCatalogOwner   = "github.com/inference-gateway/"
)

// agentsCatalogURL is the published agents catalog (catalog.json in
// inference-gateway/agents @ main) - the same index the registry site renders.
func agentsCatalogURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/catalog.json", agentsCatalogRepo, agentsCatalogRef)
}

// agentCatalogEntry is the slice of a catalog entry the add command needs:
// metadata.name/version, spec.server (listen scheme + port) and the
// deployment/scm fields the image is derived from.
type agentCatalogEntry struct {
	Metadata struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"metadata"`
	Spec struct {
		Server struct {
			Scheme string `json:"scheme"`
			Port   int    `json:"port"`
		} `json:"server"`
		Deployment struct {
			Cloudrun   catalogImageRef `json:"cloudrun"`
			Kubernetes catalogImageRef `json:"kubernetes"`
		} `json:"deployment"`
		SCM struct {
			URL string `json:"url"`
		} `json:"scm"`
	} `json:"spec"`
}

// catalogImageRef is the deployment.<platform>.image block of an entry.
type catalogImageRef struct {
	Image struct {
		Registry   string `json:"registry"`
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
	} `json:"image"`
}

// fetchAgentCatalog downloads the catalog. Nil on any failure - the catalog is
// best-effort, never fatal (same policy as the skills catalog).
func fetchAgentCatalog(ctx context.Context, catalogURL string) []agentCatalogEntry {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	client := &http.Client{Timeout: agentsCatalogTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, agentsCatalogMaxByte))
	if err != nil {
		return nil
	}
	var catalog struct {
		Agents []agentCatalogEntry `json:"agents"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil
	}
	return catalog.Agents
}

// resolveCatalogAgent looks name up in the published agents catalog and
// returns defaults derived the same way the registry site renders its install
// command: URL from spec.server, image from spec.deployment with a ghcr.io
// fallback for inference-gateway-sourced agents, run enabled only when an
// image exists. Nil when the catalog is unreachable or lacks the name.
func resolveCatalogAgent(ctx context.Context, name string) *config.AgentDefaults {
	return resolveCatalogAgentFrom(ctx, agentsCatalogURL(), name)
}

func resolveCatalogAgentFrom(ctx context.Context, catalogURL, name string) *config.AgentDefaults {
	for _, entry := range fetchAgentCatalog(ctx, catalogURL) {
		if entry.Metadata.Name != name {
			continue
		}
		return catalogAgentDefaults(entry)
	}
	return nil
}

// catalogAgentDefaults turns one catalog entry into add-command defaults. The
// URL gets a free port near spec.server.port, matching how the built-in
// defaults avoid collisions.
func catalogAgentDefaults(entry agentCatalogEntry) *config.AgentDefaults {
	scheme := entry.Spec.Server.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port := entry.Spec.Server.Port
	if port == 0 {
		port = 8080
	}

	image := catalogAgentImage(entry)
	return &config.AgentDefaults{
		URL: fmt.Sprintf("%s://localhost:%d", scheme, config.FindAvailablePort(port)),
		OCI: image,
		Run: image != "",
	}
}

// catalogAgentImage derives the OCI reference the same way the registry's
// deriveImage helper does: a deployment-declared image when present, else
// ghcr.io/inference-gateway/<name>:<version> for agents sourced from the
// inference-gateway GitHub org. Empty when neither applies (remote-only agent).
func catalogAgentImage(entry agentCatalogEntry) string {
	deployment := entry.Spec.Deployment
	declared := deployment.Cloudrun.Image
	if declared.Repository == "" {
		declared = deployment.Kubernetes.Image
	}
	if declared.Repository != "" {
		ref := declared.Repository
		if declared.Registry != "" {
			ref = declared.Registry + "/" + ref
		}
		return ref + ":" + catalogImageTag(declared.Tag, entry.Metadata.Version)
	}

	source := strings.ToLower(entry.Spec.SCM.URL)
	if strings.HasPrefix(source, "http://"+agentsCatalogOwner) ||
		strings.HasPrefix(source, "https://"+agentsCatalogOwner) {
		return fmt.Sprintf("ghcr.io/inference-gateway/%s:%s", entry.Metadata.Name, catalogImageTag("", entry.Metadata.Version))
	}
	return ""
}

// catalogImageTag returns the declared tag, falling back to the agent's
// catalog version and finally latest.
func catalogImageTag(tag, version string) string {
	if tag != "" {
		return tag
	}
	if version != "" {
		return version
	}
	return "latest"
}
