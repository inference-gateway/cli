package githubscheduler

import (
	"strings"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	githubsetup "github.com/inference-gateway/cli/internal/services/githubsetup"
	yaml "gopkg.in/yaml.v3"
)

func testJob() *domain.ScheduledJob {
	return &domain.ScheduledJob{
		ID:             "0b7f4a2c-1234-5678-9abc-def012345678",
		Name:           "morning briefing",
		CronExpression: "@daily",
		Prompt:         "Summarize my day.\nInclude: weather, calendar, and a quote.",
		Model:          "openai/gpt-4o",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

func TestRenderWorkflow(t *testing.T) {
	job := testJob()
	out, err := RenderWorkflow(job, "0 0 * * *", "anthropic/claude")
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	content := string(out)

	// Well-formed YAML round-trip (multiline prompt with a colon must survive).
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, content)
	}

	for _, want := range []string{
		"cron: 0 0 * * *",
		"name: morning briefing",
		"model: openai/gpt-4o",
		"inference-gateway/infer-action@" + githubsetup.InferActionVersion,
		"actions/checkout@" + githubsetup.CheckoutActionVersion,
		"actions/create-github-app-token@" + githubsetup.AppTokenActionVersion,
		"${{ secrets.APP_CLIENT_ID }}",
		"${{ secrets.APP_PRIVATE_KEY }}",
		"github-token: ${{ steps.app-token.outputs.token }}",
		"github-app-slug: ${{ steps.app-token.outputs.app-slug }}",
		uploadArtifactAction,
		ArtifactNamePrefix + "${{ github.run_id }}",
		"workflow_dispatch",
		"# original cron: @daily",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("rendered workflow missing %q\n%s", want, content)
		}
	}

	// Prompt survives the YAML round-trip byte-for-byte.
	jobs := doc["jobs"].(map[string]any)
	steps := jobs["run"].(map[string]any)["steps"].([]any)
	var gotPrompt string
	for _, s := range steps {
		if with, ok := s.(map[string]any)["with"].(map[string]any); ok {
			if p, ok := with["direct-prompt"].(string); ok {
				gotPrompt = p
			}
		}
	}
	if gotPrompt != job.Prompt {
		t.Errorf("prompt did not round-trip: got %q want %q", gotPrompt, job.Prompt)
	}

	if strings.Contains(content, "Disable after first fire") {
		t.Errorf("recurring job must not render the self-disable step")
	}
}

func TestRenderWorkflowRunOnceAndDefaults(t *testing.T) {
	job := testJob()
	job.Name = ""
	job.Model = ""
	job.RunOnce = true
	out, err := RenderWorkflow(job, "*/10 * * * *", "")
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	content := string(out)

	if !strings.Contains(content, "gh workflow disable") {
		t.Errorf("run_once job missing self-disable step\n%s", content)
	}
	if !strings.Contains(content, "name: infer job 0b7f4a2c") {
		t.Errorf("missing fallback name\n%s", content)
	}
	if !strings.Contains(content, "model: "+githubsetup.DefaultWorkflowModel) {
		t.Errorf("missing fallback model\n%s", content)
	}
}

func TestWorkflowPath(t *testing.T) {
	if got := WorkflowPath("abc"); got != ".github/workflows/abc.yml" {
		t.Fatalf("WorkflowPath = %q", got)
	}
}
