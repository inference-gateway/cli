package githubscheduler

import (
	"bytes"
	"fmt"
	"strings"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	githubsetup "github.com/inference-gateway/cli/internal/github/setup"
	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
)

const (
	// ArtifactNamePrefix is shared by the rendered upload step and the poller.
	ArtifactNamePrefix     = "infer-conversations-"
	uploadArtifactAction   = "actions/upload-artifact@" + githubsetup.UploadArtifactActionVersion
	workflowTimeoutMinutes = 30
)

// WorkflowPath returns the repo-relative path of a job's workflow file.
func WorkflowPath(jobID string) string {
	return ".github/workflows/" + jobID + ".yml"
}

// Workflow YAML is built from structs and marshalled with yaml.v3 so user
// prompts (multiline, colons, quotes) are always escaped correctly.
type workflowDoc struct {
	Name        string              `yaml:"name"`
	On          workflowOn          `yaml:"on"`
	Permissions workflowPermissions `yaml:"permissions"`
	Jobs        map[string]jobSpec  `yaml:"jobs"`
}

type workflowOn struct {
	Schedule         []cronSpec `yaml:"schedule"`
	WorkflowDispatch struct{}   `yaml:"workflow_dispatch"`
}

type cronSpec struct {
	Cron string `yaml:"cron"`
}

type workflowPermissions struct {
	Contents string `yaml:"contents"`
}

type jobSpec struct {
	RunsOn         string `yaml:"runs-on"`
	TimeoutMinutes int    `yaml:"timeout-minutes"`
	Steps          []step `yaml:"steps"`
}

type step struct {
	Name string            `yaml:"name,omitempty"`
	ID   string            `yaml:"id,omitempty"`
	If   string            `yaml:"if,omitempty"`
	Uses string            `yaml:"uses,omitempty"`
	With any               `yaml:"with,omitempty"`
	Env  map[string]string `yaml:"env,omitempty"`
	Run  string            `yaml:"run,omitempty"`
}

// literalString marshals as a YAML literal block scalar (|) so the value sits
// on its own lines, immune to inline quoting edge cases.
type literalString string

func (s literalString) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.LiteralStyle, Value: string(s)}, nil
}

// inferActionInputs mirrors the provider pass-through list of
// githubsetup.workflowAgentInputs; a struct keeps the rendered key order stable.
type inferActionInputs struct {
	DirectPrompt      literalString `yaml:"direct-prompt"`
	Model             string        `yaml:"model"`
	GithubToken       string        `yaml:"github-token"`
	GithubAppSlug     string        `yaml:"github-app-slug"`
	AnthropicAPIKey   string        `yaml:"anthropic-api-key"`
	OpenAIAPIKey      string        `yaml:"openai-api-key"`
	GoogleAPIKey      string        `yaml:"google-api-key"`
	DeepseekAPIKey    string        `yaml:"deepseek-api-key"`
	GroqAPIKey        string        `yaml:"groq-api-key"`
	MistralAPIKey     string        `yaml:"mistral-api-key"`
	CloudflareAPIKey  string        `yaml:"cloudflare-api-key"`
	CohereAPIKey      string        `yaml:"cohere-api-key"`
	OllamaCloudAPIKey string        `yaml:"ollama-cloud-api-key"`
	MoonshotAPIKey    string        `yaml:"moonshot-api-key"`
	MinimaxAPIKey     string        `yaml:"minimax-api-key"`
	NvidiaAPIKey      string        `yaml:"nvidia-api-key"`
	ZaiAPIKey         string        `yaml:"zai-api-key"`
}

type uploadArtifactInputs struct {
	Name           string `yaml:"name"`
	Path           string `yaml:"path"`
	IfNoFilesFound string `yaml:"if-no-files-found"`
}

// appTokenInputs configures actions/create-github-app-token: the workflow runs
// infer-action under the infer GitHub App bot identity. `repositories` is
// omitted on purpose - schedule events carry no event payload, and the default
// scopes the token to the installation's repos on the owner.
type appTokenInputs struct {
	ClientID   string `yaml:"client-id"`
	PrivateKey string `yaml:"private-key"`
	Owner      string `yaml:"owner"`
}

type checkoutInputs struct {
	Token string `yaml:"token"`
}

// appSecretNames resolves the configured Actions secret names for the GitHub
// App credentials, falling back to the defaults when unset.
func appSecretNames(cfg config.SchedulerGitHubConfig) (clientID, privateKey string) {
	clientID = cfg.AppClientIDSecret
	if clientID == "" {
		clientID = config.DefaultAppClientIDSecret
	}
	privateKey = cfg.AppPrivateKeySecret
	if privateKey == "" {
		privateKey = config.DefaultAppPrivateKeySecret
	}
	return clientID, privateKey
}

