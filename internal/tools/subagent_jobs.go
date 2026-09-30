package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// headlessSubagentJob adapts a headless subagent (an `infer headless` subprocess) to
// a BackgroundJob: Run executes it and reports the outcome. Structurally it is a
// subprocess like a shell - the supervisor owns the goroutine and the completion
// notification, replacing the SubagentPoller's headless path.
type headlessSubagentJob struct {
	tool      *AgentTool
	spec      AgentTaskSpec
	state     *scheddomain.SubagentState
	runCtx    context.Context
	cancelRun context.CancelFunc

	// done is closed once Run returns, waking the blocking fan-in that awaits
	// this job. nil for a fire-and-forget dispatch, which nobody awaits.
	done chan struct{}

	mu      sync.Mutex
	output  string
	outcome AgentSubResult
}

// Meta describes the subagent for the task view.
func (j *headlessSubagentJob) Meta() scheddomain.JobMeta {
	return scheddomain.JobMeta{
		ID:           j.state.ID,
		SessionID:    j.state.SessionID,
		Kind:         scheddomain.JobKindSubagent,
		Label:        labelOrSession(j.state.Label, j.state.SessionID),
		Description:  j.state.Description,
		Detail:       string(scheddomain.SubagentModeHeadless),
		StartedAt:    j.state.StartedAt,
		Silent:       j.state.Silent,
		HoldsSession: true,
	}
}

// Run executes the subagent subprocess and returns its outcome. It runs under the
// detached runCtx (so it survives the spawning turn); AfterFunc ties the
// supervisor's context to it, so Wind/Stop/shutdown also cancel the subprocess
// (via exec.CommandContext). Run returns promptly on either cancellation.
func (j *headlessSubagentJob) Run(ctx context.Context, _ func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	logger.Debug("headless subagent starting", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	runCtx := j.runCtx
	if runCtx == nil {
		runCtx = ctx
	}
	if j.cancelRun != nil {
		defer context.AfterFunc(ctx, j.cancelRun)()
	}

	defer j.signalDone()

	answer, stats, err := j.tool.executeOne(runCtx, j.spec, j.state.SessionID)
	sub := toSubResult(j.spec, j.state.SessionID, answer, err)
	sub.Stats = stats
	j.mu.Lock()
	j.output = answer
	j.outcome = sub
	j.mu.Unlock()

	status := scheddomain.SubagentCompleted
	if !sub.Success {
		status = scheddomain.SubagentFailed
	}
	if e := j.tool.tracker.SetSubagentStatus(j.state.ID, status); e != nil {
		logger.Warn("subagent status update failed", "id", j.state.ID, "error", e)
	}
	logger.Debug("headless subagent finished", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "success", sub.Success)

	return agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: map[string]any{"label": sub.Label, "session_id": j.state.SessionID},
		Success:   sub.Success,
		Error:     sub.Error,
		Duration:  time.Since(j.state.StartedAt),
		Data:      sub,
	}
}

// Output returns the subagent's final result message for the /tasks detail panel.
func (j *headlessSubagentJob) Output() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output
}

// Stats returns the run stats the subagent reported, nil until Run returns.
func (j *headlessSubagentJob) Stats() *scheddomain.SubagentRunStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.outcome.Stats
}

// result returns the subagent's outcome for the blocking fan-in that awaited
// the job. Zero-valued until Run returns.
func (j *headlessSubagentJob) result() AgentSubResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.outcome
}

// signalDone wakes a blocking fan-in waiter once Run has returned. It is a
// no-op for a fire-and-forget job, which no one awaits.
func (j *headlessSubagentJob) signalDone() {
	if j.done != nil {
		close(j.done)
	}
}

// Wind is a no-op: the supervisor cancels Run's context, which kills the
// subprocess.
func (j *headlessSubagentJob) Wind(_ context.Context, _ scheddomain.WindSignal) error { return nil }

