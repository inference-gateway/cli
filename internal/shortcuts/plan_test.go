package shortcuts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// mockAgentService implements domain.AgentService for testing
type mockAgentService struct {
	runWithStreamResponse chan domain.ChatEvent
	runWithStreamError    error
}

func (m *mockAgentService) RunWithStream(ctx context.Context, req *domain.AgentRequest) (<-chan domain.ChatEvent, error) {
	if m.runWithStreamError != nil {
		return nil, m.runWithStreamError
	}
	return m.runWithStreamResponse, nil
}

func (m *mockAgentService) Run(ctx context.Context, req *domain.AgentRequest) (*domain.ChatSyncResponse, error) {
	return nil, nil
}

func (m *mockAgentService) CancelRequest(requestID string) error {
	return nil
}

func (m *mockAgentService) GetMetrics(requestID string) *domain.ChatMetrics {
	return nil
}

func TestPlanShortcut_GetName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "returns correct name",
			want: "plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			if got := p.GetName(); got != tt.want {
				t.Errorf("PlanShortcut.GetName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_GetDescription(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "returns correct description",
			want: "Process PRD file and create GitHub issues from requirements",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			if got := p.GetDescription(); got != tt.want {
				t.Errorf("PlanShortcut.GetDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_GetUsage(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "returns correct usage",
			want: "/plan <prd_file_path>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			if got := p.GetUsage(); got != tt.want {
				t.Errorf("PlanShortcut.GetUsage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_CanExecute(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "valid single argument",
			args: []string{"prd.md"},
			want: true,
		},
		{
			name: "no arguments",
			args: []string{},
			want: false,
		},
		{
			name: "multiple arguments",
			args: []string{"prd.md", "extra"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			if got := p.CanExecute(tt.args); got != tt.want {
				t.Errorf("PlanShortcut.CanExecute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_validatePRDFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plan_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	validFile := filepath.Join(tempDir, "valid.md")
	if err := os.WriteFile(validFile, []byte("# PRD Content"), 0644); err != nil {
		t.Fatal(err)
	}

	emptyFile := filepath.Join(tempDir, "empty.md")
	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	dirPath := filepath.Join(tempDir, "directory")
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		prdFilePath string
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid file",
			prdFilePath: validFile,
			wantErr:     false,
		},
		{
			name:        "empty path",
			prdFilePath: "",
			wantErr:     true,
			errContains: "PRD file path is required",
		},
		{
			name:        "nonexistent file",
			prdFilePath: filepath.Join(tempDir, "nonexistent.md"),
			wantErr:     true,
			errContains: "PRD file does not exist",
		},
		{
			name:        "directory path",
			prdFilePath: dirPath,
			wantErr:     true,
			errContains: "PRD path is a directory",
		},
		{
			name:        "empty file",
			prdFilePath: emptyFile,
			wantErr:     true,
			errContains: "PRD file is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			err := p.validatePRDFile(tt.prdFilePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("PlanShortcut.validatePRDFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("PlanShortcut.validatePRDFile() error = %v, should contain %v", err, tt.errContains)
			}
		})
	}
}

func TestPlanShortcut_readPRDFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plan_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	content := "# Test PRD\n\nThis is test content."
	validFile := filepath.Join(tempDir, "test.md")
	if err := os.WriteFile(validFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		prdFilePath string
		want        string
		wantErr     bool
	}{
		{
			name:        "valid file",
			prdFilePath: validFile,
			want:        content,
			wantErr:     false,
		},
		{
			name:        "nonexistent file",
			prdFilePath: filepath.Join(tempDir, "nonexistent.md"),
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			got, err := p.readPRDFile(tt.prdFilePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("PlanShortcut.readPRDFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("PlanShortcut.readPRDFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_parseIssuesFromResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     []PlannedIssue
		wantErr  bool
	}{
		{
			name: "valid JSON response",
			response: `[
				{
					"title": "Test Issue",
					"description": "Test description\n- [ ] Criterion 1",
					"labels": ["feature", "backend"],
					"priority": "high",
					"effort": "medium"
				}
			]`,
			want: []PlannedIssue{
				{
					Title:       "Test Issue",
					Description: "Test description\\n- [ ] Criterion 1",
					Labels:      []string{"feature", "backend"},
					Priority:    "high",
					Effort:      "medium",
				},
			},
			wantErr: false,
		},
		{
			name: "JSON with text before and after",
			response: `Here are the issues:
			[
				{
					"title": "Another Issue",
					"description": "Another description",
					"labels": ["bug"],
					"priority": "low",
					"effort": "small"
				}
			]
			These are the planned issues.`,
			want: []PlannedIssue{
				{
					Title:       "Another Issue",
					Description: "Another description",
					Labels:      []string{"bug"},
					Priority:    "low",
					Effort:      "small",
				},
			},
			wantErr: false,
		},
		{
			name:     "empty array",
			response: `[]`,
			want:     []PlannedIssue{},
			wantErr:  false,
		},
		{
			name:     "no JSON array",
			response: `No valid JSON found here`,
			wantErr:  true,
		},
		{
			name:     "invalid JSON",
			response: `[{invalid json}]`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			got, err := p.parseIssuesFromResponse(tt.response)
			if (err != nil) != tt.wantErr {
				t.Errorf("PlanShortcut.parseIssuesFromResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("PlanShortcut.parseIssuesFromResponse() length = %v, want %v", len(got), len(tt.want))
					return
				}
				for i, issue := range got {
					if issue.Title != tt.want[i].Title ||
						issue.Description != tt.want[i].Description ||
						issue.Priority != tt.want[i].Priority ||
						issue.Effort != tt.want[i].Effort {
						t.Errorf("PlanShortcut.parseIssuesFromResponse() issue %d = %v, want %v", i, issue, tt.want[i])
					}
					if len(issue.Labels) != len(tt.want[i].Labels) {
						t.Errorf("PlanShortcut.parseIssuesFromResponse() issue %d labels length = %v, want %v", i, len(issue.Labels), len(tt.want[i].Labels))
					}
					for j, label := range issue.Labels {
						if j < len(tt.want[i].Labels) && label != tt.want[i].Labels[j] {
							t.Errorf("PlanShortcut.parseIssuesFromResponse() issue %d label %d = %v, want %v", i, j, label, tt.want[i].Labels[j])
						}
					}
				}
			}
		})
	}
}

func TestPlanShortcut_unquoteString(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			name: "quoted string",
			s:    `"hello world"`,
			want: "hello world",
		},
		{
			name: "unquoted string",
			s:    "hello world",
			want: "hello world",
		},
		{
			name: "empty quoted string",
			s:    `""`,
			want: "",
		},
		{
			name: "string with spaces",
			s:    `  "hello"  `,
			want: "hello",
		},
		{
			name: "single character",
			s:    `"a"`,
			want: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			if got := p.unquoteString(tt.s); got != tt.want {
				t.Errorf("PlanShortcut.unquoteString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlanShortcut_parseStringArray(t *testing.T) {
	tests := []struct {
		name     string
		arrayStr string
		want     []string
	}{
		{
			name:     "valid array",
			arrayStr: `["item1", "item2", "item3"]`,
			want:     []string{"item1", "item2", "item3"},
		},
		{
			name:     "empty array",
			arrayStr: `[]`,
			want:     []string{},
		},
		{
			name:     "single item",
			arrayStr: `["single"]`,
			want:     []string{"single"},
		},
		{
			name:     "array with spaces",
			arrayStr: `[ "item1" , "item2" ]`,
			want:     []string{"item1", "item2"},
		},
		{
			name:     "not an array",
			arrayStr: `"not an array"`,
			want:     nil,
		},
		{
			name:     "malformed array",
			arrayStr: `["unclosed`,
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PlanShortcut{}
			got := p.parseStringArray(tt.arrayStr)
			if len(got) != len(tt.want) {
				t.Errorf("PlanShortcut.parseStringArray() length = %v, want %v", len(got), len(tt.want))
				return
			}
			for i, item := range got {
				if i < len(tt.want) && item != tt.want[i] {
					t.Errorf("PlanShortcut.parseStringArray() item %d = %v, want %v", i, item, tt.want[i])
				}
			}
		})
	}
}

func TestPlanShortcut_Execute(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "plan_execute_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	validFile := filepath.Join(tempDir, "valid.md")
	if err := os.WriteFile(validFile, []byte("# Test PRD\n\nFeature requirements"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		args            []string
		agentService    *mockAgentService
		wantErr         bool
		wantSuccess     bool
		wantOutputMatch string
	}{
		{
			name:            "no arguments",
			args:            []string{},
			agentService:    &mockAgentService{},
			wantErr:         false,
			wantSuccess:     false,
			wantOutputMatch: "Usage:",
		},
		{
			name:            "too many arguments",
			args:            []string{"file1", "file2"},
			agentService:    &mockAgentService{},
			wantErr:         false,
			wantSuccess:     false,
			wantOutputMatch: "Usage:",
		},
		{
			name:            "nonexistent file",
			args:            []string{filepath.Join(tempDir, "nonexistent.md")},
			agentService:    &mockAgentService{},
			wantErr:         false,
			wantSuccess:     false,
			wantOutputMatch: "PRD file validation failed",
		},
		{
			name: "successful execution with issues",
			args: []string{validFile},
			agentService: &mockAgentService{
				runWithStreamResponse: func() chan domain.ChatEvent {
					ch := make(chan domain.ChatEvent, 2)
					ch <- domain.ChatChunkEvent{Content: `[{"title":"Test Issue","description":"Test desc","labels":["feature"],"priority":"high","effort":"medium"}]`}
					ch <- domain.ChatCompleteEvent{}
					close(ch)
					return ch
				}(),
			},
			wantErr:         false,
			wantSuccess:     true,
			wantOutputMatch: "Successfully processed PRD and planned 1 GitHub issues",
		},
		{
			name: "agent returns no issues",
			args: []string{validFile},
			agentService: &mockAgentService{
				runWithStreamResponse: func() chan domain.ChatEvent {
					ch := make(chan domain.ChatEvent, 2)
					ch <- domain.ChatChunkEvent{Content: `[]`}
					ch <- domain.ChatCompleteEvent{}
					close(ch)
					return ch
				}(),
			},
			wantErr:         false,
			wantSuccess:     true,
			wantOutputMatch: "No issues were generated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Agent: config.AgentConfig{
					Model: "test-model",
				},
			}
			p := NewPlanShortcut(tt.agentService, cfg)

			got, err := p.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("PlanShortcut.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.Success != tt.wantSuccess {
				t.Errorf("PlanShortcut.Execute() success = %v, want %v", got.Success, tt.wantSuccess)
			}
			if tt.wantOutputMatch != "" && !strings.Contains(got.Output, tt.wantOutputMatch) {
				t.Errorf("PlanShortcut.Execute() output = %v, should contain %v", got.Output, tt.wantOutputMatch)
			}
		})
	}
}

func TestNewPlanShortcut(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "creates new plan shortcut",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &mockAgentService{}
			config := &config.Config{}
			got := NewPlanShortcut(agentService, config)

			if got == nil {
				t.Errorf("NewPlanShortcut() returned nil")
			}
			if got.agentService != agentService {
				t.Errorf("NewPlanShortcut() agent service not set correctly")
			}
			if got.config != config {
				t.Errorf("NewPlanShortcut() config not set correctly")
			}
		})
	}
}

func TestPlannedIssue_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name  string
		issue PlannedIssue
	}{
		{
			name: "complete issue",
			issue: PlannedIssue{
				Title:       "Test Issue",
				Description: "Test description with\nmultiple lines",
				Labels:      []string{"feature", "backend"},
				Priority:    "high",
				Effort:      "medium",
			},
		},
		{
			name: "minimal issue",
			issue: PlannedIssue{
				Title: "Minimal Issue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.issue)
			if err != nil {
				t.Errorf("PlannedIssue JSON marshal failed: %v", err)
			}

			var unmarshaled PlannedIssue
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Errorf("PlannedIssue JSON unmarshal failed: %v", err)
			}

			if unmarshaled.Title != tt.issue.Title {
				t.Errorf("PlannedIssue title mismatch: got %v, want %v", unmarshaled.Title, tt.issue.Title)
			}
		})
	}
}
