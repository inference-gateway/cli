package tools

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	audio "github.com/inference-gateway/cli/internal/audio"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	memory "github.com/inference-gateway/cli/internal/platform/memory"
	project "github.com/inference-gateway/cli/internal/platform/project"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

// MCP tools are not built here. The MCP context (internal/mcp) wraps each
// server's tools as agentdomain.Tool values and they arrive through
// RegisterTools: from the liveness loop's status events in chat, and from a
// one-shot discovery in headless. Construction never blocks on MCP I/O
// (issue #523).

type Registry struct {
	config          *config.Config
	toolsMu         sync.RWMutex
	tools           map[string]agentdomain.Tool
	readToolUsed    atomic.Bool
	readFiles       map[string]fileReadSnapshot
	readFilesMu     sync.Mutex
	taskTracker     scheddomain.A2ATaskTracker
	subagentTracker scheddomain.SubagentTracker
	jobSubmitter    scheddomain.JobSubmitter
	jobStopper      scheddomain.JobStopper
	jobLiveness     scheddomain.JobLivenessReporter
	imageService    agentdomain.ImageService
	speechService   agentdomain.SpeechService
	musicService    agentdomain.MusicService
	sfxService      agentdomain.SoundEffectService
	videoService    agentdomain.VideoService
	shellService    scheddomain.BackgroundShellService
	annotator       agentdomain.ImageAnnotator
	frameSources    map[string]agentdomain.FrameSource
	frameSourcesMu  sync.RWMutex
	memoryBackend   memory.MemoryBackend
	stores          *storage.Stores
}

// NewRegistry creates a new tool registry with self-contained tools.
// taskTracker must be provided by the caller (typically the container, which
// constructs the unified BackgroundTaskRegistry and passes its A2A view in
// here so all tools observe the same tracker the agent's wait loop does).
// stores provides the storage backends for the Schedule and RequestPlanApproval
// tools; it may be nil when storage failed to initialize, in which case those
// tools fail at execution with a clear error.
func NewRegistry(cfg *config.Config, imageService agentdomain.ImageService, speechService agentdomain.SpeechService, musicService agentdomain.MusicService, sfxService agentdomain.SoundEffectService, videoService agentdomain.VideoService, shellService scheddomain.BackgroundShellService, annotator agentdomain.ImageAnnotator, taskTracker scheddomain.A2ATaskTracker, stores *storage.Stores) *Registry {
	if taskTracker == nil {
		taskTracker = schedinfra.NewA2ATaskTracker()
	}
	registry := &Registry{
		config:        cfg,
		tools:         make(map[string]agentdomain.Tool),
		shellService:  shellService,
		readFiles:     make(map[string]fileReadSnapshot),
		taskTracker:   taskTracker,
		imageService:  imageService,
		speechService: speechService,
		musicService:  musicService,
		sfxService:    sfxService,
		videoService:  videoService,
		annotator:     annotator,
		frameSources:  make(map[string]agentdomain.FrameSource),
		stores:        stores,
	}
	if st, ok := taskTracker.(scheddomain.SubagentTracker); ok {
		registry.subagentTracker = st
	}
	if js, ok := taskTracker.(scheddomain.JobSubmitter); ok {
		registry.jobSubmitter = js
	}
	if jst, ok := taskTracker.(scheddomain.JobStopper); ok {
		registry.jobStopper = jst
	}
	if lr, ok := taskTracker.(scheddomain.JobLivenessReporter); ok {
		registry.jobLiveness = lr
	}

	registry.registerTools()
	return registry
}

// RegisterFrameSource adds (or replaces) a named frame source. The
// GetLatestFrame tool is registered statically and reports itself enabled as
// soon as at least one source exists, so late registration (e.g. chat starting
// the screenshot server) needs no re-registration machinery.
func (r *Registry) RegisterFrameSource(name string, src agentdomain.FrameSource) {
	if name == "" || src == nil {
		return
	}
	r.frameSourcesMu.Lock()
	r.frameSources[name] = src
	r.frameSourcesMu.Unlock()
}