// Close removes the subagent from the tracker on reap.
func (j *headlessSubagentJob) Close() {
	logger.Debug("closing headless subagent", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	_ = j.tool.tracker.RemoveSubagent(j.state.ID)
}

// interactiveSubagentJob monitors a live interactive subagent (a tmux pane
// running `infer chat`) for its whole life. Run polls the pane and returns once
// the subagent reports done, fails, idles out, or its pane closes. Each terminal
// outcome tears the subagent down inline. The result file's done flag is the
// completion signal and screen stability is only an idle hint.
type interactiveSubagentJob struct {
	tool    *AgentTool
	state   *scheddomain.SubagentState
	inspect func(ctx context.Context, paneID, sessionID string) scheddomain.PaneObservation

	// Heuristic tunables (overridable in tests).
	pollInterval time.Duration
	grace        time.Duration
	stableNeeded int
	idleTimeout  time.Duration

	mu     sync.Mutex
	output string
	stats  *scheddomain.SubagentRunStats
}

func newInteractiveSubagentJob(tool *AgentTool, state *scheddomain.SubagentState) *interactiveSubagentJob {
	j := &interactiveSubagentJob{
		tool:         tool,
		state:        state,
		inspect:      NewPaneInspector(),
		pollInterval: 2 * time.Second,
		grace:        4 * time.Second,
		stableNeeded: 3,
	}
	if tool != nil && tool.config != nil {
		if secs := tool.config.Tools.Agent.IdleTimeout; secs > 0 {
			j.idleTimeout = time.Duration(secs) * time.Second
		}
	}
	return j
}

// Meta describes the interactive subagent. It is Silent because each completed
// turn's output is emitted as its own note, so the terminal result adds nothing.
// HoldsSession is false: a user-driven interactive pane must not keep a headless
// session alive, so the supervisor's HasPending skips it.
func (j *interactiveSubagentJob) Meta() scheddomain.JobMeta {
	return scheddomain.JobMeta{
		ID:           j.state.ID,
		SessionID:    j.state.SessionID,
		Kind:         scheddomain.JobKindSubagent,
		Label:        labelOrSession(j.state.Label, j.state.SessionID),
		Description:  j.state.Description,
		Detail:       string(scheddomain.SubagentModeInteractive),
		StartedAt:    j.state.StartedAt,
		Silent:       true,
		HoldsSession: false,
	}
}

// Run watches the pane until it reports done, fails, hits the idle timeout, or
// closes, emitting each completed turn's output and any pending-approval prompts.
func (j *interactiveSubagentJob) Run(ctx context.Context, emit func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	logger.Debug("monitoring interactive subagent pane", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "session_id", j.state.SessionID)
	ticker := time.NewTicker(j.pollInterval)
	defer ticker.Stop()

	started := time.Now()
	lastHarvest := ""
	notifiedApproval := ""
	idleNotified := false
	prevScreen := ""
	stableTicks := 0
	lastActivity := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.Debug("interactive subagent monitor cancelled", "subagent_id", j.state.ID, "pane_id", j.state.PaneID)
			j.teardown(scheddomain.SubagentCompleted)
			return agentdomain.ToolExecutionResult{ToolName: ToolAgent, Success: true}
		case <-ticker.C:
			obs := j.inspect(ctx, j.state.PaneID, j.state.SessionID)

			if obs.Gone || obs.Dead {
				logger.Debug("interactive subagent pane closed", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "gone", obs.Gone, "dead", obs.Dead)
				j.harvestTurn(obs.Harvested, &lastHarvest, emit)
				j.teardown(scheddomain.SubagentCompleted)
				return agentdomain.ToolExecutionResult{ToolName: ToolAgent, Success: true}
			}

			if obs.AwaitingApproval {
				if notifiedApproval != obs.ApprovalSummary {
					notifiedApproval = obs.ApprovalSummary
					emit(scheddomain.JobSignal{Note: j.approvalMessage(obs.ApprovalSummary), Enqueue: true})
				}
				lastActivity, stableTicks, prevScreen = time.Now(), 0, obs.Screen
				continue
			}
			notifiedApproval = ""

			if obs.Done {
				j.recordDoneTurn(obs, emit)
				j.teardown(turnStatus(obs))
				return agentdomain.ToolExecutionResult{ToolName: ToolAgent, Success: !obs.HarvestFailed}
			}

			if body := strings.TrimSpace(obs.Harvested); body != "" && body != lastHarvest {
				j.harvestTurn(obs.Harvested, &lastHarvest, emit)
				idleNotified = true
				lastActivity, stableTicks, prevScreen = time.Now(), 0, obs.Screen
				continue
			}

			if time.Since(started) < j.grace {
				prevScreen = obs.Screen
				continue
			}
			if obs.Screen == prevScreen {
				stableTicks++
			} else {
				stableTicks, idleNotified = 0, false
				lastActivity = time.Now()
			}
			prevScreen = obs.Screen
			if stableTicks >= j.stableNeeded && !idleNotified {
				idleNotified = true
				emit(scheddomain.JobSignal{Note: j.idleMessage(time.Since(lastActivity)), Enqueue: true})
			}
			if j.idleTimedOut(lastActivity) {
				emit(scheddomain.JobSignal{Note: j.closedMessage(j.Output()), Enqueue: true})
				j.teardown(scheddomain.SubagentCompleted)
				return agentdomain.ToolExecutionResult{ToolName: ToolAgent, Success: true}
			}
		}
	}
}

