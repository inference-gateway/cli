package githubsetup

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fakeRunner implements CommandRunner with canned responses keyed by (name, args).
type fakeRunner struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	output []byte
	err    error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	r, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s %v", name, args)
	}
	return r.output, r.err
}

func TestGetCurrentRepo(t *testing.T) {
	fr := &fakeRunner{
		responses: map[string]fakeResponse{
			"gh repo view --json nameWithOwner -q .nameWithOwner": {
				output: []byte("my-org/my-repo\n"),
			},
		},
	}
	s := NewService(fr)
	repo, err := s.GetCurrentRepo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != "my-org/my-repo" {
		t.Fatalf("expected my-org/my-repo, got %q", repo)
	}
}

func TestGetCurrentRepo_Error(t *testing.T) {
	fr := &fakeRunner{
		responses: map[string]fakeResponse{
			"gh repo view --json nameWithOwner -q .nameWithOwner": {
				err: fmt.Errorf("not in a git repo"),
			},
		},
	}
	s := NewService(fr)
	_, err := s.GetCurrentRepo()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsOrgRepo(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		ghErr   error
		want    bool
		wantErr bool
	}{
		{
			name:    "org repo",
			repo:    "my-org/my-repo",
			want:    true,
			wantErr: false,
		},
		{
			name:    "user repo",
			repo:    "my-user/my-repo",
			ghErr:   fmt.Errorf("404"),
			want:    false,
			wantErr: false,
		},
		{
			name:    "invalid format",
			repo:    "no-slash",
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &fakeRunner{
				responses: map[string]fakeResponse{
					"gh api /orgs/" + strings.Split(tt.repo, "/")[0]: {
						err: tt.ghErr,
					},
				},
			}
			s := NewService(fr)
			got, err := s.IsOrgRepo(tt.repo)
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsOrgRepo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("IsOrgRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckOrgSecretsExist(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		ghErr   error
		want    bool
		wantErr bool
	}{
		{
			name:   "both secrets exist",
			output: "INFER_APP_ID\nINFER_APP_PRIVATE_KEY\nANOTHER_SECRET\n",
			want:   true,
		},
		{
			name:   "only app id",
			output: "INFER_APP_ID\n",
			want:   false,
		},
		{
			name:   "no secrets",
			output: "OTHER_SECRET\n",
			want:   false,
		},
		{
			name:    "gh error",
			output:  "",
			ghErr:   fmt.Errorf("not authorized"),
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &fakeRunner{
				responses: map[string]fakeResponse{
					"gh secret list --org test-org": {
						output: []byte(tt.output),
						err:    tt.ghErr,
					},
				},
			}
			s := NewService(fr)
			got, err := s.CheckOrgSecretsExist("test-org")
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckOrgSecretsExist() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("CheckOrgSecretsExist() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetOrgSecret(t *testing.T) {
	fr := &fakeRunner{
		responses: map[string]fakeResponse{
			"gh secret set INFER_APP_ID --org test-org --visibility all --body my-app-id": {},
		},
	}
	s := NewService(fr)
	if err := s.SetOrgSecret("test-org", "INFER_APP_ID", "my-app-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetOrgSecret_Error(t *testing.T) {
	fr := &fakeRunner{
		responses: map[string]fakeResponse{
			"gh secret set INFER_APP_ID --org test-org --visibility all --body my-app-id": {
				output: []byte("error: secret already exists\n"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	s := NewService(fr)
	err := s.SetOrgSecret("test-org", "INFER_APP_ID", "my-app-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWriteWorkflowFile(t *testing.T) {
	s := NewService(&RealRunner{})
	dir := t.TempDir()
	path := dir + "/.github/workflows/infer.yml"

	if err := s.WriteWorkflowFile(path, "content: test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateStandardWorkflowContent(t *testing.T) {
	s := NewService(&RealRunner{})
	content := s.GenerateStandardWorkflowContent()
	assertWorkflowCommon(t, content)

	if !strings.Contains(content, "github-token: ${{ secrets.GITHUB_TOKEN }}") {
		t.Error("standard workflow must use GITHUB_TOKEN")
	}
	if strings.Contains(content, "create-github-app-token") {
		t.Error("standard workflow must not mint an app token")
	}
}

func TestGenerateGithubActionWorkflowContent(t *testing.T) {
	s := NewService(&RealRunner{})
	content := s.GenerateGithubActionWorkflowContent()
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

func TestPreparePRCreation_NoGitRepo(t *testing.T) {
	fr := &fakeRunner{
		responses: map[string]fakeResponse{
			"git symbolic-ref refs/remotes/origin/HEAD": {
				err: fmt.Errorf("not a git repo"),
			},
		},
	}
	s := NewService(fr)
	_, err := s.PreparePRCreation("my-org/my-repo", "path")
	if err == nil {
		t.Fatal("expected error when not in a git repo, got nil")
	}
}
