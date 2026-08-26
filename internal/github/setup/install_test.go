package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentrunner "github.com/inference-gateway/cli/internal/agent/application/agentrunner"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestInstallPrompt(t *testing.T) {
	tests := []struct {
		name        string
		params      installPromptParams
		wantParts   []string
		absentParts []string
	}{
		{
			name: "fresh install",
			params: installPromptParams{
				Dir:       "/tmp/x",
				Repo:      "acme/app",
				Languages: "Rust, TypeScript",
				Context:   "the repo deploys with bun",
			},
			wantParts: []string{
				"/" + InstallSkill,
				"acme/app",
				"/tmp/x",
				"Create .github/workflows/tasks.yml",
				"Do not add any comments",
				"Rust, TypeScript",
				"v<major>.<minor>.<patch>",
				"the repo deploys with bun",
				`"title"`,
			},
			absentParts: []string{"take inspiration", "GitHub App token variant"},
		},
		{
			name: "update with ci inspiration and github app",
			params: installPromptParams{
				Dir:       "/tmp/x",
				Repo:      "acme/app",
				Existing:  []string{".github/workflows/tasks.yml"},
				CIPath:    ".github/workflows/ci.yml",
				GitHubApp: true,
			},
			wantParts: []string{
				"The workflow already exists: .github/workflows/tasks.yml",
				"NEVER remove or rewrite repo-specific content",
				"Read .github/workflows/ci.yml and take inspiration",
				"GitHub App token variant",
			},
			absentParts: []string{"Create .github/workflows/tasks.yml", "Additional context"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := installPrompt(tt.params)
			for _, want := range tt.wantParts {
				if !strings.Contains(got, want) {
					t.Errorf("prompt missing %q", want)
				}
			}
			for _, absent := range tt.absentParts {
				if strings.Contains(got, absent) {
					t.Errorf("prompt unexpectedly contains %q", absent)
				}
			}
		})
	}
}

func TestParseAgentPR(t *testing.T) {
	tests := []struct {
		name      string
		answer    string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "plain json",
			answer:    `{"title": "ci: bump infer-action to v0.49.2", "body": "Bumped."}`,
			wantTitle: "ci: bump infer-action to v0.49.2",
			wantBody:  "Bumped.",
		},
		{
			name:      "fenced json with prose",
			answer:    "Done!\n```json\n{\"title\": \"ci: add workflow\", \"body\": \"Added.\"}\n```",
			wantTitle: "ci: add workflow",
			wantBody:  "Added.",
		},
		{
			name:      "no json falls back",
			answer:    "I updated the workflow.",
			wantTitle: "ci: update opentask agent workflow",
			wantBody:  "I updated the workflow.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := parseAgentPR(tt.answer)
			if title != tt.wantTitle || body != tt.wantBody {
				t.Fatalf("parseAgentPR() = (%q, %q), want (%q, %q)", title, body, tt.wantTitle, tt.wantBody)
			}
		})
	}
}

// installScriptRunner scripts InstallWorkflow's git/gh calls and records them.
type installScriptRunner struct {
	calls        []string
	branchExists bool
}

func (r *installScriptRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case strings.Contains(call, "fetch origin "+InstallBranch):
		if r.branchExists {
			return nil, nil
		}
		return []byte("couldn't find remote ref"), errors.New("exit 128")
	case strings.Contains(call, "diff --cached --quiet"):
		return nil, errors.New("exit 1") // staged changes present
	case strings.Contains(call, "gh api repos/acme/app/languages"):
		return []byte("Go, Rust\n"), nil
	case strings.Contains(call, "gh pr create"):
		return []byte("https://github.com/acme/app/pull/7\n"), nil
	case strings.Contains(call, "gh pr view"):
		return []byte("https://github.com/acme/app/pull/3\n"), nil
	}
	return nil, nil
}

func (r *installScriptRunner) called(fragment string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, fragment) {
			return true
		}
	}
	return false
}

func TestInstallWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		branchExists bool
		wantURL      string
		wantCreate   bool
	}{
		{"fresh install opens a PR", false, "https://github.com/acme/app/pull/7", true},
		{"re-install continues the open PR", true, "https://github.com/acme/app/pull/3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &installScriptRunner{branchExists: tt.branchExists}
			service := NewService(runner)
			var gotPrompt string
			service.SetAgentRunner(func(_ context.Context, opts agentrunner.Options) (agentrunner.Result, error) {
				gotPrompt = opts.Prompt
				return agentrunner.Result{FinalAssistant: `{"title": "ci: add opentask workflow", "body": "Adds it."}`}, nil
			})

			url, err := service.InstallWorkflow(context.Background(), agentdomain.InstallWorkflowOptions{
				Repo:  "acme/app",
				Model: "openai/gpt-4o",
			})
			if err != nil {
				t.Fatalf("InstallWorkflow() error = %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if !strings.Contains(gotPrompt, "Go, Rust") {
				t.Error("prompt missing repo languages")
			}
			if !runner.called("commit -m ci: add opentask workflow") {
				t.Error("expected commit with the agent's title")
			}
			if !runner.called("push -u origin " + InstallBranch) {
				t.Error("expected push of the install branch")
			}
			if got := runner.called("gh pr create"); got != tt.wantCreate {
				t.Errorf("gh pr create called = %v, want %v", got, tt.wantCreate)
			}
		})
	}
}

func TestInstallWorkflowAgentNoChanges(t *testing.T) {
	runner := &funcRunner{fn: func(name string, args []string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if strings.Contains(call, "fetch origin") {
			return nil, errors.New("no remote ref")
		}
		return nil, nil // diff --cached --quiet exits 0: nothing staged
	}}
	service := NewService(runner)
	service.SetAgentRunner(func(_ context.Context, _ agentrunner.Options) (agentrunner.Result, error) {
		return agentrunner.Result{FinalAssistant: `{"title": "ci: noop", "body": ""}`}, nil
	})

	_, err := service.InstallWorkflow(context.Background(), agentdomain.InstallWorkflowOptions{Repo: "acme/app"})
	if err == nil || !strings.Contains(err.Error(), "no workflow changes") {
		t.Fatalf("expected no-changes error, got %v", err)
	}
}