// harvestTurn emits a completed turn's output once and consumes the result file
// so the next turn's write is a fresh signal.
func (j *interactiveSubagentJob) harvestTurn(harvested string, last *string, emit func(scheddomain.JobSignal)) {
	body := strings.TrimSpace(harvested)
	if body == "" || body == *last {
		return
	}
	*last = body
	j.mu.Lock()
	j.output = body
	j.mu.Unlock()
	logger.Debug("interactive subagent turn harvested", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "bytes", len(body))
	emit(scheddomain.JobSignal{Note: j.completedMessage(body), Enqueue: true})
	_ = os.Remove(subagentResultFilePath(j.state.SessionID))
}

// Output returns the last harvested turn for the /tasks detail panel.
func (j *interactiveSubagentJob) Output() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output
}

// Stats returns the run stats of the terminal turn, nil until one is harvested.
func (j *interactiveSubagentJob) Stats() *scheddomain.SubagentRunStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.stats
}

// recordDoneTurn records a terminal turn's output and emits its single
// completion note. The monitor tears the subagent down right after.
func (j *interactiveSubagentJob) recordDoneTurn(obs scheddomain.PaneObservation, emit func(scheddomain.JobSignal)) {
	j.mu.Lock()
	j.output = strings.TrimSpace(obs.Harvested)
	j.stats = obs.HarvestStats
	j.mu.Unlock()
	logger.Debug("interactive subagent terminal turn harvested", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "failed", obs.HarvestFailed)
	emit(scheddomain.JobSignal{Note: j.completedMessage(turnResultBody(obs)), Enqueue: true})
}

// turnResultBody renders the terminal turn's note body: the run stats, the
// harvested answer and the recorded error when the turn failed.
func turnResultBody(obs scheddomain.PaneObservation) string {
	parts := make([]string, 0, 3)
	if obs.HarvestStats != nil {
		parts = append(parts, obs.HarvestStats.String())
	}
	if body := strings.TrimSpace(obs.Harvested); body != "" {
		parts = append(parts, body)
	}
	if errText := strings.TrimSpace(obs.HarvestError); errText != "" {
		parts = append(parts, "Error: "+errText)
	}
	return strings.Join(parts, "\n\n")
}

// turnStatus maps a terminal turn to the tracker status it records: a failed
// turn marks the subagent failed, a done one completed.
func turnStatus(obs scheddomain.PaneObservation) scheddomain.SubagentStatus {
	if obs.HarvestFailed {
		return scheddomain.SubagentFailed
	}
	return scheddomain.SubagentCompleted
}

