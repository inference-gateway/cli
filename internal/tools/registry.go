package tools

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	memory "github.com/inference-gateway/cli/internal/platform/memory"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	projects "github.com/inference-gateway/cli/internal/projects"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// MCP tools are not built here. The MCP context (internal/protocols/mcp) wraps each
// server's tools as agentdomain.Tool values and they arrive through
// RegisterTools. Construction never blocks on MCP I/O.

type Registry struct {
	config         *config.Config
	toolsMu        sync.RWMutex
	tools          map[string]agentdomain.Tool
	readToolUsed   atomic.Bool
	readFiles      map[string]fileReadSnapshot
	readFilesMu    sync.Mutex
	deps           RegistryDeps
	frameSources   map[string]agentdomain.FrameSource
	frameSourcesMu sync.RWMutex
	memoryBackend  memory.MemoryBackend
	mdAgents       []markdownAgent
}

// RegistryDeps carries the optional collaborators NewRegistry wires into the
// tools it registers. The zero value is valid: a nil collaborator leaves out or
// degrades the tools that need it. BackgroundTasks is the container's registry,
// shared with the agent's wait loop, and nil Stores fail Schedule and
// RequestPlanApproval at execution.
type RegistryDeps struct {
	Image             agentdomain.ImageService
	Speech            agentdomain.SpeechService
	Music             agentdomain.MusicService
	SoundEffects      agentdomain.SoundEffectService
	Video             agentdomain.VideoService
	VideoSoundEffects agentdomain.VideoSoundEffectService
	BackgroundShells  scheddomain.BackgroundShellService
	ImageAnnotator    agentdomain.ImageAnnotator
	BackgroundTasks   scheddomain.BackgroundTaskRegistry
	Stores            *storage.Stores
}

// NewRegistry creates a new tool registry with self-contained tools.
func NewRegistry(cfg *config.Config, deps RegistryDeps) *Registry {
	registry := &Registry{
		config:       cfg,
		tools:        make(map[string]agentdomain.Tool),
		readFiles:    make(map[string]fileReadSnapshot),
		deps:         deps,
		frameSources: make(map[string]agentdomain.FrameSource),
	}

	registry.registerTools()
	return registry
}

// LoadMarkdownAgents loads the Markdown subagent definitions (.infer/agents/*.md)
// once per session and installs them into the Agent tool, so its tool
// description lists them and tool-name validation runs against the tools this
// session registered so far. Tools registered after it, such as MCP's, are
// unknown to the agents.
func (r *Registry) LoadMarkdownAgents() {
	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()
	agentTool, ok := r.tools[ToolAgent].(*AgentTool)
	if !ok {
		return
	}
	known := make(map[string]bool, len(r.tools))
	for name := range r.tools {
		known[name] = true
	}
	r.mdAgents = loadMarkdownAgents(nil, known)
	agentTool.setMarkdownAgents(r.mdAgents, r)
}

// MarkdownSubagents returns the Markdown-defined subagent presets loaded this
// session (.infer/agents/*.md, project then user home), for the /agents listing.
func (r *Registry) MarkdownSubagents() []agentdomain.SubagentInfo {
	if len(r.mdAgents) == 0 {
		return nil
	}
	infos := make([]agentdomain.SubagentInfo, 0, len(r.mdAgents))
	for _, agent := range r.mdAgents {
		infos = append(infos, agent.subagentInfo(r))
	}
	return infos
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
	return slices.Sorted(maps.Keys(r.frameSources))
}

