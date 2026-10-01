package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
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

	// A keep-alive job holds the child's stdin open between turns, reports
	// each turn itself and closes the child after idleTimeout without input.
	keepAlive   bool
	idleTimeout time.Duration
	stdinRead   *os.File
	emit        func(scheddomain.JobSignal)

	mu         sync.Mutex
	output     string
	outcome    AgentSubResult
	live       scheddomain.SubagentRunStats
	stdinWrite *os.File
	idle       *time.Timer
	closing    bool
}

// idleKillGrace is how long a hung-up keep-alive child gets to exit on its own
// before it is killed.
const idleKillGrace = 10 * time.Second

// newKeepAliveSubagentJob builds an async headless job whose child keeps
// reading follow-up messages on a pipe. The job reports its own turns, so the
// subagent is Silent to the supervisor. Without a pipe it is a one-shot job.
func newKeepAliveSubagentJob(tool *AgentTool, spec AgentTaskSpec, state *scheddomain.SubagentState, runCtx context.Context, cancel context.CancelFunc) *headlessSubagentJob {
	job := &headlessSubagentJob{tool: tool, spec: spec, state: state, runCtx: runCtx, cancelRun: cancel}
	r, w, err := os.Pipe()
	if err != nil {
		logger.Warn("subagent stdin pipe failed, running one turn only", "subagent_id", state.ID, "error", err)
		return job
	}
	job.keepAlive, job.stdinRead, job.stdinWrite = true, r, w
	if secs := tool.config.Tools.Agent.IdleTimeout; secs > 0 {
		job.idleTimeout = time.Duration(secs) * time.Second
	}
	state.Silent = true
	state.Input = job.send
	return job
}

// release closes the pipe of a keep-alive job that was never dispatched.
func (j *headlessSubagentJob) release() {
	if j.stdinRead != nil {
		_ = j.stdinRead.Close()
	}
	j.hangUp()
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
func (j *headlessSubagentJob) Run(ctx context.Context, emit func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	if j.keepAlive {
		return j.runKeepAlive(ctx, emit)
	}
	logger.Debug("headless subagent starting", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	runCtx := j.runCtx
	if runCtx == nil {
		runCtx = ctx
	}
	if j.cancelRun != nil {
		defer context.AfterFunc(ctx, j.cancelRun)()
	}

	defer j.signalDone()

	answer, stats, err := j.tool.executeOne(runCtx, j.spec, j.state.SessionID, j.tally, nil)
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

// runKeepAlive runs the child with its stdin held open. Each turn line the
// child prints is reported as it arrives, the idle timer hangs up a quiet
// child, and a stop from the supervisor kills it.
func (j *headlessSubagentJob) runKeepAlive(ctx context.Context, emit func(scheddomain.JobSignal)) agentdomain.ToolExecutionResult {
	logger.Debug("keep-alive headless subagent starting", "subagent_id", j.state.ID, "session_id", j.state.SessionID)
	j.emit = emit
	defer context.AfterFunc(ctx, func() { j.cancelRun(); j.hangUp() })()

	answer, stats, err := j.tool.executeOne(j.runCtx, j.spec, j.state.SessionID, j.onLine, j.stdinRead)
	_ = j.stdinRead.Close()
	j.hangUp()
	return j.exitResult(answer, stats, err)
}

// onLine tallies one line the child printed and reports the turn it ends.
func (j *headlessSubagentJob) onLine(line []byte) {
	j.tally(line)
	var turn scheddomain.SubagentTurnLine
	if json.Unmarshal(line, &turn) != nil || turn.Type != scheddomain.SubagentTurnLineType {
		return
	}
	j.completeTurn(turn.SubagentResultFile)
}

// completeTurn records a finished turn and emits its note. A done turn leaves
// the subagent idle until the parent writes again or the idle timer closes it.
// The note is emitted outside the lock: the supervisor hands it to the UI,
// whose render reads this job back through Idle and Stats.
func (j *headlessSubagentJob) completeTurn(turn scheddomain.SubagentResultFile) {
	if note := j.recordTurn(turn); note != "" {
		j.emit(scheddomain.JobSignal{Note: note, Enqueue: true})
	}
}

func (j *headlessSubagentJob) recordTurn(turn scheddomain.SubagentResultFile) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closing {
		return ""
	}
	j.output = turn.FinalAssistant
	j.outcome = toSubResult(j.spec, j.state.SessionID, turn.FinalAssistant, nil)
	j.outcome.Success, j.outcome.Error, j.outcome.Stats = turn.Success, turn.Error, turn.Stats
	logger.Debug("keep-alive headless subagent turn finished", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "success", turn.Success, "done", turn.Done)
	if turn.Done {
		j.setStatus(turnStatusOf(turn.Success))
		j.armIdleLocked()
	}
	return subagentNote(turnVerb(turn.Success), j.label(), turnNoteBody(turn.Stats, turn.FinalAssistant, turn.Error))
}

func (j *headlessSubagentJob) armIdleLocked() {
	if j.idle != nil {
		j.idle.Stop()
	}
	if j.idleTimeout > 0 {
		j.idle = time.AfterFunc(j.idleTimeout, j.closeIdle)
	}
}

// closeIdle hangs up on a subagent that sat idle past the timeout and
// announces it, then kills the child if it ignores the hang-up.
func (j *headlessSubagentJob) closeIdle() {
	note := j.hangUpIdleLocked()
	if note == "" {
		return
	}
	j.emit(scheddomain.JobSignal{Note: note, Enqueue: true})
	time.AfterFunc(idleKillGrace, j.cancelRun)
}

func (j *headlessSubagentJob) hangUpIdleLocked() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closing || j.stdinWrite == nil {
		return ""
	}
	j.closing = true
	logger.Debug("keep-alive headless subagent idle, hanging up", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "idle_timeout", j.idleTimeout)
	j.hangUpLocked()
	return closedMessage(j.label(), fmt.Sprintf("closed after %s of inactivity", j.idleTimeout), j.output)
}

// send writes one follow-up user message to the child as its next turn.
func (j *headlessSubagentJob) send(text string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closing || j.stdinWrite == nil {
		return fmt.Errorf("subagent %s has exited and accepts no more messages", j.label())
	}
	frame := runInputFrame{Type: ipc.RunAgentInputFrameType, Input: agui.RunAgentInput{
		ThreadID: j.state.SessionID, RunID: uuid.NewString(),
		Messages: []agui.Message{{ID: uuid.NewString(), Role: "user", Content: text}},
	}}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := j.stdinWrite.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to subagent %s: %w", j.label(), err)
	}
	if j.idle != nil {
		j.idle.Stop()
	}
	j.setStatus(scheddomain.SubagentRunning)
	return nil
}

