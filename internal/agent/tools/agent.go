package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	agentrunner "github.com/inference-gateway/cli/internal/services/agentrunner"
	sdk "github.com/inference-gateway/sdk"
)

// subagentDepthEnv guards against subagent fork-bombs: each spawned subagent
// inherits INFER_SUBAGENT_DEPTH=<parent+1>, and the tool disables itself once
// the depth reaches the configured max (a subagent is itself an `infer agent`).
const subagentDepthEnv = "INFER_SUBAGENT_DEPTH"

// subagentSystemPromptEnv carries a per-subagent system prompt to the spawned
// subagent (read in initConfig), so each subagent can run with its own role.
const subagentSystemPromptEnv = "INFER_SUBAGENT_SYSTEM_PROMPT"

// AgentTaskSpec is one delegated unit of work within an Agent tool call.
type AgentTaskSpec struct {
	Label        string
	Description  string
	Model        string
	Files        []string
	SystemPrompt string
	// Mode is the subagent's capability, resolved from the `type` argument:
	// ReadOnly -> AgentModeReadOnly (Explore-like, no approval), ReadWrite ->
	// AgentModeStandard (can mutate, approval applies).
	Mode domain.AgentMode
}

// AgentSubResult is the per-subagent outcome reported back to the LLM.
type AgentSubResult struct {
	Label     string `json:"label"`
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AgentToolResult is the structured payload of an Agent tool call.
type AgentToolResult struct {
	Mode       string           `json:"mode"`
	Wait       bool             `json:"wait"`
	Dispatched int              `json:"dispatched"`
	Subagents  []AgentSubResult `json:"subagents,omitempty"`
	Message    string           `json:"message,omitempty"`
}

// AgentTool spawns local subagents (each an `infer agent` subprocess) in
// parallel and folds their results back into the main context. It is the
// lightweight, no-A2A-server complement to the A2A tools.
type AgentTool struct {
	config    *config.Config
	tracker   domain.SubagentTracker
	submitter domain.JobSubmitter
	formatter domain.BaseFormatter
	binary    string

	// Injection points for tests; default to real implementations.
	runHeadless          func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error)
	interactiveAvailable func() bool
	launchPane           func(ctx context.Context, title, command string) (string, error)
	sendTask             func(ctx context.Context, paneID, task string) error
}

// NewAgentTool creates the Agent tool. tracker is the session's SubagentTracker
// (the data store the subagent control tools read); submitter is the job
// supervisor that monitors async/interactive subagents to completion.
func NewAgentTool(cfg *config.Config, tracker domain.SubagentTracker, submitter domain.JobSubmitter) *AgentTool {
	t := &AgentTool{
		config:    cfg,
		tracker:   tracker,
		submitter: submitter,
		formatter: domain.NewBaseFormatter("Agent"),
		binary:    os.Args[0],
	}
	t.runHeadless = agentrunner.Run
	t.interactiveAvailable = tmuxAvailable
	t.launchPane = t.launchTmuxPane
	t.sendTask = t.sendTaskToPane
	return t
}

// Definition returns the tool definition for the LLM.
func (t *AgentTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Agent.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Agent",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"tasks": map[string]any{
						"type":        "array",
						"description": "Subagent tasks to run in parallel. Each runs in its own isolated session.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"description":   map[string]any{"type": "string", "description": "The task for the subagent to perform"},
								"label":         map[string]any{"type": "string", "description": "Short label for the subagent (shown in progress/panes)"},
								"model":         map[string]any{"type": "string", "description": "Optional model override for this subagent"},
								"system_prompt": map[string]any{"type": "string", "description": "Optional system prompt giving THIS subagent a specialized role/persona for its task"},
								"type":          map[string]any{"type": "string", "enum": []string{"ReadOnly", "ReadWrite"}, "description": "Capability. ReadOnly (default) is Explore-like: read/search tools only, never needs approval - use for investigation/research. ReadWrite can modify files and run commands; its mutations require approval."},
							},
							"required": []string{"description"},
						},
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Shorthand for a single subagent task (alternative to tasks)",
					},
					"system_prompt": map[string]any{
						"type":        "string",
						"description": "Optional system prompt for the single-task (description) form, giving the subagent a specialized role",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"ReadOnly", "ReadWrite"},
						"description": "Capability for the single-task form. ReadOnly (default) is Explore-like: read/search only, never needs approval. ReadWrite can modify files and run commands; mutations require approval.",
					},
				},
			},
		},
	}
}