// FrameSource returns the named frame source.
func (r *Registry) FrameSource(name string) (agentdomain.FrameSource, bool) {
	r.frameSourcesMu.RLock()
	defer r.frameSourcesMu.RUnlock()
	src, ok := r.frameSources[name]
	return src, ok
}

// FrameSourceNames returns the registered source names, sorted.
func (r *Registry) FrameSourceNames() []string {
	r.frameSourcesMu.RLock()
	defer r.frameSourcesMu.RUnlock()
	names := make([]string, 0, len(r.frameSources))
	for name := range r.frameSources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// registerTools initializes and registers all available tools. It runs during
// construction, before the Registry is shared with other goroutines, so it
// does not take toolsMu.
func (r *Registry) registerTools() { // nolint:gocyclo,cyclop
	cfg := r.config

	r.tools["Bash"] = NewBashTool(cfg, r.shellService)

	if cfg.Tools.Bash.BackgroundShells.Enabled && r.shellService != nil {
		r.tools["BashOutput"] = NewBashOutputTool(cfg, r.shellService)
		r.tools["KillShell"] = NewKillShellTool(cfg, r.shellService)
		r.tools["ListShells"] = NewListShellsTool(cfg, r.shellService)
	}

	r.tools["Read"] = NewReadTool(cfg)
	r.tools["Write"] = NewWriteTool(cfg)
	r.tools["Edit"] = NewEditToolWithRegistry(cfg, r)
	r.tools["MultiEdit"] = NewMultiEditToolWithRegistry(cfg, r)
	r.tools["Delete"] = NewDeleteTool(cfg)
	r.tools["Grep"] = NewGrepTool(cfg)
	r.tools["Tree"] = NewTreeTool(cfg)
	r.tools["TodoWrite"] = NewTodoWriteTool(cfg)

	var planStore storage.PlanStorage
	var jobStore storage.ScheduledJobStorage
	if r.stores != nil {
		planStore = r.stores.Plans
		jobStore = r.stores.ScheduledJobs
	}
	r.tools["RequestPlanApproval"] = NewRequestPlanApprovalTool(cfg, planStore)

	if cfg.Tools.AskUserQuestion.Enabled {
		r.tools["AskUserQuestion"] = NewAskUserQuestionTool(cfg)
	}

	r.tools["RequestApproval"] = NewRequestApprovalTool(cfg)

	if cfg.Tools.Schedule.Enabled {
		r.tools["Schedule"] = NewScheduleTool(cfg, jobStore)
	}

	if cfg.Tools.Wait.Enabled {
		r.tools["Wait"] = NewWaitTool(cfg, r.shellService)
	}

	if cfg.IsAgentToolEnabled() && r.subagentTracker != nil {
		r.tools["Agent"] = NewAgentTool(cfg, r.subagentTracker, r.jobSubmitter)
		r.tools["ListSubagents"] = NewListSubagentsTool(cfg, r.subagentTracker)
		r.tools["GetSubagentResult"] = NewGetSubagentResultTool(cfg, r.subagentTracker)
		r.tools["CloseSubagent"] = NewCloseSubagentTool(cfg, r.subagentTracker, r.jobStopper)
		r.tools["ReadSubagentScreen"] = NewReadSubagentScreenTool(cfg, r.subagentTracker)
		r.tools["SendSubagentInput"] = NewSendSubagentInputTool(cfg, r.subagentTracker)
		r.tools["ApproveSubagent"] = NewApproveSubagentTool(cfg, r.subagentTracker)
	}

	if cfg.Tools.WebFetch.Enabled {
		r.tools["WebFetch"] = NewWebFetchTool(cfg)
	}

	if cfg.Tools.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(cfg)
	}

	if cfg.Tools.ImageGeneration.Enabled && r.imageService != nil {
		r.tools["ImageGeneration"] = NewImageGenerationTool(cfg, r.imageService)
	}

	if cfg.Tools.ImageEdit.Enabled && r.imageService != nil {
		r.tools["ImageEdit"] = NewImageEditTool(cfg, r.imageService)
	}

	if cfg.Tools.ImageVariation.Enabled && r.imageService != nil {
		r.tools["ImageVariation"] = NewImageVariationTool(cfg, r.imageService)
	}

	if cfg.TextToSpeech.Enabled {
		r.registerTextToSpeech(cfg)
	}

	if cfg.TextToMusic.Enabled && r.musicService != nil {
		r.tools["TextToMusic"] = NewTextToMusicTool(cfg, r.musicService)
	}

	if cfg.TextToSFX.Enabled && r.sfxService != nil {
		r.tools["TextToSFX"] = NewTextToSFXTool(cfg, r.sfxService)
	}

	if cfg.TextToVideo.Enabled && r.videoService != nil {
		r.tools["TextToVideo"] = NewTextToVideoTool(cfg, r.videoService)
	}

	if cfg.TextToVideo.Enabled && cfg.TextToVideo.CreateAvatar && r.imageService != nil {
		r.tools["CreateAvatar"] = NewCreateAvatarTool(cfg, r.imageService)
	}

	if cfg.IsA2AToolsEnabled() {
		r.tools["A2A_QueryAgent"] = NewA2AQueryAgentTool(cfg)
		r.tools["A2A_QueryTask"] = NewA2AQueryTaskTool(cfg, r.jobLiveness)
		r.tools["A2A_SubmitTask"] = NewA2ASubmitTaskTool(cfg, r.taskTracker, r.jobSubmitter)
	}

	if r.imageService != nil {
		r.tools["ImageDecode"] = NewImageDecodeTool(cfg, r.imageService, r.annotator)
	}

	if cfg.Memory.Enabled {
		r.tools["Memory"] = NewMemoryTool(cfg, r.memoryBackend, project.Detect())
	}
}

