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
	formatter domain.BaseFormatter
	binary    string

	// Injection points for tests; default to real implementations.
	runHeadless          func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error)
	interactiveAvailable func() bool
	launchPane           func(ctx context.Context, title, command string) (string, error)
}

// NewAgentTool creates the Agent tool. tracker must be the session's
// SubagentTracker (the same instance the SubagentPoller watches).
func NewAgentTool(cfg *config.Config, tracker domain.SubagentTracker) *AgentTool {
	t := &AgentTool{
		config:    cfg,
		tracker:   tracker,
		formatter: domain.NewBaseFormatter("Agent"),
		binary:    os.Args[0],
	}
	t.runHeadless = agentrunner.Run
	t.interactiveAvailable = tmuxAvailable
	t.launchPane = t.launchTmuxPane
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
	for i := range specs {
		specs[i].Model = t.resolveModel(specs[i].Model, parentModel)
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
			ResultChan:  make(chan *domain.ToolExecutionResult, 1),
			ErrorChan:   make(chan error, 1),
		}
		_ = t.tracker.AddSubagent(state)

		wg.Add(1)
		go func(i int, spec AgentTaskSpec, state *domain.SubagentState) {
			defer wg.Done()
			answer, err := t.executeOne(ctx, spec, state.SessionID, mode)
			sub := toSubResult(spec, state.SessionID, answer, err)
			results[i] = sub
			state.Status = domain.SubagentCompleted
			if !sub.Success {
				state.Status = domain.SubagentFailed
			}

			select {
			case state.ResultChan <- &domain.ToolExecutionResult{
				ToolName: "Agent",
				Success:  sub.Success,
				Error:    sub.Error,
				Duration: time.Since(state.StartedAt),
				Data:     sub,
			}:
			default:
			}
		}(i, spec, state)
	}
	wg.Wait()

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
// outcome is delivered later by the SubagentPoller via its ResultChan.
func (t *AgentTool) runAsync(ctx context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, mode, parentSession string, notes []string) *domain.ToolExecutionResult {
	dispatched := make([]AgentSubResult, 0, len(specs))
	for _, spec := range specs {
		sessionID := newSubagentSessionID(parentSession)
		state := &domain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        mode,
			SessionID:   sessionID,
			Status:      domain.SubagentRunning,
			StartedAt:   time.Now(),
			ResultChan:  make(chan *domain.ToolExecutionResult, 1),
			ErrorChan:   make(chan error, 1),
		}
		if err := t.tracker.AddSubagent(state); err != nil {
			logger.Warn("failed to track subagent", "error", err)
			continue
		}

		// Detach from the tool-call context so the subagent outlives this turn.
		runCtx, cancel := context.WithCancel(context.Background())
		state.CancelFunc = cancel
		go func(spec AgentTaskSpec, state *domain.SubagentState) {
			defer cancel()
			answer, err := t.executeOne(runCtx, spec, state.SessionID, state.Mode)
			sub := toSubResult(spec, state.SessionID, answer, err)
			state.Status = domain.SubagentCompleted
			if !sub.Success {
				state.Status = domain.SubagentFailed
			}
			state.ResultChan <- &domain.ToolExecutionResult{
				ToolName:  "Agent",
				Arguments: map[string]any{"label": sub.Label, "session_id": state.SessionID},
				Success:   sub.Success,
				Error:     sub.Error,
				Duration:  time.Since(state.StartedAt),
				Data:      sub,
			}
		}(spec, state)

		dispatched = append(dispatched, AgentSubResult{Label: spec.Label, SessionID: sessionID, Success: true})
	}

	msg := fmt.Sprintf("Dispatched %d subagent(s) in %s mode. You will be notified automatically when each completes - do not wait or poll.", len(dispatched), mode)
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

// executeOne runs a single subagent and returns its final assistant message.
func (t *AgentTool) executeOne(ctx context.Context, spec AgentTaskSpec, sessionID, mode string) (string, error) {
	if mode == domain.SubagentModeInteractive {
		return t.executeInteractive(ctx, spec, sessionID)
	}
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
	// Prefer the result file's harvested answer (it skips the subagent's trailing
	// "task complete" verification turn); fall back to the streamed final message.
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

// executeInteractive launches a live `infer chat` subagent in a tmux pane and
// types the task into it via stdin (tmux send-keys). It is fire-and-watch:
// `infer chat` is interactive and gives no completion signal, so there is no
// automatic result harvest - the user watches and drives the side-chat.
func (t *AgentTool) executeInteractive(ctx context.Context, spec AgentTaskSpec, _ string) (string, error) {
	title := spec.Label
	if title == "" {
		title = "subagent"
	}
	paneID, err := t.launchPane(ctx, title, t.buildChatPaneCommand(spec))
	if err != nil {
		return "", fmt.Errorf("failed to open tmux pane: %w", err)
	}
	if err := t.sendTaskToPane(ctx, paneID, spec.Description); err != nil {
		logger.Warn("failed to send task to interactive subagent pane", "pane", paneID, "error", err)
	}
	return fmt.Sprintf("Launched an interactive chat subagent in tmux pane %s and typed in the task: %q. Watch and interact with it in the pane.", paneID, spec.Description), nil
}

// buildChatPaneCommand assembles the shell command run inside the tmux pane: a
// live `infer chat` using the parent model (via INFER_AGENT_MODEL so no model
// dropdown appears) plus the depth guard.
func (t *AgentTool) buildChatPaneCommand(spec AgentTaskSpec) string {
	parts := []string{fmt.Sprintf("%s=%d", subagentDepthEnv, currentSubagentDepth()+1)}
	if spec.SystemPrompt != "" {
		parts = append(parts, subagentSystemPromptEnv+"="+shellQuote(spec.SystemPrompt))
	}
	if spec.Model != "" {
		parts = append(parts, "INFER_AGENT_MODEL="+shellQuote(spec.Model))
	}
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
	return env
}

// sendTaskToPane waits for the chat TUI to be ready, then types the task into
// the pane and presses Enter (via tmux send-keys).
func (t *AgentTool) sendTaskToPane(ctx context.Context, paneID, task string) error {
	if paneID == "" {
		return nil
	}
	t.waitForPaneReady(ctx, paneID)
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID, "-l", task).Run(); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID, "Enter").Run()
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
		}}, nil
	}

	return nil, fmt.Errorf("provide either 'tasks' (a non-empty array) or 'description' (a single task)")
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
