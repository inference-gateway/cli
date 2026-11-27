package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentheadless "github.com/inference-gateway/cli/internal/agent/headless"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	daemon "github.com/inference-gateway/cli/internal/daemon"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	process "github.com/inference-gateway/cli/internal/platform/process"
	projects "github.com/inference-gateway/cli/internal/projects"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// subagentSystemPromptEnv carries a per-subagent system prompt to the spawned
// subagent (read in initConfig), so each subagent can run with its own role.
const subagentSystemPromptEnv = "INFER_SUBAGENT_SYSTEM_PROMPT"

// AgentTaskSpec is one delegated unit of work within an Agent tool call. Agent
// names a Markdown agent whose settings replace the per-task overrides. Tools
// and MCPServers are passed to the subagent, nil to inherit. Mode is ReadOnly
// without approval or Standard with it, from the `type` argument. Workspace is
// the computer the agent runs on, and Team the note that names its teammates.
// Peer marks a message to another agent, which runs as that agent in a session
// of its own instead of as a subagent.
type AgentTaskSpec struct {
	Label        string
	Description  string
	Model        string
	Files        []string
	SystemPrompt string
	Agent        string
	Tools        []string
	MCPServers   []string
	Mode         agentdomain.AgentMode
	Workspace    string
	Team         string
	Peer         bool
}

// task is what the subagent starts on: the task itself, then its team note.
func (s AgentTaskSpec) task() string {
	if s.Team == "" {
		return s.Description
	}
	return s.Description + "\n\n" + s.Team
}