// registerTextToSpeech selects the synthesis backend from the configured
// engine: the injected gateway speech service, or the local llama-tts
// synthesizer (the default).
func (r *Registry) registerTextToSpeech(cfg *config.Config) {
	if cfg.TextToSpeech.IsGatewayEngine() {
		if r.speechService != nil {
			r.tools["TextToSpeech"] = NewTextToSpeechTool(cfg, r.speechService)
		}
		return
	}
	r.tools["TextToSpeech"] = NewTextToSpeechTool(cfg, audio.NewSynthesizer(cfg.TextToSpeech))
}

// RegisterTools installs capability tools constructed outside this package
// (browser use, and later computer use). The agent core consumes them through
// the agentdomain.Tool contract only.
func (r *Registry) RegisterTools(tools map[string]agentdomain.Tool) {
	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()
	for name, tool := range tools {
		if name == "" || tool == nil {
			continue
		}
		r.tools[name] = tool
	}
}

// SetMemoryBackend wires the memory sync backend into the Memory tool so a
// write/delete pushes to the remote. The container calls this after
// constructing the shared backend; it re-registers the Memory tool so the
// backend takes effect. A nil backend (or the local no-op backend) means no
// remote sync.
func (r *Registry) SetMemoryBackend(backend memory.MemoryBackend) {
	r.memoryBackend = backend
	if r.config.Memory.Enabled {
		r.toolsMu.Lock()
		r.tools["Memory"] = NewMemoryTool(r.config, backend, project.Detect())
		r.toolsMu.Unlock()
	}
}

// GetTool retrieves a tool by name
func (r *Registry) GetTool(name string) (agentdomain.Tool, error) {
	r.toolsMu.RLock()
	tool, exists := r.tools[name]
	r.toolsMu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}

// ListAvailableTools returns names of all available and enabled tools
func (r *Registry) ListAvailableTools() []string {
	r.toolsMu.RLock()
	defer r.toolsMu.RUnlock()
	var tools []string
	for name, tool := range r.tools {
		if tool.IsEnabled() {
			tools = append(tools, name)
		}
	}
	return tools
}