// Execute runs the tool with the given arguments.
//
//nolint:funlen,gocyclo,cyclop
func (t *AgentTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.IsEnabled() {
		return t.errorResult(args, start, "Agent tool is disabled in configuration"), nil
	}
	if currentSubagentDepth() >= t.maxDepth() {
		return t.errorResult(args, start, fmt.Sprintf("subagent depth limit reached (max_depth=%d); a subagent cannot spawn more subagents", t.maxDepth())), nil
	}

	specs, err := parseAgentTasks(args)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	mode := t.resolveMode()
	wait := t.resolveWait()

	var notes []string
	if maxN := t.maxParallel(); len(specs) > maxN {
		notes = append(notes, fmt.Sprintf("%d task(s) dropped (max_parallel=%d)", len(specs)-maxN, maxN))
		specs = specs[:maxN]
	}

	if mode == domain.SubagentModeInteractive && !t.interactiveAvailable() {
		switch t.config.Tools.Agent.Interactive.Fallback {
		case "error":
			return t.errorResult(args, start, "interactive mode requires running inside tmux (no $TMUX session detected)"), nil
		default:
			notes = append(notes, "not inside tmux - falling back to headless mode")
			mode = domain.SubagentModeHeadless
		}
	}

	parentSession := domain.GetSessionID(ctx)
	parentModel := domain.GetModel(ctx)
	// The subagent's capability comes from its per-task `type` (spec.Mode, resolved
	// in parseAgentTasks), NOT from the parent's mode - a ReadOnly subagent stays
	// read-only even when spawned from an AutoAccept chat.
	for i := range specs {
		specs[i].Model = t.resolveModel(specs[i].Model, parentModel)
	}

	if mode == domain.SubagentModeInteractive {
		return t.runInteractive(ctx, args, start, specs, parentSession, notes), nil
	}

	if wait {
		return t.runWait(ctx, args, start, specs, mode, parentSession, notes), nil
	}
	return t.runAsync(ctx, args, start, specs, mode, parentSession, notes), nil
}

// runWait spawns all subagents concurrently and blocks until they finish,
// returning one aggregated result (fan-out / fan-in).
func (t *AgentTool) runWait(ctx context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, mode, parentSession string, notes []string) *domain.ToolExecutionResult {
	results := make([]AgentSubResult, len(specs))
	states := make([]*domain.SubagentState, len(specs))
	var wg sync.WaitGroup
	for i, spec := range specs {
		state := &domain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        mode,
			SessionID:   newSubagentSessionID(parentSession),
			Status:      domain.SubagentRunning,
			StartedAt:   time.Now(),
			Silent:      true,
		}
		states[i] = state
		_ = t.tracker.AddSubagent(state)

		wg.Add(1)
		go func(i int, spec AgentTaskSpec, state *domain.SubagentState) {
			defer wg.Done()
			answer, err := t.executeOne(ctx, spec, state.SessionID)
			sub := toSubResult(spec, state.SessionID, answer, err)
			results[i] = sub
			status := domain.SubagentCompleted
			if !sub.Success {
				status = domain.SubagentFailed
			}
			_ = t.tracker.SetSubagentStatus(state.ID, status)
		}(i, spec, state)
	}
	wg.Wait()

	// This path is synchronous (the tool call blocks until all subagents finish),
	// so it is not supervised; clean up the tracker entries here instead.
	for _, state := range states {
		_ = t.tracker.RemoveSubagent(state.ID)
	}

	success := true
	for _, r := range results {
		if !r.Success {
			success = false
		}
	}
	return &domain.ToolExecutionResult{
		ToolName:  "Agent",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data: AgentToolResult{
			Mode:       mode,
			Wait:       true,
			Dispatched: len(specs),
			Subagents:  results,
			Message:    strings.Join(notes, "; "),
		},
	}
}