// AgentSubResult is the per-subagent outcome reported back to the LLM.
type AgentSubResult struct {
	Label     string `json:"label"`
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`

	Stats *scheddomain.SubagentRunStats `json:"stats,omitempty"`
}

// AgentToolResult is the structured payload of an Agent tool call.
type AgentToolResult struct {
	Mode       scheddomain.SubagentMode `json:"mode"`
	Wait       bool                     `json:"wait"`
	Dispatched int                      `json:"dispatched"`
	Subagents  []AgentSubResult         `json:"subagents,omitempty"`
	Message    string                   `json:"message,omitempty"`
}

// AgentTool spawns subagents in parallel and folds their results back into the
// main context, the lightweight complement to the A2A tools. It also sends
// messages between the agents of a team, and relayed counts what it relayed for
// its teammates. mdAgents are the session's Markdown agents, toolManifests
// tells read-only tools, and the func fields are test injection points.
type AgentTool struct {
	config    *config.Config
	tracker   scheddomain.SubagentTracker
	submitter scheddomain.JobSubmitter
	formatter BaseFormatter
	binary    string

	mdAgents      []markdownAgent
	toolManifests agentdomain.ToolManifestLookup
	models        convdomain.ModelService
	relayed       atomic.Int64

	dialDaemon           func(ctx context.Context) (agui.DialConfig, error)
	runHeadless          func(ctx context.Context, opts agentheadless.Options) (agentheadless.Result, error)
	interactiveAvailable func() bool
	launchPane           func(ctx context.Context, title, command string) (string, error)
	sendTask             func(ctx context.Context, paneID, task string) error
	sendKeys             func(ctx context.Context, paneID, text string, keys []string) error
	paneState            func(ctx context.Context, paneID string) paneState
}

// NewAgentTool creates the Agent tool. tracker is the session's SubagentTracker
// (the data store the subagent control tools read). Submitter is the job
// supervisor that monitors async/interactive subagents to completion.
func NewAgentTool(cfg *config.Config, tracker scheddomain.SubagentTracker, submitter scheddomain.JobSubmitter) *AgentTool {
	t := &AgentTool{
		config:    cfg,
		tracker:   tracker,
		submitter: submitter,
		formatter: NewBaseFormatter(ToolAgent),
		binary:    os.Args[0],
	}
	t.dialDaemon = t.dialDaemonBinding
	t.runHeadless = agentheadless.Run
	t.interactiveAvailable = tmuxAvailable
	t.launchPane = t.launchTmuxPane
	t.sendTask = t.sendTaskToPane
	t.sendKeys = tmuxSendKeys
	t.paneState = tmuxPaneState
	return t
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *AgentTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolAgent).WithRequireApproval(t.config.Tools.Agent.RequireApproval)
}

// Definition returns the tool definition for the LLM. An agent at the depth
// limit can only send messages, so it gets no catalog of agents to spawn.
func (t *AgentTool) Definition() sdk.ChatCompletionTool {
	def := t.Manifest().Definition()
	if list := t.markdownAgentCatalog(); list != "" && currentSubagentDepth() < t.maxDepth() {
		description := *def.Function.Description + "\n\n" + list
		def.Function.Description = &description
	}
	return def
}

// Execute runs the tool with the given arguments.
//
//nolint:funlen,gocyclo,cyclop
func (t *AgentTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.IsEnabled() {
		return t.errorResult(args, start, "Agent tool is disabled in configuration"), nil
	}
	if isMessage(args) {
		return t.sendMessage(ctx, args, start), nil
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

	if mode == scheddomain.SubagentModeInteractive && !t.interactiveAvailable() {
		logger.Debug("interactive subagent unavailable, falling back to headless", "reason", "not inside tmux ($TMUX unset)")
		notes = append(notes, "not inside tmux - falling back to headless mode")
		mode = scheddomain.SubagentModeHeadless
	}

	parentSession := agentdomain.GetSessionID(ctx)
	parentModel := agentdomain.GetModel(ctx)
	logger.Debug("agent tool invoked", "mode", mode, "wait", wait, "tasks", len(specs), "parent_session", parentSession)
	served := t.servedModels(ctx)
	for i := range specs {
		requested := specs[i].Model
		if err := t.applyNamedAgent(&specs[i]); err != nil {
			return t.errorResult(args, start, err.Error()), nil
		}
		if err := t.checkPlacement(specs[i], mode); err != nil {
			return t.errorResult(args, start, err.Error()), nil
		}
		if requested != "" && specs[i].Model != requested {
			notes = append(notes, overriddenModelNote(specs[i], requested))
		}
		specs[i].Model = t.resolveModel(specs[i].Model, parentModel)
		if err := unservedModelError(specs[i], served, t.mdAgents, parentModel); err != nil {
			return t.errorResult(args, start, err.Error()), nil
		}
	}
	forceReadOnlySpawns(ctx, specs)
	t.formTeam(specs, mode)

	if mode == scheddomain.SubagentModeInteractive {
		return t.runInteractive(ctx, args, start, specs, parentSession, notes), nil
	}

	if wait {
		return t.runWait(ctx, args, start, specs, mode, parentSession, notes), nil
	}
	return t.runAsync(ctx, args, start, specs, mode, parentSession, notes), nil
}

// runWait fans every subagent out through the job supervisor and blocks until
// all of them finish, returning one aggregated result (fan-out / fan-in).
// Supervising the run - rather than monitoring it on private goroutines - keeps
// a blocking fan-out visible to every registry-backed surface: the sub-agent
// list under the composer, the status-bar counter and the task view.
func (t *AgentTool) runWait(ctx context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, mode scheddomain.SubagentMode, parentSession string, notes []string) *agentdomain.ToolExecutionResult {
	logger.Debug("running subagents synchronously", "tasks", len(specs), "mode", mode)
	jobs := t.dispatchWaitJobs(ctx, specs, mode, parentSession)
	if len(jobs) == 0 {
		return t.errorResult(args, start, "no subagent could be dispatched: the Agent tool has no job supervisor wired")
	}

	results := make([]AgentSubResult, len(jobs))
	success := len(jobs) == len(specs)
	for i, job := range jobs {
		select {
		case <-job.done:
		case <-ctx.Done():
			return t.errorResult(args, start, "subagent fan-out cancelled before every subagent reported back")
		}
		results[i] = job.result()
		if !results[i].Success {
			success = false
		}
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data: AgentToolResult{
			Mode:       mode,
			Wait:       true,
			Dispatched: len(jobs),
			Subagents:  results,
			Message:    strings.Join(notes, "; "),
		},
	}
}

// dispatchWaitJobs tracks each subagent and submits it to the supervisor,
// returning the jobs the blocking fan-in awaits. Each job's context derives
// from ctx, so cancelling the turn kills its subprocess - and a wind-down from
// the task view kills it too.
func (t *AgentTool) dispatchWaitJobs(ctx context.Context, specs []AgentTaskSpec, mode scheddomain.SubagentMode, parentSession string) []*headlessSubagentJob {
	jobs := make([]*headlessSubagentJob, 0, len(specs))
	for _, spec := range specs {
		runCtx, cancel := context.WithCancel(ctx)
		state := &scheddomain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        mode,
			SessionID:   newSubagentSessionID(parentSession),
			Status:      scheddomain.SubagentRunning,
			StartedAt:   time.Now(),
			CancelFunc:  cancel,
			Silent:      true,
		}
		job := &headlessSubagentJob{
			tool:      t,
			spec:      spec,
			state:     state,
			runCtx:    runCtx,
			cancelRun: cancel,
			done:      make(chan struct{}),
		}
		if !t.dispatchHeadless(job) {
			cancel()
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs
}

// dispatchHeadless tracks a headless subagent and hands it to the supervisor -
// the single monitor behind the status bar, the sub-agent list and the task
// view. It reports whether the job was handed over.
func (t *AgentTool) dispatchHeadless(job *headlessSubagentJob) bool {
	if err := t.tracker.AddSubagent(job.state); err != nil {
		logger.Warn("failed to track subagent", "error", err)
		return false
	}
	if t.submitter == nil {
		logger.Warn("headless subagent not supervised: no job submitter", "subagent_id", job.state.ID, "session_id", job.state.SessionID)
		return false
	}
	t.submitter.Submit(job)
	return true
}

// dispatchKeepAlive hands one keep-alive headless subagent, running as session
// sessionID for the session parentSession, to the supervisor. It reports
// whether the job was handed over.
func (t *AgentTool) dispatchKeepAlive(spec AgentTaskSpec, mode scheddomain.SubagentMode, sessionID, parentSession string) bool {
	runCtx, cancel := context.WithCancel(agentdomain.WithSessionID(context.Background(), parentSession))
	state := &scheddomain.SubagentState{
		ID:          uuid.New().String(),
		Label:       spec.Label,
		Description: spec.Description,
		Model:       spec.Model,
		Mode:        mode,
		SessionID:   sessionID,
		Status:      scheddomain.SubagentRunning,
		StartedAt:   time.Now(),
		CancelFunc:  cancel,
		Peer:        spec.Peer,
	}
	job := newKeepAliveSubagentJob(t, spec, state, runCtx, cancel)
	if !t.dispatchHeadless(job) {
		cancel()
		job.release()
		return false
	}
	return true
}

// runAsync dispatches subagents and returns immediately. Each subagent's
// outcome is delivered later by the supervisor monitoring its headlessSubagentJob.
func (t *AgentTool) runAsync(_ context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, mode scheddomain.SubagentMode, parentSession string, notes []string) *agentdomain.ToolExecutionResult {
	logger.Debug("dispatching subagents", "tasks", len(specs), "mode", mode)
	dispatched := make([]AgentSubResult, 0, len(specs))
	for _, spec := range specs {
		sessionID := newSubagentSessionID(parentSession)
		if !t.dispatchKeepAlive(spec, mode, sessionID, parentSession) {
			continue
		}
		dispatched = append(dispatched, AgentSubResult{Label: spec.Label, SessionID: sessionID, Success: true})
	}

	msg := fmt.Sprintf("Dispatched %d subagent(s) in %s mode. END YOUR TURN NOW - you will be notified automatically when each completes; do NOT poll with ListSubagents/GetSubagentResult. To hurry or redirect one, message it with Agent(to: \"<label>\", message: \"...\").", len(dispatched), mode)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, "; ") + ")"
	}
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
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

// threadRouted reports whether headless subagents run as threads on the
// daemon, which needs the binding to dial. Without it they run as subprocesses.
func (t *AgentTool) threadRouted() bool {
	return t.config.Binding().Dialable()
}

// dialDaemonBinding resolves the binding to dial, starting the daemon when
// none listens, the way the browser tools reach it.
func (t *AgentTool) dialDaemonBinding(ctx context.Context) (agui.DialConfig, error) {
	if err := daemon.EnsureRunning(ctx, t.config.Binding().Port); err != nil {
		return agui.DialConfig{}, err
	}
	return daemon.LocalDialConfig(ctx, t.config, subagentClientKind)
}

// openThread opens the subagent's thread on the daemon in the parent's project
// dir, the same dir a subprocess would run in, and moves it to its agent's
// workspace before its first turn when the agent runs on its own computer.
func (t *AgentTool) openThread(ctx context.Context, spec AgentTaskSpec, sessionID string, onTurn func(scheddomain.SubagentResultFile), onFrame func([]byte), onMessage func(scheddomain.TeamMessage)) (*subagentThread, error) {
	dial, err := t.dialDaemon(ctx)
	if err != nil {
		return nil, err
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving the project dir: %w", err)
	}
	thread, err := openSubagentThread(ctx, dial, projectDir, sessionID, t.threadOptions(ctx, spec), onTurn, onFrame, onMessage)
	if err != nil || spec.Workspace == "" {
		return thread, err
	}
	if err := thread.place(ctx, projectDir, spec.Workspace); err != nil {
		thread.stop()
		return nil, err
	}
	return thread, nil
}

// checkPlacement refuses a subagent whose agent runs on its own computer where
// only the daemon could put it there: without the binding, or in a tmux pane.
func (t *AgentTool) checkPlacement(spec AgentTaskSpec, mode scheddomain.SubagentMode) error {
	if spec.Workspace == "" || (mode == scheddomain.SubagentModeHeadless && t.threadRouted()) {
		return nil
	}
	return fmt.Errorf("agent %s runs on workspace %s, which only a headless subagent on the daemon can reach - enable the daemon binding or remove its workspace", spec.Agent, spec.Workspace)
}

// threadOptions are the subagent's thread options: the task's model, mode and
// system prompt, who spawned it with what tools, and its label and agent. A peer
// names its agent for the daemon to resolve, like a chat opened as that agent,
// and the session that sent it. The daemon's worker inherits the rest, the
// gateway mock switch included, from the daemon itself.
func (t *AgentTool) threadOptions(ctx context.Context, spec AgentTaskSpec) sessionsdomain.ThreadOptions {
	if spec.Peer {
		return sessionsdomain.ThreadOptions{
			Model:  spec.Model,
			Agent:  spec.Agent,
			Origin: sessionsdomain.Origin{Label: spec.Label, SentBy: agentdomain.GetSessionID(ctx)},
		}
	}
	opts := sessionsdomain.ThreadOptions{
		Model:        spec.Model,
		SystemPrompt: spec.SystemPrompt,
		MCPServers:   spec.MCPServers,
		Subagent: &sessionsdomain.Subagent{
			Parent: agentdomain.GetSessionID(ctx),
			Depth:  currentSubagentDepth() + 1,
			Tools:  spec.Tools,
		},
		Origin: sessionsdomain.Origin{Agent: spec.Agent, Label: spec.Label},
	}
	if spec.Mode != agentdomain.AgentModeStandard {
		opts.Mode = spec.Mode.ModeKey()
	}
	return opts
}

// executeOne runs a single headless subagent and returns its final assistant
// message and run stats. onLine sees each line the run prints as it arrives,
// and a stdin keeps the child alive for follow-up turns. Interactive subagents
// are handled separately by runInteractive.
func (t *AgentTool) executeOne(ctx context.Context, spec AgentTaskSpec, sessionID string, onLine func(line []byte), stdin *os.File) (string, *scheddomain.SubagentRunStats, error) {
	resultFile := subagentResultFilePath(sessionID)
	_ = os.Remove(resultFile)
	defer func() { _ = os.Remove(resultFile) }()

	res, err := t.runHeadless(ctx, agentheadless.Options{
		BinaryPath: t.binary,
		SessionID:  sessionID,
		Prompt:     spec.task(),
		Model:      spec.Model,
		Files:      spec.Files,
		ResultFile: resultFile,
		ExtraEnv:   t.subagentExtraEnv(ctx, spec),
		OnLine:     onLine,
		Stdin:      stdin,
		KeepAlive:  stdin != nil,
	})

	answer := res.FinalAssistant
	var stats *scheddomain.SubagentRunStats
	if rf, ok := scheddomain.ReadSubagentResultFile(resultFile); ok {
		stats = rf.Stats
		if rf.FinalAssistant != "" {
			answer = rf.FinalAssistant
		}
		if err == nil && !rf.Success && rf.Error != "" {
			err = fmt.Errorf("%s", rf.Error)
		}
	}
	if err != nil && strings.TrimSpace(res.Stderr) != "" {
		err = fmt.Errorf("%w: %s", err, stderrTail(res.Stderr, 500))
	}
	return answer, stats, err
}

// runInteractive launches each subagent in its own live `infer chat` tmux pane,
// types in the task, and tracks it as a running interactive subagent. This is
// fire-and-track: it returns once the panes are launched. The supervisor then
// monitors them. A done or failed result turn and the idle timeout each close
// the pane, and an Agent message re-prompts a live one.
func (t *AgentTool) runInteractive(ctx context.Context, args map[string]any, start time.Time, specs []AgentTaskSpec, parentSession string, notes []string) *agentdomain.ToolExecutionResult {
	logger.Debug("launching interactive subagents", "tasks", len(specs), "parent_session", parentSession)
	launched := make([]AgentSubResult, 0, len(specs))
	for _, spec := range specs {
		sessionID := newSubagentSessionID(parentSession)
		title := spec.Label
		if title == "" {
			title = "subagent"
		}
		_ = os.Remove(subagentResultFilePath(sessionID))
		_ = os.Remove(subagentApprovalFilePath(sessionID))
		paneID, err := t.launchPane(ctx, title, t.buildChatPaneCommand(spec, parentSession, sessionID))
		if err != nil {
			logger.Error("failed to open tmux pane for interactive subagent", "label", labelOrSession(spec.Label, sessionID), "session_id", sessionID, "error", err)
			notes = append(notes, fmt.Sprintf("%s: failed to open tmux pane: %v", labelOrSession(spec.Label, sessionID), err))
			continue
		}
		logger.Debug("interactive subagent pane created", "pane_id", paneID, "session_id", sessionID, "label", spec.Label)
		if err := t.sendTask(ctx, paneID, spec.Description); err != nil {
			logger.Warn("failed to send task to interactive subagent pane", "pane", paneID, "error", err)
		}

		state := &scheddomain.SubagentState{
			ID:          uuid.New().String(),
			Label:       spec.Label,
			Description: spec.Description,
			Model:       spec.Model,
			Mode:        scheddomain.SubagentModeInteractive,
			SessionID:   sessionID,
			PaneID:      paneID,
			Status:      scheddomain.SubagentRunning,
			StartedAt:   time.Now(),
		}
		if err := t.tracker.AddSubagent(state); err != nil {
			logger.Warn("failed to track interactive subagent", "error", err)
			continue
		}

		if t.submitter != nil {
			t.submitter.Submit(newInteractiveSubagentJob(t, state))
		} else {
			logger.Warn("interactive subagent not supervised: no job submitter", "subagent_id", state.ID, "pane_id", paneID)
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
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: AgentToolResult{
			Mode:       scheddomain.SubagentModeInteractive,
			Wait:       false,
			Dispatched: len(launched),
			Subagents:  launched,
			Message:    msg,
		},
	}
}

// labelOrSession returns label, or a short session id when the label is blank,
// for use in user-facing notes.
func stderrTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

func labelOrSession(label, sessionID string) string {
	if label != "" {
		return label
	}
	return shortSession(sessionID)
}

// buildChatPaneCommand assembles the shell command run inside the tmux pane: a
// live `infer chat` using the parent model (via INFER_AGENT_MODEL so no model
// dropdown appears) plus the depth guard and invocation metadata. The chat runs
// under the subagent's session ID, so the parent can read its conversation.
func (t *AgentTool) buildChatPaneCommand(spec AgentTaskSpec, parentSession, sessionID string) string {
	parts := []string{fmt.Sprintf("%s=%d", scheddomain.EnvSubagentDepth, currentSubagentDepth()+1)}
	if parentSession != "" {
		parts = append(parts, scheddomain.EnvSubagentParentSession+"="+shellQuote(parentSession))
	}
	parts = append(parts, scheddomain.EnvSubagentInvokedBy+"="+scheddomain.SubagentInvokedByAgent)
	if spec.Label != "" {
		parts = append(parts, scheddomain.EnvSessionLabel+"="+shellQuote(spec.Label))
	}
	if spec.Agent != "" {
		parts = append(parts, scheddomain.EnvSessionAgent+"="+shellQuote(spec.Agent))
	}
	if spec.SystemPrompt != "" {
		parts = append(parts, subagentSystemPromptEnv+"="+shellQuote(spec.SystemPrompt))
	}
	if len(spec.Tools) > 0 {
		parts = append(parts, scheddomain.EnvSubagentTools+"="+shellQuote(strings.Join(spec.Tools, ",")))
	}
	if spec.MCPServers != nil {
		parts = append(parts, scheddomain.EnvMCPServers+"="+shellQuote(strings.Join(spec.MCPServers, ",")))
	}
	if spec.Mode != agentdomain.AgentModeStandard {
		parts = append(parts, scheddomain.EnvSubagentAgentMode+"="+shellQuote(spec.Mode.ModeKey()))
	}
	if spec.Model != "" {
		parts = append(parts, "INFER_AGENT_MODEL="+shellQuote(spec.Model))
	}
	if t.config.Gateway.Mock && t.config.Tools.Agent.InheritMock {
		parts = append(parts, "INFER_GATEWAY_MOCK=true")
	}

	historyName := projects.Slugify(spec.Label)
	if historyName == "" {
		historyName = scheddomain.SubagentHistoryMemoryOnly
	}
	parts = append(parts, scheddomain.EnvSubagentHistoryName+"="+shellQuote(historyName))

	parts = append(parts, scheddomain.EnvSubagentResultFile+"="+shellQuote(subagentResultFilePath(sessionID)))
	parts = append(parts, scheddomain.EnvSubagentApprovalFile+"="+shellQuote(subagentApprovalFilePath(sessionID)))
	parts = append(parts, shellQuote(t.binary), "chat", "--session-id", shellQuote(sessionID))
	return strings.Join(parts, " ")
}

// subagentExtraEnv builds the environment passed to a headless subagent: the
// depth guard plus an optional per-subagent system prompt and trace context,
// and invocation metadata (parent session, invoked_by).
func (t *AgentTool) subagentExtraEnv(ctx context.Context, spec AgentTaskSpec) []string {
	env := []string{fmt.Sprintf("%s=%d", scheddomain.EnvSubagentDepth, currentSubagentDepth()+1)}
	env = append(env, agentdomain.GetTraceEnv(ctx)...)
	if parentSession := agentdomain.GetSessionID(ctx); parentSession != "" {
		env = append(env, scheddomain.EnvSubagentParentSession+"="+parentSession)
	}
	env = append(env, scheddomain.EnvSubagentInvokedBy+"="+scheddomain.SubagentInvokedByAgent)
	if spec.Label != "" {
		env = append(env, scheddomain.EnvSessionLabel+"="+spec.Label)
	}
	if spec.Agent != "" {
		env = append(env, scheddomain.EnvSessionAgent+"="+spec.Agent)
	}
	if spec.SystemPrompt != "" {
		env = append(env, subagentSystemPromptEnv+"="+spec.SystemPrompt)
	}
	if len(spec.Tools) > 0 {
		env = append(env, scheddomain.EnvSubagentTools+"="+strings.Join(spec.Tools, ","))
	}
	if spec.MCPServers != nil {
		env = append(env, scheddomain.EnvMCPServers+"="+strings.Join(spec.MCPServers, ","))
	}
	if spec.Mode != agentdomain.AgentModeStandard {
		env = append(env, scheddomain.EnvSubagentAgentMode+"="+spec.Mode.ModeKey())
	}
	if t.config.Gateway.Mock && t.config.Tools.Agent.InheritMock {
		env = append(env, "INFER_GATEWAY_MOCK=true")
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
		out, err := process.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", paneID).Output()
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
// the pane is created. The pane keeps running the subagent and, via
// remain-on-exit, stays open after it finishes so its output remains readable.
func (t *AgentTool) launchTmuxPane(ctx context.Context, title, command string) (string, error) {
	args := []string{"split-window", "-v", "-P", "-F", "#{pane_id}", command}
	out, err := process.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		var stderr string
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		logger.Error("tmux pane creation failed", "tmux_args", strings.Join(args, " "), "error", err, "stderr", stderr)
		return "", err
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		logger.Error("tmux split-window returned an empty pane id", "tmux_args", strings.Join(args, " "))
		return "", nil
	}

	_ = process.CommandContext(ctx, "tmux", "set-option", "-p", "-t", paneID, "remain-on-exit", "on").Run()
	_ = process.CommandContext(ctx, "tmux", "select-pane", "-t", paneID, "-T", title).Run()
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
	af, ok := scheddomain.ReadSubagentApprovalFile(subagentApprovalFilePath(sessionID))
	if !ok || !af.Awaiting {
		return "", false
	}
	return strings.TrimSpace(af.Summary), true
}

// readSubagentResultMessage returns the subagent chat's real last assistant
// message from its result file (trimmed), or "" if the file is absent or empty.
// It is the single harvest path: the subagent's pane is never scraped for content
// (its TUI chrome is noise), so on a miss callers deliver nothing. Shared by the
// poller's inspector and the GetSubagentResult / CloseSubagent tools.
func readSubagentResultMessage(sessionID string) string {
	if rf, ok := scheddomain.ReadSubagentResultFile(subagentResultFilePath(sessionID)); ok {
		return strings.TrimSpace(rf.FinalAssistant)
	}
	return ""
}

// Validate checks the tool arguments.
func (t *AgentTool) Validate(args map[string]any) error {
	if isMessage(args) {
		return validateMessage(args)
	}
	_, err := parseAgentTasks(args)
	return err
}

// IsEnabled reports whether the tool is enabled. Past the depth limit it stays
// on, so a teammate can message its team, and only spawning is refused.
func (t *AgentTool) IsEnabled() bool {
	return t.config.IsAgentToolEnabled()
}

// RequiresApproval lets a message through, since it changes nothing, and gates
// a spawn by tools.agent.require_approval outside auto-accept and read-only runs.
func (t *AgentTool) RequiresApproval(args map[string]any, mode agentdomain.AgentMode) bool {
	if isMessage(args) || mode == agentdomain.AgentModeAutoAccept || mode == agentdomain.AgentModeReadOnly {
		return false
	}
	return t.Manifest().RequiresApproval(t.config.Tools.Safety.RequireApproval)
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
func (t *AgentTool) resolveMode() scheddomain.SubagentMode {
	mode := scheddomain.SubagentMode(strings.TrimSpace(string(t.config.Tools.Agent.Mode)))
	if mode == scheddomain.SubagentModeInteractive {
		return scheddomain.SubagentModeInteractive
	}
	return scheddomain.SubagentModeHeadless
}

// resolveWait returns whether the tool blocks for aggregated results, from
// config (tools.agent.wait) only. Like mode, this is an operator policy - not a
// per-call LLM choice - so there is no wait parameter the model can override.
func (t *AgentTool) resolveWait() bool {
	return t.config.Tools.Agent.Wait
}

// setMarkdownAgents installs the Markdown-defined agents loaded once per
// session by the registry (see mdagent.go), and the manifests that decide
// whether an agent's tool allowlist is read-only. Called during construction,
// before the tool is shared, so no synchronization is needed.
func (t *AgentTool) setMarkdownAgents(agents []markdownAgent, toolManifests agentdomain.ToolManifestLookup) {
	t.mdAgents = agents
	t.toolManifests = toolManifests
}

// markdownAgentCatalog renders the Available agents list appended to the tool
// description, or "" when no agents are loaded (description stays unchanged).
func (t *AgentTool) markdownAgentCatalog() string {
	if len(t.mdAgents) == 0 {
		return ""
	}
	agents := slices.SortedFunc(slices.Values(t.mdAgents), func(a, b markdownAgent) int {
		return strings.Compare(a.name, b.name)
	})

	var out strings.Builder
	out.WriteString("Available agents:")
	for _, agent := range agents {
		fmt.Fprintf(&out, "\n- %s: %s", agentdomain.AgentLabel(agent.name, agent.role), agent.description)
	}
	return out.String()
}

// applyNamedAgent overlays a Markdown agent preset onto the task spec. The body
// becomes the system prompt and a frontmatter model wins unless it is inherit.
// The mode is ReadOnly when every allowed tool is read-only and ReadWrite
// otherwise, including with no allowlist, where the parent's tools are kept. An
// allowlist always keeps Agent, so the subagent can message its team.
func (t *AgentTool) applyNamedAgent(spec *AgentTaskSpec) error {
	if spec.Agent == "" {
		return nil
	}
	agent, err := findMarkdownAgent(t.mdAgents, spec.Agent)
	if err != nil {
		return err
	}
	spec.SystemPrompt = agent.systemPrompt
	if agent.model != "" {
		spec.Model = agent.model
	}
	spec.MCPServers = agent.mcpServers
	spec.Workspace = agent.workspace
	if len(agent.tools) > 0 {
		spec.Mode = deriveSubagentMode(agent.tools, t.toolManifests)
		spec.Tools = agent.tools
		if !slices.Contains(agent.tools, ToolAgent) && !slices.Contains(agent.disallowedTools, ToolAgent) {
			spec.Tools = append(slices.Clone(agent.tools), ToolAgent)
		}
	} else {
		spec.Mode = agentdomain.AgentModeStandard
	}
	return nil
}

// resolveModel picks the subagent model: the task's model, which a named agent's
// file model has already replaced, else the configured tools.agent.model, else
// the parent turn's model (so subagents inherit the model the user is already
// using). An empty result would make the subagent fail with "no model specified".
func (t *AgentTool) resolveModel(taskModel, parentModel string) string {
	if taskModel != "" {
		return taskModel
	}
	if t.config.Tools.Agent.Model != "" {
		return t.config.Tools.Agent.Model
	}
	return parentModel
}

// servedModels lists the models the gateway serves, or nil when it cannot tell,
// so an unknown list never blocks a spawn.
func (t *AgentTool) servedModels(ctx context.Context) []string {
	if t.models == nil {
		return nil
	}
	served, err := t.models.ListModels(ctx)
	if err != nil {
		logger.Debug("agent tool could not list the served models", "error", err)
		return nil
	}
	return served
}

// overriddenModelNote tells the caller a task's model was not used because the
// named agent's file pins another one, which wins as docs/subagents.md says.
func overriddenModelNote(spec AgentTaskSpec, requested string) string {
	return fmt.Sprintf("model %q not used for agent %s - its file pins %q, and an agent file's model wins over a task's", requested, spec.Agent, spec.Model)
}

// unservedModelError refuses a task whose model the gateway does not serve,
// before anything is dispatched. When the model is pinned by the named agent's
// file it names that file, since a task's model cannot override it. Nil when
// the model is served or the served list is unknown.
func unservedModelError(spec AgentTaskSpec, served []string, agents []markdownAgent, parentModel string) error {
	if len(served) == 0 || spec.Model == "" || slices.Contains(served, spec.Model) {
		return nil
	}
	if agent, err := findMarkdownAgent(agents, spec.Agent); err == nil && agent.model == spec.Model {
		return fmt.Errorf("agent %s cannot run: its file %s pins model %q, which the gateway does not serve. A task's model cannot override an agent file's model - change or remove the model line in that file", spec.Agent, agent.path, spec.Model)
	}
	if parentModel == "" {
		return fmt.Errorf("model %q is not served by the gateway", spec.Model)
	}
	return fmt.Errorf("model %q is not served by the gateway - pass one it serves, such as %q, the model this chat runs on", spec.Model, parentModel)
}

func (t *AgentTool) errorResult(args map[string]any, start time.Time, msg string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     msg,
		Data:      AgentToolResult{Message: msg},
	}
}

// FormatResult formats tool execution results for different contexts.
func (t *AgentTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatResult(result, formatType, t.FormatForUI, t.FormatForLLM, t.FormatPreview)
}

// FormatPreview returns a compact one-line preview.
func (t *AgentTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	if sent, ok := result.Data.(AgentMessageResult); ok {
		return "Messaged " + sent.To
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
func (t *AgentTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	return t.formatter.FormatCollapsed(result, t.FormatPreview(result))
}

// FormatForLLM renders a structured detail block for the assistant.
func (t *AgentTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Agent result unavailable"
	}
	var dataContent string
	if result.Data != nil {
		dataContent = t.formatAgentData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

func (t *AgentTool) formatAgentData(data any) string {
	d, ok := data.(AgentToolResult)
	if !ok {
		if sub, okSub := data.(AgentSubResult); okSub {
			return formatSubResult(sub)
		}
		if sent, okSent := data.(AgentMessageResult); okSent {
			return sent.Message
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
	if sub.Stats != nil {
		fmt.Fprintf(&out, "%s\n", sub.Stats)
	}
	if sub.Error != "" {
		fmt.Fprintf(&out, "Error: %s\n", sub.Error)
	}
	if sub.Result != "" {
		fmt.Fprintf(&out, "%s\n", sub.Result)
	}
	return out.String()
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
			desc := subagentTask(m)
			if desc == "" {
				return nil, fmt.Errorf("tasks[%d].description is required and must be a non-empty string", i)
			}
			specs = append(specs, AgentTaskSpec{
				Label:        optionalString(m, "label"),
				Description:  desc,
				Model:        optionalString(m, "model"),
				Files:        optionalStringSlice(m, "files"),
				SystemPrompt: optionalString(m, "system_prompt"),
				Agent:        optionalString(m, "agent"),
				Mode:         resolveSubagentType(optionalString(m, "type")),
			})
		}
		return specs, nil
	}

	if desc := subagentTask(args); desc != "" {
		return []AgentTaskSpec{{
			Label:        optionalString(args, "label"),
			Description:  desc,
			Model:        optionalString(args, "model"),
			Files:        optionalStringSlice(args, "files"),
			SystemPrompt: optionalString(args, "system_prompt"),
			Agent:        optionalString(args, "agent"),
			Mode:         resolveSubagentType(optionalString(args, "type")),
		}}, nil
	}

	return nil, fmt.Errorf("provide either 'tasks' (a non-empty array) or 'description' (a single task)")
}

// subagentTask is the text a subagent starts on. Models trained on other agent
// tools send the short title as description and the full instructions as
// prompt, so a prompt follows the description instead of being dropped.
func subagentTask(m map[string]any) string {
	parts := make([]string, 0, 2)
	for _, key := range []string{"description", "prompt"} {
		if text := strings.TrimSpace(optionalString(m, key)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// resolveSubagentType maps the Agent tool's `type` argument to a subagent
// capability mode: "ReadWrite" -> AgentModeStandard (can mutate, approval
// applies). Anything else, including the default empty value, -> AgentModeReadOnly
// (Explore-like read/search, no approval). Matching is case-insensitive.
func resolveSubagentType(t string) agentdomain.AgentMode {
	if strings.EqualFold(strings.TrimSpace(t), "ReadWrite") {
		return agentdomain.AgentModeStandard
	}
	return agentdomain.AgentModeReadOnly
}

// forceReadOnlySpawns coerces every spec to ReadOnly while the parent run is in
// plan or read-only mode, so a subagent can only explore whatever capability
// the call asked for or a named agent derived. It runs after applyNamedAgent,
// the last place a spec's mode is set.
func forceReadOnlySpawns(ctx context.Context, specs []AgentTaskSpec) {
	mode, ok := agentdomain.AgentModeFromContext(ctx)
	if !ok || (mode != agentdomain.AgentModePlan && mode != agentdomain.AgentModeReadOnly) {
		return
	}
	for i := range specs {
		specs[i].Mode = agentdomain.AgentModeReadOnly
	}
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
	const shortSessionIDLength = 16
	if len(id) > shortSessionIDLength {
		return id[:shortSessionIDLength]
	}
	return id
}

// currentSubagentDepth reads the recursion depth from the environment (0 at the
// top level).
func currentSubagentDepth() int {
	if v := os.Getenv(scheddomain.EnvSubagentDepth); v != "" {
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