// GetToolDefinitions returns definitions for all enabled tools, sorted by
// name. The order must be deterministic: it feeds both the outbound tools
// array and the system-prompt roster, and providers serialize tools into the
// cached prompt prefix — a map-order shuffle would invalidate the KV cache
// on every turn despite the byte-stable system prompt.
func (r *Registry) GetToolDefinitions() []sdk.ChatCompletionTool {
	r.toolsMu.RLock()
	defer r.toolsMu.RUnlock()
	var definitions []sdk.ChatCompletionTool
	for _, tool := range r.tools {
		if tool.IsEnabled() {
			definitions = append(definitions, tool.Definition())
		}
	}
	slices.SortFunc(definitions, func(a, b sdk.ChatCompletionTool) int {
		return cmp.Compare(a.Function.Name, b.Function.Name)
	})
	return definitions
}

// IsToolEnabled checks if a specific tool is enabled
func (r *Registry) IsToolEnabled(name string) bool {
	r.toolsMu.RLock()
	tool, exists := r.tools[name]
	r.toolsMu.RUnlock()
	if !exists {
		return false
	}
	return tool.IsEnabled()
}

// UnregisterToolsWithPrefix removes every tool whose name starts with prefix,
// e.g. all tools of an MCP server that disconnected.
func (r *Registry) UnregisterToolsWithPrefix(prefix string) int {
	removedCount := 0

	r.toolsMu.Lock()
	for toolName := range r.tools {
		if strings.HasPrefix(toolName, prefix) {
			delete(r.tools, toolName)
			removedCount++
		}
	}
	r.toolsMu.Unlock()

	if removedCount > 0 {
		logger.Debug("unregistered tools", "prefix", prefix, "count", removedCount)
	}

	return removedCount
}

// SetReadToolUsed marks that the Read tool has been used. Tool calls in one
// assistant turn execute concurrently, so this must be safe under parallel use.
func (r *Registry) SetReadToolUsed() {
	r.readToolUsed.Store(true)
}

// IsReadToolUsed returns whether the Read tool has been used
func (r *Registry) IsReadToolUsed() bool {
	return r.readToolUsed.Load()
}

// fileReadSnapshot captures a file's state the last time the agent read or wrote it, so a later
// edit can detect that the file changed underneath it.
type fileReadSnapshot struct {
	modTime time.Time
	size    int64
}

// RecordFileRead snapshots a file's modtime/size, keyed by its absolute path. Called when the
// Read tool reads a file and refreshed after Edit/MultiEdit/Write so the agent's own writes do
// not look like external modifications.
func (r *Registry) RecordFileRead(path string, modTime time.Time, size int64) {
	key := normalizeReadPath(path)
	r.readFilesMu.Lock()
	defer r.readFilesMu.Unlock()
	r.readFiles[key] = fileReadSnapshot{modTime: modTime, size: size}
}

// LastReadInfo returns the snapshot recorded for path (by absolute path) and whether one exists.
func (r *Registry) LastReadInfo(path string) (time.Time, int64, bool) {
	key := normalizeReadPath(path)
	r.readFilesMu.Lock()
	defer r.readFilesMu.Unlock()
	snap, ok := r.readFiles[key]
	return snap.modTime, snap.size, ok
}

// normalizeReadPath resolves path to an absolute, cleaned form so read and edit sites agree on
// the map key regardless of whether the model passed a relative or absolute path.
func normalizeReadPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// GetBackgroundShellService returns the background shell service instance
func (r *Registry) GetBackgroundShellService() scheddomain.BackgroundShellService {
	return r.shellService
}

// IsComputerUseTool returns true if the given tool name is a computer use tool
// Computer use tools operate directly on the computer (mouse, keyboard,
// screenshot, screen recording) and bypass the standard approval flow
func IsComputerUseTool(toolName string) bool {
	switch toolName {
	case "Computer", "GetLatestFrame", "RecordStart", "RecordStop":
		return true
	}
	return false
}

// IsSessionOnlyTool returns true if the tool keeps state past its own call
// (a running screen recording) and so needs a chat or headless session that
// finalizes it on exit; a one-shot `infer tools execute` cannot.
func IsSessionOnlyTool(toolName string) bool {
	return toolName == "RecordStart" || toolName == "RecordStop"
}