// runAsync dispatches subagents and returns immediately. Each subagent's
// outcome is delivered later by the supervisor monitoring its headlessSubagentJob.
func (t *AgentTool) runAsync(_ context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, mode, parentSession string, notes []string) *domain.ToolExecutionResult {
	dispatched := make([]AgentSubResult, 0, len(specs))
	for _, spec := range specs {
		sessionID := newSubagentSessionID(parentSession)

		runCtx, cancel := context.WithCancel(context.Background())
		state := &domain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        mode,
			SessionID:   sessionID,
			Status:      domain.SubagentRunning,
			StartedAt:   time.Now(),
			CancelFunc:  cancel,
		}
		if err := t.tracker.AddSubagent(state); err != nil {
			cancel()
			logger.Warn("failed to track subagent", "error", err)
			continue
		}

		if t.submitter != nil {
			t.submitter.Submit(&headlessSubagentJob{tool: t, spec: spec, state: state, runCtx: runCtx, cancelRun: cancel})
		} else {
			cancel()
		}

		dispatched = append(dispatched, AgentSubResult{Label: spec.Label, SessionID: sessionID, Success: true})
	}

	msg := fmt.Sprintf("Dispatched %d subagent(s) in %s mode. END YOUR TURN NOW - you will be notified automatically when each completes; do NOT poll with ListSubagents/GetSubagentResult.", len(dispatched), mode)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, "; ") + ")"
	}
	return &domain.ToolExecutionResult{
		ToolName:  "Agent",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: AgentToolResult{
			Mode:       mode,
			Wait:       false,
			Dispatched: len(dispatched),
			Subagents:  dispatched,
			Message:    msg,
		},
	}
}

// executeOne runs a single headless subagent and returns its final assistant
// message. Interactive subagents are handled separately by runInteractive.
func (t *AgentTool) executeOne(ctx context.Context, spec AgentTaskSpec, sessionID string) (string, error) {
	resultFile := subagentResultFilePath(sessionID)
	_ = os.Remove(resultFile)
	defer func() { _ = os.Remove(resultFile) }()

	res, err := t.runHeadless(ctx, agentrunner.Options{
		BinaryPath: t.binary,
		SessionID:  sessionID,
		Prompt:     spec.Description,
		Model:      spec.Model,
		Files:      spec.Files,
		ResultFile: resultFile,
		ExtraEnv:   subagentExtraEnv(spec),
	})

	answer := res.FinalAssistant
	if rf, ok := readSubagentResultFile(resultFile); ok {
		if rf.FinalAssistant != "" {
			answer = rf.FinalAssistant
		}
		if err == nil && !rf.Success && rf.Error != "" {
			err = fmt.Errorf("%s", rf.Error)
		}
	}
	return answer, err
}

