package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// headlessSubagentJob adapts a headless subagent (an `infer agent` subprocess) to
// a BackgroundJob: Run executes it and reports the outcome. Structurally it is a
// subprocess like a shell - the supervisor owns the goroutine and the completion
// notification, replacing the SubagentPoller's headless path.
type headlessSubagentJob struct {
	tool      *AgentTool
	spec      AgentTaskSpec
	state     *domain.SubagentState
	runCtx    context.Context
	cancelRun context.CancelFunc
	output    string
}

// Meta describes the subagent for the task view.
func (j *headlessSubagentJob) Meta() domain.JobMeta {
	return domain.JobMeta{
		ID:           j.state.ID,
		Kind:         domain.JobKindSubagent,
		Label:        labelOrSession(j.state.Label, j.state.SessionID),
		Description:  j.state.Description,
		Detail:       domain.SubagentModeHeadless,
		StartedAt:    j.state.StartedAt,
		Silent:       j.state.Silent,
		HoldsSession: true,
	}
}

// Run executes the subagent subprocess and returns its outcome. It runs under the
// detached runCtx (so it survives the spawning turn); AfterFunc ties the
// supervisor's context to it, so Wind/Stop/shutdown also cancel the subprocess
// (via exec.CommandContext). Run returns promptly on either cancellation.
func (j *headlessSubagentJob) Run(ctx context.Context, _ func(domain.JobSignal)) domain.ToolExecutionResult {
	logger.Debug("headless subagent starting", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	runCtx := j.runCtx
	if runCtx == nil {
		runCtx = ctx
	}
	if j.cancelRun != nil {
		defer context.AfterFunc(ctx, j.cancelRun)()
	}

	answer, err := j.tool.executeOne(runCtx, j.spec, j.state.SessionID)
	j.output = answer
	sub := toSubResult(j.spec, j.state.SessionID, answer, err)

	status := domain.SubagentCompleted
	if !sub.Success {
		status = domain.SubagentFailed
	}
	if e := j.tool.tracker.SetSubagentStatus(j.state.ID, status); e != nil {
		logger.Warn("subagent status update failed", "id", j.state.ID, "error", e)
	}
	logger.Debug("headless subagent finished", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "success", sub.Success)

	return domain.ToolExecutionResult{
		ToolName:  "Agent",
		Arguments: map[string]any{"label": sub.Label, "session_id": j.state.SessionID},
		Success:   sub.Success,
		Error:     sub.Error,
		Duration:  time.Since(j.state.StartedAt),
		Data:      sub,
	}
}

// Output returns the subagent's final result message for the /tasks detail panel.
func (j *headlessSubagentJob) Output() string { return j.output }

// Wind is a no-op: the supervisor cancels Run's context, which kills the
// subprocess.
func (j *headlessSubagentJob) Wind(_ context.Context, _ domain.WindSignal) error { return nil }

// Close removes the subagent from the tracker on reap.
func (j *headlessSubagentJob) Close() {
	logger.Debug("closing headless subagent", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	_ = j.tool.tracker.RemoveSubagent(j.state.ID)
}

// interactiveSubagentJob monitors a live interactive subagent (a tmux pane
// running `infer chat`) for its whole life. Run polls the pane and emits a
// notification for each completed turn (and for pending approvals); it returns
// only when the pane closes. This is the supervisor's replacement for the
// SubagentPoller's interactive path - one persistent monitor instead of a
// discovery ticker plus per-turn re-arming. The completion heuristic prefers the
// authoritative result file (Harvested); screen stability is only an idle
// fallback for a turn that produced no result file.
type interactiveSubagentJob struct {
	tool    *AgentTool
	state   *domain.SubagentState
	inspect func(ctx context.Context, paneID, sessionID string) domain.PaneObservation

	// Heuristic tunables (overridable in tests).
	pollInterval time.Duration
	grace        time.Duration
	stableNeeded int
}

func newInteractiveSubagentJob(tool *AgentTool, state *domain.SubagentState) *interactiveSubagentJob {
	return &interactiveSubagentJob{
		tool:         tool,
		state:        state,
		inspect:      NewPaneInspector(),
		pollInterval: 2 * time.Second,
		grace:        4 * time.Second,
		stableNeeded: 3,
	}
}

// Meta describes the interactive subagent. It is Silent because each completed
// turn's output is emitted as its own note, so the terminal result adds nothing.
// HoldsSession is false: a user-driven interactive pane must not keep a headless
// session alive, so the supervisor's HasPending skips it.
func (j *interactiveSubagentJob) Meta() domain.JobMeta {
	return domain.JobMeta{
		ID:           j.state.ID,
		Kind:         domain.JobKindSubagent,
		Label:        labelOrSession(j.state.Label, j.state.SessionID),
		Description:  j.state.Description,
		Detail:       domain.SubagentModeInteractive,
		StartedAt:    j.state.StartedAt,
		Silent:       true,
		HoldsSession: false,
	}
}

// Run watches the pane until it closes, emitting each completed turn's output and
// any pending-approval prompts.
func (j *interactiveSubagentJob) Run(ctx context.Context, emit func(domain.JobSignal)) domain.ToolExecutionResult {
	logger.Debug("monitoring interactive subagent pane", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "session_id", j.state.SessionID)
	ticker := time.NewTicker(j.pollInterval)
	defer ticker.Stop()

	started := time.Now()
	lastHarvest := ""
	notifiedApproval := ""
	idleNotified := false
	prevScreen := ""
	stableTicks := 0

	for {
		select {
		case <-ctx.Done():
			logger.Debug("interactive subagent monitor cancelled", "subagent_id", j.state.ID, "pane_id", j.state.PaneID)
			return domain.ToolExecutionResult{ToolName: "Agent", Success: true}
		case <-ticker.C:
			obs := j.inspect(ctx, j.state.PaneID, j.state.SessionID)

			if obs.Gone || obs.Dead {
				logger.Debug("interactive subagent pane closed", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "gone", obs.Gone, "dead", obs.Dead)
				j.harvestTurn(obs.Harvested, &lastHarvest, emit)
				return domain.ToolExecutionResult{ToolName: "Agent", Success: true}
			}

			if obs.AwaitingApproval {
				if notifiedApproval != obs.ApprovalSummary {
					notifiedApproval = obs.ApprovalSummary
					emit(domain.JobSignal{Note: j.approvalMessage(obs.ApprovalSummary), Enqueue: true})
				}
				stableTicks, prevScreen = 0, obs.Screen
				continue
			}
			notifiedApproval = ""

			if obs.Harvested != "" && obs.Harvested != lastHarvest {
				j.harvestTurn(obs.Harvested, &lastHarvest, emit)
				idleNotified, stableTicks, prevScreen = true, 0, obs.Screen
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
			}
			prevScreen = obs.Screen
			if stableTicks >= j.stableNeeded && !idleNotified {
				idleNotified = true
				emit(domain.JobSignal{Note: j.idleMessage(), Enqueue: true})
			}
		}
	}
}

// harvestTurn emits a completed turn's output once and consumes the result file
// so the next turn's write is a fresh signal.
func (j *interactiveSubagentJob) harvestTurn(harvested string, last *string, emit func(domain.JobSignal)) {
	body := strings.TrimSpace(harvested)
	if body == "" || body == *last {
		return
	}
	*last = body
	logger.Debug("interactive subagent turn harvested", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "bytes", len(body))
	emit(domain.JobSignal{Note: j.completedMessage(body), Enqueue: true})
	_ = os.Remove(subagentResultFilePath(j.state.SessionID))
}

// Wind kills the pane on WindStop (which makes Run observe Gone and return);
// WindWrapUp is a no-op (no graceful wind-down for a user-driven pane).
func (j *interactiveSubagentJob) Wind(ctx context.Context, sig domain.WindSignal) error {
	if sig == domain.WindStop {
		return tmuxKillPane(ctx, j.state.PaneID)
	}
	return nil
}

// Close tears the subagent down on reap: kill the (remain-on-exit) pane, remove
// temp files, and drop it from the tracker.
func (j *interactiveSubagentJob) Close() {
	logger.Debug("closing interactive subagent, killing pane", "subagent_id", j.state.ID, "pane_id", j.state.PaneID, "session_id", j.state.SessionID)
	_ = tmuxKillPane(context.Background(), j.state.PaneID)
	_ = os.Remove(subagentResultFilePath(j.state.SessionID))
	_ = os.Remove(subagentApprovalFilePath(j.state.SessionID))
	_ = j.tool.tracker.RemoveSubagent(j.state.ID)
}

func (j *interactiveSubagentJob) completedMessage(body string) string {
	return fmt.Sprintf("[Subagent Completed: %s]\n\n%s", labelOrSession(j.state.Label, j.state.SessionID), body)
}

func (j *interactiveSubagentJob) idleMessage() string {
	return fmt.Sprintf("[Subagent Completed: %s]\n\n(No final message was captured - the subagent ended its turn without output or is waiting for input. Use ReadSubagentScreen to inspect it, SendSubagentInput to re-prompt it, or CloseSubagent to stop it. Do not assume it failed or produced nothing.)",
		labelOrSession(j.state.Label, j.state.SessionID))
}

func (j *interactiveSubagentJob) approvalMessage(summary string) string {
	content := fmt.Sprintf("[Subagent Awaiting Approval: %s]", labelOrSession(j.state.Label, j.state.SessionID))
	if s := strings.TrimSpace(summary); s != "" {
		content += "\n\n" + s
	}
	content += fmt.Sprintf("\n\nThis subagent is blocked waiting to run the above. Review it, then respond with ApproveSubagent(subagent_id=%q, decision=\"approve\") or decision=\"reject\".", j.state.ID)
	return content
}