// runInputFrame is the stdin frame that runs a message as the child's next turn.
type runInputFrame struct {
	Type  string             `json:"type"`
	Input agui.RunAgentInput `json:"input"`
}

// exitResult turns the child's exit into the job's outcome. An idle close or a
// requested stop was announced already or needs no note. Any other exit while
// a turn was still running is a failure the parent must hear about.
func (j *headlessSubagentJob) exitResult(answer string, stats *scheddomain.SubagentRunStats, err error) agentdomain.ToolExecutionResult {
	result, note := j.recordExit(answer, stats, err)
	if note != "" {
		j.emit(scheddomain.JobSignal{Note: note, Enqueue: true})
	}
	return result
}

func (j *headlessSubagentJob) recordExit(answer string, stats *scheddomain.SubagentRunStats, err error) (agentdomain.ToolExecutionResult, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.idle != nil {
		j.idle.Stop()
	}
	note := ""
	switch {
	case j.closing, j.runCtx.Err() != nil:
		j.setStatus(scheddomain.SubagentCompleted)
	case err != nil || j.state.Status == scheddomain.SubagentRunning:
		if err == nil {
			err = fmt.Errorf("exited before finishing its turn")
		}
		j.outcome = toSubResult(j.spec, j.state.SessionID, answer, err)
		j.outcome.Stats = stats
		j.setStatus(scheddomain.SubagentFailed)
		note = subagentNote("Failed", j.label(), turnNoteBody(stats, answer, err.Error()))
	}
	logger.Debug("keep-alive headless subagent exited", "subagent_id", j.state.ID, "session_id", j.state.SessionID, "error", err)
	return agentdomain.ToolExecutionResult{
		ToolName:  ToolAgent,
		Arguments: map[string]any{"label": j.outcome.Label, "session_id": j.state.SessionID},
		Success:   j.outcome.Success && err == nil,
		Error:     j.outcome.Error,
		Duration:  time.Since(j.state.StartedAt),
		Data:      j.outcome,
	}, note
}

// Idle reports whether the keep-alive child sits between turns, so the
// supervisor does not hold the parent's session for it.
func (j *headlessSubagentJob) Idle() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.keepAlive && j.state.Status != scheddomain.SubagentRunning
}

// hangUp closes the child's stdin once so it exits after its current turn.
func (j *headlessSubagentJob) hangUp() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.hangUpLocked()
}

func (j *headlessSubagentJob) hangUpLocked() {
	if j.stdinWrite != nil {
		_ = j.stdinWrite.Close()
		j.stdinWrite = nil
	}
}

func (j *headlessSubagentJob) setStatus(status scheddomain.SubagentStatus) {
	if err := j.tool.tracker.SetSubagentStatus(j.state.ID, status); err != nil {
		logger.Debug("subagent status update skipped", "subagent_id", j.state.ID, "error", err)
	}
}