// runInteractive launches each subagent in its own live `infer chat` tmux pane,
// types in the task, and tracks it as a running interactive subagent. There is
// no completion signal (the pane is a user-driven REPL), so this is
// fire-and-track: it returns once the panes are launched. The main agent then
// uses ListSubagents / GetSubagentResult / CloseSubagent to inspect and close
// them.
func (t *AgentTool) runInteractive(ctx context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, parentSession string, notes []string) *domain.ToolExecutionResult {
	launched := make([]AgentSubResult, 0, len(specs))
	for _, spec := range specs {
		sessionID := newSubagentSessionID(parentSession)
		title := spec.Label
		if title == "" {
			title = "subagent"
		}
		_ = os.Remove(subagentResultFilePath(sessionID))
		_ = os.Remove(subagentApprovalFilePath(sessionID))
		paneID, err := t.launchPane(ctx, title, t.buildChatPaneCommand(spec, sessionID))
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: failed to open tmux pane: %v", labelOrSession(spec.Label, sessionID), err))
			continue
		}
		if err := t.sendTask(ctx, paneID, spec.Description); err != nil {
			logger.Warn("failed to send task to interactive subagent pane", "pane", paneID, "error", err)
		}

		state := &domain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        domain.SubagentModeInteractive,
			SessionID:   sessionID,
			PaneID:      paneID,
			Status:      domain.SubagentRunning,
			StartedAt:   time.Now(),
		}
		if err := t.tracker.AddSubagent(state); err != nil {
			logger.Warn("failed to track interactive subagent", "error", err)
			continue
		}

		if t.submitter != nil {
			t.submitter.Submit(newInteractiveSubagentJob(t, state))
		}

		launched = append(launched, AgentSubResult{
			Label:     spec.Label,
			SessionID: sessionID,
			Success:   true,
			Result:    fmt.Sprintf("running in tmux pane %s", paneID),
		})
	}

	msg := fmt.Sprintf("Launched %d interactive subagent(s) in tmux panes - live chats you can watch. You will be AUTOMATICALLY NOTIFIED when each finishes (its final output is folded back into this conversation). END YOUR TURN NOW and wait: do NOT call ListSubagents/GetSubagentResult to poll, and do NOT CloseSubagent to fetch a result (closing only stops one early). The '[Subagent Completed: ...]' message arrives on its own - act on it then.", len(launched))
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, "; ") + ")"
	}
	return &domain.ToolExecutionResult{
		ToolName:  "Agent",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: AgentToolResult{
			Mode:       domain.SubagentModeInteractive,
			Wait:       false,
			Dispatched: len(launched),
			Subagents:  launched,
			Message:    msg,
		},
	}
}

// labelOrSession returns label, or a short session id when the label is blank,
// for use in user-facing notes.
func labelOrSession(label, sessionID string) string {
	if label != "" {
		return label
	}
	return shortSession(sessionID)
}

// buildChatPaneCommand assembles the shell command run inside the tmux pane: a
// live `infer chat` using the parent model (via INFER_AGENT_MODEL so no model
// dropdown appears) plus the depth guard.
func (t *AgentTool) buildChatPaneCommand(spec AgentTaskSpec, sessionID string) string {
	parts := []string{fmt.Sprintf("%s=%d", subagentDepthEnv, currentSubagentDepth()+1)}
	if spec.SystemPrompt != "" {
		parts = append(parts, subagentSystemPromptEnv+"="+shellQuote(spec.SystemPrompt))
	}
	if spec.Mode != domain.AgentModeStandard {
		parts = append(parts, domain.EnvSubagentAgentMode+"="+shellQuote(spec.Mode.AllowedlistKey()))
	}
	if spec.Model != "" {
		parts = append(parts, "INFER_AGENT_MODEL="+shellQuote(spec.Model))
	}

	historyName := sanitizeSlug(spec.Label)
	if historyName == "" {
		historyName = domain.SubagentHistoryMemoryOnly
	}
	parts = append(parts, domain.EnvSubagentHistoryName+"="+shellQuote(historyName))

	parts = append(parts, domain.EnvSubagentResultFile+"="+shellQuote(subagentResultFilePath(sessionID)))
	parts = append(parts, domain.EnvSubagentApprovalFile+"="+shellQuote(subagentApprovalFilePath(sessionID)))
	parts = append(parts, shellQuote(t.binary), "chat")
	return strings.Join(parts, " ")
}

// subagentExtraEnv builds the environment passed to a headless subagent: the
// depth guard plus an optional per-subagent system prompt.
func subagentExtraEnv(spec AgentTaskSpec) []string {
	env := []string{fmt.Sprintf("%s=%d", subagentDepthEnv, currentSubagentDepth()+1)}
	if spec.SystemPrompt != "" {
		env = append(env, subagentSystemPromptEnv+"="+spec.SystemPrompt)
	}
	if spec.Mode != domain.AgentModeStandard {
		env = append(env, domain.EnvSubagentAgentMode+"="+spec.Mode.AllowedlistKey())
	}
	return env
}

