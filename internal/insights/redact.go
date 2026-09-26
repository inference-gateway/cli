package insights

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// redacted replaces every matched secret in the digest and the report.
const redacted = "[redacted]"

// minSecretLen skips trivially short env values, matching infer-action's
// redact.ts: masking "yes" corrupts more than it protects.
const minSecretLen = 8

// secretEnvNames are the credential variables infer-action's redact.ts masks;
// the provider block mirrors its generated list.
var secretEnvNames = []string{
	"GITHUB_TOKEN",
	"OLLAMA_CLOUD_API_KEY",
	"GROQ_API_KEY",
	"LLAMACPP_API_KEY",
	"OPENAI_API_KEY",
	"CLOUDFLARE_API_KEY",
	"COHERE_API_KEY",
	"ANTHROPIC_API_KEY",
	"DEEPSEEK_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"MINIMAX_API_KEY",
	"MOONSHOT_API_KEY",
	"NVIDIA_API_KEY",
	"ZAI_API_KEY",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"MEMORY_TOKEN",
	"MEMORY_DEPLOY_KEY",
}

// credentialShapes matches forms whose false-positive risk is near zero: the
// PEM private-key block (RSA, DSA, EC, OpenSSH, PKCS#8, PGP) and GitHub's
// reserved token prefixes. Kept in step with infer-action's redact.ts.
var credentialShapes = regexp.MustCompile(strings.Join([]string{
	`-----BEGIN [A-Z ]*PRIVATE KEY( BLOCK)?-----[\s\S]+?-----END [A-Z ]*PRIVATE KEY( BLOCK)?-----`,
	`github_pat_[A-Za-z0-9_]{82,}`,
	`gh[pours]_[A-Za-z0-9]{36,}`,
}, "|"))

// redact masks credential shapes and the values of secret env vars set in this
// environment, so session content reaches the analysis model and the saved
// report without carrying secrets. It runs entirely locally, before any
// network call.
func redact(s string) string {
	if s == "" {
		return s
	}
	out := credentialShapes.ReplaceAllString(s, redacted)
	for _, secret := range collectEnvSecrets() {
		out = strings.ReplaceAll(out, secret, redacted)
		if escaped := jsonEscape(secret); escaped != secret {
			out = strings.ReplaceAll(out, escaped, redacted)
		}
	}
	return out
}

// collectEnvSecrets gathers the secret values currently set, longest first so
// a longer value masks before a shorter value that prefixes it.
func collectEnvSecrets() []string {
	var values []string
	seen := map[string]bool{}
	for _, name := range secretEnvNames {
		v := os.Getenv(name)
		if len(strings.TrimSpace(v)) < minSecretLen || seen[v] {
			continue
		}
		seen[v] = true
		values = append(values, v)
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	return values
}

// jsonEscape renders s the way it appears inside a JSON string, so a secret
// embedded in a JSON log line is masked too.
func jsonEscape(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}
