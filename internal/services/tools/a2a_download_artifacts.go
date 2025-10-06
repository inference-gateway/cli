package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

type A2ADownloadArtifactsTool struct {
	config      *config.Config
	formatter   domain.CustomFormatter
	taskTracker domain.TaskTracker
	client      client.A2AClient
}

type A2ADownloadArtifactsResult struct {
	AgentName       string          `json:"agent_name"`
	ContextID       string          `json:"context_id"`
	TaskID          string          `json:"task_id"`
	Artifacts       []adk.Artifact  `json:"artifacts"`
	DownloadResults []DownloadInfo  `json:"download_results"`
	Success         bool            `json:"success"`
	Message         string          `json:"message"`
	Duration        time.Duration   `json:"duration"`
}

type DownloadInfo struct {
	ArtifactID string `json:"artifact_id"`
	LocalPath  string `json:"local_path"`
	Size       int64  `json:"size"`
	Downloaded bool   `json:"downloaded"`
	Error      string `json:"error,omitempty"`
}

func NewA2ADownloadArtifactsTool(cfg *config.Config, taskTracker domain.TaskTracker) *A2ADownloadArtifactsTool {
	return &A2ADownloadArtifactsTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      nil,
		formatter: domain.NewCustomFormatter("A2A_DownloadArtifacts", func(key string) bool {
			return key == "content"
		}),
	}
}

// NewA2ADownloadArtifactsToolWithClient creates a new A2A download artifacts tool with an injected client (for testing)
func NewA2ADownloadArtifactsToolWithClient(cfg *config.Config, taskTracker domain.TaskTracker, client client.A2AClient) *A2ADownloadArtifactsTool {
	return &A2ADownloadArtifactsTool{
		config:      cfg,
		taskTracker: taskTracker,
		client:      client,
		formatter: domain.NewCustomFormatter("A2A_DownloadArtifacts", func(key string) bool {
			return key == "content"
		}),
	}
}

func (t *A2ADownloadArtifactsTool) Definition() sdk.ChatCompletionTool {
	description := "Download artifacts from a completed A2A task. The agent must first fetch the task to verify it's completed before downloading artifacts. Only works when the task status is 'completed'."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "A2A_DownloadArtifacts",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"agent_url": map[string]any{
						"type":        "string",
						"description": "URL of the A2A agent server",
					},
					"context_id": map[string]any{
						"type":        "string",
						"description": "Context ID for the task",
					},
					"task_id": map[string]any{
						"type":        "string",
						"description": "ID of the completed task to download artifacts from",
					},
				},
				"required": []string{"agent_url", "context_id", "task_id"},
			},
		},
	}
}