// sendTaskToPane waits for the chat TUI to be ready, then types the task into
// the pane and presses Enter (via tmux send-keys).
func (t *AgentTool) sendTaskToPane(ctx context.Context, paneID, task string) error {
	if paneID == "" {
		return nil
	}
	t.waitForPaneReady(ctx, paneID)
	return tmuxSendKeys(ctx, paneID, task, []string{"Enter"})
}

// waitForPaneReady polls the pane content until the chat input prompt appears
// (or a timeout), so the task isn't typed before the TUI is listening.
func (t *AgentTool) waitForPaneReady(ctx context.Context, paneID string) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", paneID).Output()
		if err == nil && strings.Contains(string(out), "Type your message") {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// launchTmuxPane opens a new tmux pane/window running command. It returns once
// the pane is created; the pane keeps running the subagent and, via
// remain-on-exit, stays open after it finishes so its output remains readable.
func (t *AgentTool) launchTmuxPane(ctx context.Context, title, command string) (string, error) {
	args := []string{"split-window", "-v"}
	switch strings.TrimSpace(t.config.Tools.Agent.Interactive.Layout) {
	case "horizontal":
		args = []string{"split-window", "-h"}
	case "window":
		args = []string{"new-window"}
	}

	args = append(args, "-P", "-F", "#{pane_id}", command)
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		return "", nil
	}

	_ = exec.CommandContext(ctx, "tmux", "set-option", "-p", "-t", paneID, "remain-on-exit", "on").Run()
	_ = exec.CommandContext(ctx, "tmux", "select-pane", "-t", paneID, "-T", title).Run()
	return paneID, nil
}

// subagentResultFilePath is the temp path a subagent writes its outcome to.
func subagentResultFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), "infer-subagent-"+sessionID+".json")
}

// subagentApprovalFilePath is the temp path an interactive subagent writes while
// it is blocked on a tool-approval prompt (see EnvSubagentApprovalFile).
func subagentApprovalFilePath(sessionID string) string {
	return filepath.Join(os.TempDir(), "infer-subagent-"+sessionID+".approval.json")
}

// readSubagentApproval reads a subagent's approval sidecar. It returns
// (summary, true) when the subagent is currently blocked on a tool-approval
// prompt, or ("", false) when it is not (file absent, malformed, or not awaiting).
func readSubagentApproval(sessionID string) (string, bool) {
	data, err := os.ReadFile(subagentApprovalFilePath(sessionID))
	if err != nil {
		return "", false
	}
	var af domain.SubagentApprovalFile
	if err := json.Unmarshal(data, &af); err != nil || !af.Awaiting {
		return "", false
	}
	return strings.TrimSpace(af.Summary), true
}

// readSubagentResultFile reads and parses a subagent result file without
// waiting. Returns ok=false when the file is absent or malformed.
func readSubagentResultFile(path string) (domain.SubagentResultFile, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.SubagentResultFile{}, false
	}
	var rf domain.SubagentResultFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return domain.SubagentResultFile{}, false
	}
	return rf, true
}

// readSubagentResultMessage returns the subagent chat's real last assistant
// message from its result file (trimmed), or "" if the file is absent or empty.
// It is the single harvest path: the subagent's pane is never scraped for content
// (its TUI chrome is noise), so on a miss callers deliver nothing. Shared by the
// poller's inspector and the GetSubagentResult / CloseSubagent tools.
func readSubagentResultMessage(sessionID string) string {
	if rf, ok := readSubagentResultFile(subagentResultFilePath(sessionID)); ok {
		return strings.TrimSpace(rf.FinalAssistant)
	}
	return ""
}

// Validate checks the tool arguments.
func (t *AgentTool) Validate(args map[string]any) error {
	_, err := parseAgentTasks(args)
	return err
}

// IsEnabled reports whether the tool is enabled (and below the depth limit).
func (t *AgentTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled() && currentSubagentDepth() < t.maxDepth()
}