// RenderWorkflow renders one job's Actions workflow. ghCron must already be
// translated via TranslateCron; defaultModel is used when the job has none.
func RenderWorkflow(job *scheddomain.ScheduledJob, ghCron, defaultModel string, cfg config.SchedulerGitHubConfig) ([]byte, error) {
	clientIDSecret, privateKeySecret := appSecretNames(cfg)
	model := job.Model
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		model = githubsetup.DefaultWorkflowModel
	}
	name := job.Name
	if name == "" {
		name = "infer job " + shortID(job.ID)
	}

	permissions := workflowPermissions{Contents: "read"}
	steps := []step{
		{
			Name: "Generate GitHub App token",
			ID:   "app-token",
			Uses: "actions/create-github-app-token@" + githubsetup.AppTokenActionVersion,
			With: appTokenInputs{
				ClientID:   fmt.Sprintf("${{ secrets.%s }}", clientIDSecret),
				PrivateKey: fmt.Sprintf("${{ secrets.%s }}", privateKeySecret),
				Owner:      "${{ github.repository_owner }}",
			},
		},
		{
			Uses: "actions/checkout@" + githubsetup.CheckoutActionVersion,
			With: checkoutInputs{Token: "${{ steps.app-token.outputs.token }}"},
		},
		{
			Name: "Run Infer Agent",
			Uses: "inference-gateway/infer-action@" + githubsetup.InferActionVersion,
			With: inferActionInputs{
				DirectPrompt:      literalString(job.Prompt),
				Model:             model,
				GithubToken:       "${{ steps.app-token.outputs.token }}",
				GithubAppSlug:     "${{ steps.app-token.outputs.app-slug }}",
				AnthropicAPIKey:   "${{ secrets.ANTHROPIC_API_KEY }}",
				OpenAIAPIKey:      "${{ secrets.OPENAI_API_KEY }}",
				GoogleAPIKey:      "${{ secrets.GOOGLE_API_KEY }}",
				DeepseekAPIKey:    "${{ secrets.DEEPSEEK_API_KEY }}",
				GroqAPIKey:        "${{ secrets.GROQ_API_KEY }}",
				MistralAPIKey:     "${{ secrets.MISTRAL_API_KEY }}",
				CloudflareAPIKey:  "${{ secrets.CLOUDFLARE_API_KEY }}",
				CohereAPIKey:      "${{ secrets.COHERE_API_KEY }}",
				OllamaCloudAPIKey: "${{ secrets.OLLAMA_CLOUD_API_KEY }}",
				MoonshotAPIKey:    "${{ secrets.MOONSHOT_API_KEY }}",
				MinimaxAPIKey:     "${{ secrets.MINIMAX_API_KEY }}",
				NvidiaAPIKey:      "${{ secrets.NVIDIA_API_KEY }}",
				ZaiAPIKey:         "${{ secrets.ZAI_API_KEY }}",
			},
		},
		{
			Name: "Collect conversation and output files",
			If:   "always()",
			Run:  "cp -r ~/.infer/conversations \"$GITHUB_WORKSPACE/.infer-conversations\" 2>/dev/null || true\nmkdir -p \"$GITHUB_WORKSPACE/.infer-conversations/.artifacts\"\ncp -rn \"$GITHUB_WORKSPACE\"/.infer/artifacts/* ~/.infer/artifacts/* \"$GITHUB_WORKSPACE/.infer-conversations/.artifacts/\" 2>/dev/null || true",
		},
		{
			Name: "Upload conversation artifact",
			If:   "always()",
			Uses: uploadArtifactAction,
			With: uploadArtifactInputs{
				Name:           ArtifactNamePrefix + "${{ github.run_id }}",
				Path:           ".infer-conversations",
				IfNoFilesFound: "ignore",
			},
		},
	}
	if job.RunOnce {
		steps = append(steps, step{
			Name: "Disable after first fire",
			If:   "always()",
			Env:  map[string]string{"GH_TOKEN": "${{ steps.app-token.outputs.token }}"},
			Run:  `gh workflow disable "${{ github.workflow }}" --repo "${{ github.repository }}"`,
		})
	}

	doc := workflowDoc{
		Name:        name,
		On:          workflowOn{Schedule: []cronSpec{{Cron: ghCron}}},
		Permissions: permissions,
		Jobs: map[string]jobSpec{
			"run": {
				RunsOn:         "ubuntu-24.04",
				TimeoutMinutes: workflowTimeoutMinutes,
				Steps:          steps,
			},
		},
	}

	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow for job %s: %w", job.ID, err)
	}
	body = bytes.Replace(body, []byte("\n\"on\":\n"), []byte("\non:\n"), 1)
	header := fmt.Sprintf(`# Generated by infer - scheduled job %s
# name: %s
# original cron: %s
# NOTE: GitHub Actions schedules run in UTC.
`, job.ID, name, job.CronExpression)
	return append([]byte(header), body...), nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// providerSecrets lists the provider API key secrets the rendered workflow
// passes through to infer-action. The CLI never writes secret values.
var providerSecrets = strings.Fields(`ANTHROPIC_API_KEY OPENAI_API_KEY GOOGLE_API_KEY DEEPSEEK_API_KEY
GROQ_API_KEY MISTRAL_API_KEY CLOUDFLARE_API_KEY COHERE_API_KEY OLLAMA_CLOUD_API_KEY
MOONSHOT_API_KEY MINIMAX_API_KEY NVIDIA_API_KEY ZAI_API_KEY`)

// requiredSecrets lists the Actions secrets the rendered workflow references,
// for the PR body: the configured App credential secrets plus the provider keys.
func requiredSecrets(cfg config.SchedulerGitHubConfig) []string {
	clientID, privateKey := appSecretNames(cfg)
	return append([]string{clientID, privateKey}, providerSecrets...)
}