func (j *headlessSubagentJob) label() string {
	return labelOrSession(j.state.Label, j.state.SessionID)
}

// Output returns the subagent's final result message for the /tasks detail panel.
func (j *headlessSubagentJob) Output() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.output
}

// Stats returns the run stats the subagent reported. Until it reports them,
// they are the live tally of the output it has printed so far.
func (j *headlessSubagentJob) Stats() *scheddomain.SubagentRunStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.outcome.Stats != nil {
		return j.outcome.Stats
	}
	live := j.live
	return &live
}

// subagentOutputLine is what the live tally reads from one line a headless run
// prints: a tool result's outcome or an assistant step's token usage.
type subagentOutputLine struct {
	Type       string               `json:"type"`
	Role       sdk.MessageRole      `json:"role"`
	Failed     bool                 `json:"failed"`
	TokenUsage *sdk.CompletionUsage `json:"token_usage"`
}

// tally adds one line the running subagent printed to its live stats.
func (j *headlessSubagentJob) tally(line []byte) {
	var msg subagentOutputLine
	if err := json.Unmarshal(line, &msg); err != nil || msg.Type != "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case msg.Role == sdk.Tool && msg.Failed:
		j.live.ToolsFailed++
	case msg.Role == sdk.Tool:
		j.live.ToolsSucceeded++
	case msg.Role == sdk.Assistant && msg.TokenUsage != nil:
		j.live.InputTokens += int(msg.TokenUsage.PromptTokens)
		j.live.OutputTokens += int(msg.TokenUsage.CompletionTokens)
		j.live.CachedTokens += cachedTokens(msg.TokenUsage)
	}
}

// cachedTokens is the prompt-cache hit count of one step, zero when the
// provider reported none.
func cachedTokens(usage *sdk.CompletionUsage) int {
	if usage.PromptTokensDetails == nil || usage.PromptTokensDetails.CachedTokens == nil {
		return 0
	}
	return int(*usage.PromptTokensDetails.CachedTokens)
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
	return turnNoteBody(obs.HarvestStats, obs.Harvested, obs.HarvestError)
}

// turnNoteBody renders a finished turn's note body: the run stats, the answer
// and the error when the turn failed.
func turnNoteBody(stats *scheddomain.SubagentRunStats, answer, errText string) string {
	parts := make([]string, 0, 3)
	if stats != nil {
		parts = append(parts, stats.String())
	}
	if body := strings.TrimSpace(answer); body != "" {
		parts = append(parts, body)
	}
	if errText = strings.TrimSpace(errText); errText != "" {
		parts = append(parts, "Error: "+errText)
	}
	return strings.Join(parts, "\n\n")
}

// turnStatus maps a terminal turn to the tracker status it records: a failed
// turn marks the subagent failed, a done one completed.
func turnStatus(obs scheddomain.PaneObservation) scheddomain.SubagentStatus {
	return turnStatusOf(!obs.HarvestFailed)
}

func turnStatusOf(success bool) scheddomain.SubagentStatus {
	if success {
		return scheddomain.SubagentCompleted
	}
	return scheddomain.SubagentFailed
}

func turnVerb(success bool) string {
	if success {
		return "Completed"
	}
	return "Failed"
}

// subagentNote is the one note shape every subagent event lands on the queue.
func subagentNote(verb, label, body string) string {
	return fmt.Sprintf("[Subagent %s: %s]\n\n%s", verb, label, body)
}

// closedMessage announces an idle-timeout close as one self-contained note,
// carrying the last message when there is one.
func closedMessage(label, reason, body string) string {
	content := subagentNote("Closed", label, reason)
	if trimmed := strings.TrimSpace(body); trimmed != "" {
		content += "\n\n" + trimmed
	}
	return content
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
	return subagentNote("Completed", labelOrSession(j.state.Label, j.state.SessionID), body)
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

// closedMessage announces the idle-timeout close of a pane that never
// reported done.
func (j *interactiveSubagentJob) closedMessage(body string) string {
	reason := fmt.Sprintf("closed after %s of inactivity without a done signal", j.idleTimeout)
	return closedMessage(labelOrSession(j.state.Label, j.state.SessionID), reason, body)
}

func (j *interactiveSubagentJob) approvalMessage(summary string) string {
	content := fmt.Sprintf("[Subagent Awaiting Approval: %s]", labelOrSession(j.state.Label, j.state.SessionID))
	if s := strings.TrimSpace(summary); s != "" {
		content += "\n\n" + s
	}
	content += fmt.Sprintf("\n\nThis subagent is blocked waiting to run the above. Review it, then respond with ApproveSubagent(subagent_id=%q, decision=\"approve\") or decision=\"reject\".", j.state.ID)
	return content
}