func (t *AgentTool) maxDepth() int {
	if d := t.config.Tools.Agent.MaxDepth; d > 0 {
		return d
	}
	return 1
}

func (t *AgentTool) maxParallel() int {
	if n := t.config.Tools.Agent.MaxParallel; n > 0 {
		return n
	}
	return 1
}

// resolveMode returns the subagent surface from config (tools.agent.mode).
// Mode is an environment decision (interactive needs a tmux session), not a
// per-task LLM choice, so it is config-driven and intentionally NOT an LLM
// parameter - otherwise the model overrides the operator's config.
func (t *AgentTool) resolveMode() string {
	mode := strings.TrimSpace(t.config.Tools.Agent.Mode)
	if mode == domain.SubagentModeInteractive {
		return domain.SubagentModeInteractive
	}
	return domain.SubagentModeHeadless
}

// resolveWait returns whether the tool blocks for aggregated results, from
// config (tools.agent.wait) only. Like mode, this is an operator policy - not a
// per-call LLM choice - so there is no wait parameter the model can override.
func (t *AgentTool) resolveWait() bool {
	return t.config.Tools.Agent.Wait
}

// resolveModel picks the subagent model: explicit per-task override, else the
// configured tools.agent.model, else the parent turn's model (so subagents
// inherit the model the user is already using). An empty result would make the
// subagent process fail with "no model specified".
func (t *AgentTool) resolveModel(taskModel, parentModel string) string {
	if taskModel != "" {
		return taskModel
	}
	if t.config.Tools.Agent.Model != "" {
		return t.config.Tools.Agent.Model
	}
	return parentModel
}

func (t *AgentTool) errorResult(args map[string]any, start time.Time, msg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "Agent",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     msg,
		Data:      AgentToolResult{Message: msg},
	}
}

// FormatResult formats tool execution results for different contexts.
func (t *AgentTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a compact one-line preview.
func (t *AgentTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	data, ok := result.Data.(AgentToolResult)
	if !ok {
		if result.Success {
			return "Agent completed"
		}
		return "Agent failed"
	}
	if data.Wait {
		return fmt.Sprintf("Ran %d subagent(s) in %s mode", data.Dispatched, data.Mode)
	}
	return fmt.Sprintf("Dispatched %d subagent(s) in %s mode", data.Dispatched, data.Mode)
}

// FormatForUI renders the tool call header with a single-line preview.
func (t *AgentTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", toolCall)
	fmt.Fprintf(&out, "└─ %s %s", statusIcon, t.FormatPreview(result))
	return out.String()
}

// FormatForLLM renders a structured detail block for the assistant.
func (t *AgentTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	var out strings.Builder
	out.WriteString(t.formatter.FormatExpandedHeader(result))
	if result.Data != nil {
		out.WriteString(t.formatter.FormatDataSection(t.formatAgentData(result.Data), len(result.Metadata) > 0))
	}
	out.WriteString(t.formatter.FormatExpandedFooter(result, result.Data != nil))
	return out.String()
}

func (t *AgentTool) formatAgentData(data any) string {
	d, ok := data.(AgentToolResult)
	if !ok {
		// Async single-subagent completions carry an AgentSubResult.
		if sub, okSub := data.(AgentSubResult); okSub {
			return formatSubResult(sub)
		}
		return t.formatter.FormatAsJSON(data)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Mode: %s | Wait: %t | Dispatched: %d\n", d.Mode, d.Wait, d.Dispatched)
	if d.Message != "" {
		fmt.Fprintf(&out, "%s\n", d.Message)
	}
	for _, sub := range d.Subagents {
		out.WriteString("\n")
		out.WriteString(formatSubResult(sub))
	}
	return out.String()
}

func formatSubResult(sub AgentSubResult) string {
	var out strings.Builder
	label := sub.Label
	if label == "" {
		label = sub.SessionID
	}
	status := "ok"
	if !sub.Success {
		status = "failed"
	}
	fmt.Fprintf(&out, "[%s] (%s)\n", label, status)
	if sub.Error != "" {
		fmt.Fprintf(&out, "Error: %s\n", sub.Error)
	}
	if sub.Result != "" {
		fmt.Fprintf(&out, "%s\n", sub.Result)
	}
	return out.String()
}

// ShouldCollapseArg keeps the Agent tool's arguments visible rather than
// collapsing them to "...". The task description is the one thing a user needs
// to see when approving an Agent call - and unlike Write/Edit there is no diff
// or preview below the summary to fall back on - so it must show in the approval
// prompt and the tool-call line. Long values are truncated by the surrounding
// view's width budget.
func (t *AgentTool) ShouldCollapseArg(string) bool {
	return false
}

// ShouldAlwaysExpand keeps the result block collapsed by default.
func (t *AgentTool) ShouldAlwaysExpand() bool {
	return false
}

// --- helpers ---

func parseAgentTasks(args map[string]any) ([]AgentTaskSpec, error) {
	if raw, ok := args["tasks"].([]any); ok && len(raw) > 0 {
		specs := make([]AgentTaskSpec, 0, len(raw))
		for i, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("tasks[%d] must be an object", i)
			}
			desc, _ := m["description"].(string)
			if strings.TrimSpace(desc) == "" {
				return nil, fmt.Errorf("tasks[%d].description is required and must be a non-empty string", i)
			}
			specs = append(specs, AgentTaskSpec{
				Label:        optionalString(m, "label"),
				Description:  desc,
				Model:        optionalString(m, "model"),
				Files:        optionalStringSlice(m, "files"),
				SystemPrompt: optionalString(m, "system_prompt"),
				Mode:         resolveSubagentType(optionalString(m, "type")),
			})
		}
		return specs, nil
	}

	if desc, ok := args["description"].(string); ok && strings.TrimSpace(desc) != "" {
		return []AgentTaskSpec{{
			Label:        optionalString(args, "label"),
			Description:  desc,
			Model:        optionalString(args, "model"),
			Files:        optionalStringSlice(args, "files"),
			SystemPrompt: optionalString(args, "system_prompt"),
			Mode:         resolveSubagentType(optionalString(args, "type")),
		}}, nil
	}

	return nil, fmt.Errorf("provide either 'tasks' (a non-empty array) or 'description' (a single task)")
}

