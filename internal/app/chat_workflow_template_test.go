package app

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func assertWorkflowCommon(t *testing.T, content string) {
	t.Helper()

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("generated workflow is not valid YAML: %v", err)
	}

	for _, want := range []string{
		"inference-gateway/infer-action@" + inferActionVersion,
		"actions/checkout@" + checkoutActionVersion,
		"timeout-minutes: 15",
		"model: " + workflowDefaultModel,
		"workflow_dispatch:",
		"browser-agent:",
		"debug:",
		"direct-prompt: ${{ inputs.prompt }}",
		"github.event.issue.number || github.event.pull_request.number || github.run_id",
		"pull_request_review_comment:",
		"moonshot-api-key:",
		"minimax-api-key:",
		"nvidia-api-key:",
		"zai-api-key:",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("workflow missing %q", want)
		}
	}

	for _, unwanted := range []string{
		"max-turns",
		"memory-repo",
		"plugins:",
		"skills:",
		"bash-allow-append",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("workflow should not contain %q", unwanted)
		}
	}
}

func TestGenerateStandardWorkflowContent(t *testing.T) {
	content := (&ChatApplication{}).generateStandardWorkflowContent()
	assertWorkflowCommon(t, content)

	if !strings.Contains(content, "github-token: ${{ secrets.GITHUB_TOKEN }}") {
		t.Error("standard workflow must use GITHUB_TOKEN")
	}
	if strings.Contains(content, "create-github-app-token") {
		t.Error("standard workflow must not mint an app token")
	}
}

func TestGenerateGithubActionWorkflowContent(t *testing.T) {
	content := (&ChatApplication{}).generateGithubActionWorkflowContent()
	assertWorkflowCommon(t, content)

	for _, want := range []string{
		"actions/create-github-app-token@" + appTokenActionVersion,
		"client-id: ${{ secrets.INFER_APP_ID }}",
		"private-key: ${{ secrets.INFER_APP_PRIVATE_KEY }}",
		"github-token: ${{ steps.app-token.outputs.token }}",
		"github-app-slug: ${{ steps.app-token.outputs.app-slug }}",
		"Get GitHub App User ID",
		"Set up Git",
		"commit.signoff true",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("org workflow missing %q", want)
		}
	}
}