func (t *A2ADownloadArtifactsTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &domain.ToolExecutionResult{
			ToolName:  "A2A_DownloadArtifacts",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: A2ADownloadArtifactsResult{
				Success: false,
				Message: "A2A connections are disabled",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	contextID, ok := args["context_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "context_id parameter is required and must be a string")
	}

	taskID, ok := args["task_id"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_id parameter is required and must be a string")
	}

	var adkClient client.A2AClient
	if t.client != nil {
		adkClient = t.client
	} else {
		adkClient = client.NewClient(agentURL)
	}

	queryParams := adk.TaskQueryParams{ID: taskID}
	taskResponse, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		logger.Error("Failed to query task before artifact download", "agent_url", agentURL, "task_id", taskID, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to query task: %v", err))
	}

	taskBytes, err := json.Marshal(taskResponse.Result)
	if err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to marshal task result: %v", err))
	}

	var task adk.Task
	if err := json.Unmarshal(taskBytes, &task); err != nil {
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to unmarshal task: %v", err))
	}

	if task.Status.State != adk.TaskStateCompleted {
		return t.errorResult(args, startTime, fmt.Sprintf("Task %s is not completed (current state: %s). Artifacts can only be downloaded from completed tasks", taskID, task.Status.State))
	}

	downloadResults, err := t.downloadTaskArtifacts(ctx, &task)
	if err != nil {
		logger.Error("Failed to download artifacts", "agent_url", agentURL, "task_id", taskID, "error", err)
		return t.errorResult(args, startTime, fmt.Sprintf("Failed to download artifacts: %v", err))
	}

	result := A2ADownloadArtifactsResult{
		AgentName:       agentURL,
		ContextID:       contextID,
		TaskID:          taskID,
		Artifacts:       task.Artifacts,
		DownloadResults: downloadResults,
		Success:         true,
		Duration:        time.Since(startTime),
		Message:         fmt.Sprintf("Downloaded %d artifact(s) from completed task %s", len(downloadResults), taskID),
	}

	return &domain.ToolExecutionResult{
		ToolName:  "A2A_DownloadArtifacts",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data:      result,
	}, nil
}

func (t *A2ADownloadArtifactsTool) downloadTaskArtifacts(ctx context.Context, task *adk.Task) ([]DownloadInfo, error) {
	downloadDir := "/tmp/downloads"
	if t.config.A2A.Tools.DownloadArtifacts.DownloadDir != "" {
		downloadDir = t.config.A2A.Tools.DownloadArtifacts.DownloadDir
	}

	timeoutSeconds := 30
	if t.config.A2A.Tools.DownloadArtifacts.TimeoutSeconds > 0 {
		timeoutSeconds = t.config.A2A.Tools.DownloadArtifacts.TimeoutSeconds
	}

	helper := client.NewArtifactHelper()
	config := &client.DownloadConfig{
		OutputDir:            downloadDir,
		HTTPClient:           &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		OverwriteExisting:    true,
		OrganizeByArtifactID: true,
	}

	results, err := helper.DownloadAllArtifacts(ctx, task, config)
	if err != nil {
		return nil, fmt.Errorf("failed to download artifacts: %w", err)
	}

	downloadInfos := make([]DownloadInfo, 0, len(results))
	artifactIndex := 0

	for _, result := range results {
		if artifactIndex >= len(task.Artifacts) {
			break
		}

		currentArtifact := task.Artifacts[artifactIndex]

		info := DownloadInfo{
			ArtifactID: currentArtifact.ArtifactID,
			LocalPath:  result.FilePath,
			Size:       result.BytesWritten,
			Downloaded: result.Error == nil,
		}

		if result.Error != nil {
			info.Error = result.Error.Error()
			logger.Error("Failed to download artifact", "artifact_id", info.ArtifactID, "path", result.FilePath, "error", result.Error)
		} else {
			logger.Info("Successfully downloaded artifact", "artifact_id", info.ArtifactID, "path", result.FilePath, "size", result.BytesWritten)
		}

		downloadInfos = append(downloadInfos, info)
		artifactIndex++
	}

	return downloadInfos, nil
}

func (t *A2ADownloadArtifactsTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "A2A_DownloadArtifacts",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: A2ADownloadArtifactsResult{
			Success: false,
			Message: errorMsg,
		},
	}, nil
}

func (t *A2ADownloadArtifactsTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	if _, ok := args["context_id"].(string); !ok {
		return fmt.Errorf("context_id parameter is required and must be a string")
	}
	if _, ok := args["task_id"].(string); !ok {
		return fmt.Errorf("task_id parameter is required and must be a string")
	}
	return nil
}

func (t *A2ADownloadArtifactsTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

func (t *A2ADownloadArtifactsTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatter.FormatAsJSON(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

func (t *A2ADownloadArtifactsTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

func (t *A2ADownloadArtifactsTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Data == nil {
		return result.Error
	}

	if data, ok := result.Data.(A2ADownloadArtifactsResult); ok {
		return fmt.Sprintf("A2A Download Artifacts: %s", data.Message)
	}

	return "A2A download artifacts operation completed"
}

func (t *A2ADownloadArtifactsTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

func (t *A2ADownloadArtifactsTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *A2ADownloadArtifactsTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.DownloadArtifacts.Enabled
}