// resolveSubagentType maps the Agent tool's `type` argument to a subagent
// capability mode: "ReadWrite" -> AgentModeStandard (can mutate, approval
// applies); anything else, including the default empty value, -> AgentModeReadOnly
// (Explore-like read/search, no approval). Matching is case-insensitive.
func resolveSubagentType(t string) domain.AgentMode {
	if strings.EqualFold(strings.TrimSpace(t), "ReadWrite") {
		return domain.AgentModeStandard
	}
	return domain.AgentModeReadOnly
}

func optionalStringSlice(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toSubResult(spec AgentTaskSpec, sessionID, answer string, err error) AgentSubResult {
	sub := AgentSubResult{
		Label:     spec.Label,
		SessionID: sessionID,
		Success:   err == nil,
		Result:    answer,
	}
	if sub.Label == "" {
		sub.Label = shortSession(sessionID)
	}
	if err != nil {
		sub.Error = err.Error()
	}
	return sub
}

func newSubagentSessionID(parentSession string) string {
	id := uuid.New().String()
	if parentSession == "" {
		return "subagent-" + id
	}
	return "subagent-" + parentSession + "-" + id
}

func shortSession(id string) string {
	if len(id) > 16 {
		return id[:16]
	}
	return id
}

// currentSubagentDepth reads the recursion depth from the environment (0 at the
// top level).
func currentSubagentDepth() int {
	if v := os.Getenv(subagentDepthEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// tmuxAvailable reports whether we are inside a tmux session and the binary is
// on PATH.
func tmuxAvailable() bool {
	if os.Getenv("TMUX") == "" {
		return false
	}
	_, err := exec.LookPath("tmux")
	return err == nil
}

// shellQuote single-quotes a string for safe inclusion in a tmux pane command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