// Wind kills the pane on WindStop, which makes Run tear down and return.
// WindWrapUp is a no-op because a user-driven pane has no graceful wind-down.
func (j *interactiveSubagentJob) Wind(ctx context.Context, sig scheddomain.WindSignal) error {
	if sig == scheddomain.WindStop {
		return tmuxKillPane(ctx, j.state.PaneID)
	}
	return nil
}

// Close drops the subagent from the tracker on reap. The monitor's teardown
// has already killed the pane and removed the temp files.
func (j *interactiveSubagentJob) Close() {
	logger.Debug("closing interactive subagent, dropping tracker entry", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "session_id", j.state.SessionID)
	_ = j.tool.tracker.RemoveSubagent(j.state.ID)
}

// teardown closes an interactive subagent inline on every terminal outcome,
// because the supervisor only calls Close on reap. It kills the pane, removes
// the temp files and records the terminal status. It is idempotent.
func (j *interactiveSubagentJob) teardown(status scheddomain.SubagentStatus) {
	logger.Debug("tearing down interactive subagent", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "session_id", j.state.SessionID, "status", string(status))
	_ = tmuxKillPane(context.Background(), j.state.PaneID)
	_ = os.Remove(subagentResultFilePath(j.state.SessionID))
	_ = os.Remove(subagentApprovalFilePath(j.state.SessionID))
	if err := j.tool.tracker.SetSubagentStatus(j.state.ID, status); err != nil {
		logger.Debug("interactive subagent status update skipped", "subagent_id", j.state.ID, "error", err)
	}
}

func (j *interactiveSubagentJob) completedMessage(body string) string {
	return fmt.Sprintf("[Subagent Completed: %s]\n\n%s", labelOrSession(j.state.Label, j.state.SessionID), body)
}

// idleTimedOut reports whether the pane has sat inactive - no harvested turn,
// unchanged screen, no pending approval - past the configured idle timeout.
// A zero timeout disables the auto-close.
func (j *interactiveSubagentJob) idleTimedOut(lastActivity time.Time) bool {
	return j.idleTimeout > 0 && time.Since(lastActivity) >= j.idleTimeout
}

// idleMessage is the no-output fallback for a frozen, never-harvested pane. It
// warns instead of masquerading as a completion, and names the pending auto-close
// so the parent can intercede with SendSubagentInput first.
func (j *interactiveSubagentJob) idleMessage(idleFor time.Duration) string {
	content := fmt.Sprintf("[Subagent Idle: %s]\n\nThe subagent ended its turn without output or is waiting for input. Do not assume it failed or produced nothing: use ReadSubagentScreen to inspect it or SendSubagentInput to re-prompt it.",
		labelOrSession(j.state.Label, j.state.SessionID))
	if j.idleTimeout > 0 {
		remaining := max(j.idleTimeout-idleFor, 0)
		content += fmt.Sprintf(" It will be closed in %s unless it reports done or receives input.", remaining.Round(time.Second))
	}
	return content
}

// closedMessage announces the idle-timeout close as one self-contained note,
// carrying the last harvested message when there is one.
func (j *interactiveSubagentJob) closedMessage(body string) string {
	content := fmt.Sprintf("[Subagent Closed: %s]\n\nclosed after %s of inactivity without a done signal",
		labelOrSession(j.state.Label, j.state.SessionID), j.idleTimeout)
	if trimmed := strings.TrimSpace(body); trimmed != "" {
		content += "\n\n" + trimmed
	}
	return content
}

func (j *interactiveSubagentJob) approvalMessage(summary string) string {
	content := fmt.Sprintf("[Subagent Awaiting Approval: %s]", labelOrSession(j.state.Label, j.state.SessionID))
	if s := strings.TrimSpace(summary); s != "" {
		content += "\n\n" + s
	}
	content += fmt.Sprintf("\n\nThis subagent is blocked waiting to run the above. Review it, then respond with ApproveSubagent(subagent_id=%q, decision=\"approve\") or decision=\"reject\".", j.state.ID)
	return content
}