// registerTools initializes and registers all available tools. It runs during
// construction, before the Registry is shared with other goroutines, so it
// does not take toolsMu.
func (r *Registry) registerTools() { // nolint:gocyclo,cyclop
	cfg := r.config

	r.register(NewBashTool(cfg, r.deps.BackgroundShells))

	if cfg.Tools.Bash.BackgroundShells.Enabled && r.deps.BackgroundShells != nil {
		r.register(NewBashOutputTool(cfg, r.deps.BackgroundShells))
		r.register(NewKillShellTool(cfg, r.deps.BackgroundShells))
		r.register(NewListShellsTool(cfg, r.deps.BackgroundShells))
	}

	r.register(NewReadTool(cfg, r.deps.Image))
	r.register(NewWriteTool(cfg))
	r.register(NewEditTool(cfg, r))
	r.register(NewMultiEditTool(cfg, r))
	r.register(NewDeleteTool(cfg))
	r.register(NewGrepTool(cfg))
	r.register(NewTreeTool(cfg))
	r.register(NewTodoWriteTool(cfg))

	var planStore storage.PlanStorage
	var jobStore storage.ScheduledJobStorage
	if r.deps.Stores != nil {
		planStore = r.deps.Stores.Plans
		jobStore = r.deps.Stores.ScheduledJobs
	}
	r.register(NewRequestPlanApprovalTool(cfg, planStore))

	if cfg.Tools.AskUserQuestion.Enabled {
		r.register(NewAskUserQuestionTool(cfg))
	}

	r.register(NewRequestApprovalTool(cfg))

	if cfg.Tools.Schedule.Enabled {
		r.register(NewScheduleTool(cfg, jobStore))
	}

	if cfg.Tools.Wait.Enabled {
		r.register(NewWaitTool(cfg, r.deps.BackgroundShells))
	}

	if cfg.IsAgentToolEnabled() && r.deps.BackgroundTasks != nil {
		r.register(NewAgentTool(cfg, r.deps.BackgroundTasks, r.deps.BackgroundTasks))
		r.register(NewListSubagentsTool(cfg, r.deps.BackgroundTasks))
		r.register(NewGetSubagentResultTool(cfg, r.deps.BackgroundTasks))
		r.register(NewCloseSubagentTool(cfg, r.deps.BackgroundTasks, r.deps.BackgroundTasks))
		r.register(NewReadSubagentScreenTool(cfg, r.deps.BackgroundTasks))
		r.register(NewSendSubagentInputTool(cfg, r.deps.BackgroundTasks))
		r.register(NewApproveSubagentTool(cfg, r.deps.BackgroundTasks))
	}

	if cfg.Tools.WebFetch.Enabled {
		r.register(NewWebFetchTool(cfg))
	}

	if cfg.Tools.WebSearch.Enabled {
		r.register(NewWebSearchTool(cfg))
	}

	if cfg.Tools.ImageGeneration.Enabled && r.deps.Image != nil {
		r.register(NewImageGenerationTool(cfg, r.deps.Image))
	}

	if cfg.Tools.ImageEdit.Enabled && r.deps.Image != nil {
		r.register(NewImageEditTool(cfg, r.deps.Image))
	}

	if cfg.Tools.ImageVariation.Enabled && r.deps.Image != nil {
		r.register(NewImageVariationTool(cfg, r.deps.Image))
	}

	if cfg.TextToSpeech.Enabled {
		r.registerTextToSpeech(cfg)
	}

	if cfg.TextToMusic.Enabled && r.deps.Music != nil {
		r.register(NewTextToMusicTool(cfg, r.deps.Music))
	}

	if cfg.TextToSFX.Enabled && r.deps.SoundEffects != nil {
		r.register(NewTextToSFXTool(cfg, r.deps.SoundEffects))
	}

	if cfg.TextToVideo.Enabled && r.deps.Video != nil {
		r.register(NewTextToVideoTool(cfg, r.deps.Video))
	}

	if cfg.TextToVideo.Enabled && cfg.TextToVideo.CreateAvatar && r.deps.Image != nil {
		r.register(NewCreateAvatarTool(cfg, r.deps.Image))
	}

	if cfg.VideoToSFX.Enabled && r.deps.VideoSoundEffects != nil {
		r.register(NewVideoToSFXTool(cfg, r.deps.VideoSoundEffects))
	}

	if cfg.VideoToSFX.DubEnabled() && r.deps.Video != nil {
		r.register(NewDubVideoTool(cfg, r.deps.Video))
	}

	if r.deps.Image != nil {
		r.register(NewImageDecodeTool(cfg, r.deps.Image, r.deps.ImageAnnotator))
	}

	if cfg.Memory.Enabled {
		r.register(NewMemoryTool(cfg, r.memoryBackend, projects.Detect()))
	}
}

// registerTextToSpeech registers the tool on the gateway speech service, the
// one way speech is synthesized.
func (r *Registry) registerTextToSpeech(cfg *config.Config) {
	if r.deps.Speech != nil {
		r.register(NewTextToSpeechTool(cfg, r.deps.Speech))
	}
}

// RegisterTools installs capability tools constructed outside this package
// (A2A, browser use, computer use, MCP). The agent core consumes them through the
// agentdomain.Tool contract only. A tool that brings its own manifest also
// brings its policy.
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

// Manifest returns the named tool's manifest, including its configured
// require_approval: a registered tool answers for itself, an agent tool that
// is not enabled falls back to its embedded manifest, and any other tool,
// such as an MCP tool, gets the default policy.
func (r *Registry) Manifest(name string) agentdomain.ToolManifest {
	r.toolsMu.RLock()
	tool := r.tools[name]
	r.toolsMu.RUnlock()
	if manifestTool, ok := tool.(agentdomain.ManifestTool); ok {
		return manifestTool.Manifest()
	}
	return toolManifests.Manifest(name)
}

// builtinTool is an agent tool defined by an embedded manifest.
type builtinTool interface {
	agentdomain.Tool
	agentdomain.ManifestTool
}

// register adds a built-in tool under the name its manifest declares. Callers
// hold toolsMu or run before the registry is shared.
func (r *Registry) register(tool builtinTool) {
	r.tools[tool.Manifest().Name] = tool
}

// SetMemoryBackend wires the memory sync backend into the Memory tool so a
// write/delete pushes to the remote. The container calls this after
// constructing the shared backend. It re-registers the Memory tool so the
// backend takes effect. A nil backend (or the local no-op backend) means no
// remote sync.
func (r *Registry) SetMemoryBackend(backend memory.MemoryBackend) {
	r.memoryBackend = backend
	if r.config.Memory.Enabled {
		r.toolsMu.Lock()
		r.register(NewMemoryTool(r.config, backend, projects.Detect()))
		r.toolsMu.Unlock()
	}
}

// SetSkillsService registers the Skill tool over the skills port. The
// container calls it once skills are loaded, after the registry is built.
func (r *Registry) SetSkillsService(skills agentdomain.SkillsService) {
	r.toolsMu.Lock()
	defer r.toolsMu.Unlock()
	r.register(NewSkillTool(r.config, skills))
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
	return r.deps.BackgroundShells
}
