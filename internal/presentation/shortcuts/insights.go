package shortcuts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// Bounds on the digest: a machine with thousands of sessions must produce the
// same size prompt as one with ten.
const (
	maxInsightSessions   = 50
	maxDigestChars       = 12000
	maxErrorsPerTool     = 5
	maxIntentChars       = 200
	maxToolsPerSession   = 20
	insightsMaxTokens    = 2000
	insightsTimeout      = 120 * time.Second
	insightsAnalysisHead = "## Analysis"
)

// digitRun folds "exit status 2" and "exit status 127" into one recurring
// failure instead of two singletons.
var digitRun = regexp.MustCompile(`\d+`)

// InsightsGenerator distills past sessions into a markdown report: which
// workflows repeat often enough to deserve a skill, and which tool calls keep
// failing the same way. Counts and error strings are computed here; the model
// only interprets them, because a model asked to both count and interpret will
// confidently invent the counts.
type InsightsGenerator struct {
	client sdk.Client
	cfg    *config.Config
	store  storage.ConversationStorage
	models convdomain.ModelService
}

func NewInsightsGenerator(client sdk.Client, cfg *config.Config, store storage.ConversationStorage, models convdomain.ModelService) *InsightsGenerator {
	return &InsightsGenerator{client: client, cfg: cfg, store: store, models: models}
}

// Available reports whether the generator has everything it needs. The metadata
// registry builds shortcuts with nil dependencies and storage can be disabled,
// so both call sites check first.
func (g *InsightsGenerator) Available() bool {
	return g != nil && g.store != nil && g.client != nil
}

type sessionDigest struct {
	Title   string
	Project string
	When    time.Time
	Intent  string
	Tools   []string
}

// toolFailure is one tool's recurring errors, keyed by the normalized message.
type toolFailure struct {
	Tool   string
	Errors map[string]int
	Sample map[string]string // normalized key -> first raw error seen
}

// Generate reads the sessions, asks the model to interpret them, and writes the
// report to ~/.infer/insights.
func (g *InsightsGenerator) Generate(ctx context.Context, since time.Time) (markdown, path string, err error) {
	if !g.Available() {
		return "", "", fmt.Errorf("insights need conversation storage and a configured model")
	}

	sessions, failures, err := g.collect(ctx, since)
	if err != nil {
		return "", "", err
	}
	if len(sessions) == 0 {
		return "", "", fmt.Errorf("no saved sessions to analyze yet")
	}

	// Telemetry can be disabled or aged past retention_days; its absence costs
	// the reliability table, not the report.
	var tools []telemetry.ToolStat
	if stats, aggErr := telemetry.Aggregate(config.TelemetryDir(), since, ""); aggErr == nil && !stats.Empty {
		tools = stats.Tools
	}

	analysis, err := g.analyze(ctx, buildDigest(sessions, failures, tools))
	if err != nil {
		return "", "", err
	}

	markdown = renderReport(since, sessions, failures, tools, analysis)
	path, err = writeReport(markdown)
	if err != nil {
		return "", "", err
	}
	return markdown, path, nil
}

// collect walks the most recent sessions across every project.
func (g *InsightsGenerator) collect(ctx context.Context, since time.Time) ([]sessionDigest, []toolFailure, error) {
	summaries, err := g.store.ListConversations(ctx, "", maxInsightSessions, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("listing conversations: %w", err)
	}

	sessions := make([]sessionDigest, 0, len(summaries))
	byTool := map[string]*toolFailure{}

	for _, summary := range summaries {
		if !since.IsZero() && summary.UpdatedAt.Before(since) {
			continue
		}
		entries, meta, loadErr := g.store.LoadConversation(ctx, summary.ID)
		if loadErr != nil {
			continue // a single unreadable session must not sink the report
		}
		sessions = append(sessions, digestSession(summary, meta, entries))
		foldFailures(entries, byTool)
	}

	failures := make([]toolFailure, 0, len(byTool))
	for _, f := range byTool {
		failures = append(failures, *f)
	}
	sort.Slice(failures, func(i, j int) bool {
		li, lj := totalErrors(failures[i]), totalErrors(failures[j])
		if li != lj {
			return li > lj
		}
		return failures[i].Tool < failures[j].Tool
	})
	return sessions, failures, nil
}

func digestSession(summary convdomain.ConversationSummary, meta convdomain.ConversationMetadata, entries []convdomain.ConversationEntry) sessionDigest {
	d := sessionDigest{
		Title:   firstNonEmpty(summary.Title, meta.Title, "(untitled)"),
		Project: filepath.Base(firstNonEmpty(summary.Project, meta.Project)),
		When:    summary.UpdatedAt,
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		if d.Intent == "" && !entry.Hidden && entry.Message.Role == sdk.User {
			if content, err := entry.Message.Content.AsMessageContent0(); err == nil {
				d.Intent = oneLine(content, maxIntentChars)
			}
		}
		if entry.ToolExecution != nil && !seen[entry.ToolExecution.ToolName] && len(d.Tools) < maxToolsPerSession {
			seen[entry.ToolExecution.ToolName] = true
			d.Tools = append(d.Tools, entry.ToolExecution.ToolName)
		}
	}
	return d
}

// foldFailures counts each tool's failed executions by normalized error. A
// rejection is a user decision, not a failure - the distinction the telemetry
// recorder also makes.
func foldFailures(entries []convdomain.ConversationEntry, byTool map[string]*toolFailure) {
	for _, entry := range entries {
		exec := entry.ToolExecution
		if exec == nil || exec.Success || exec.Rejected || exec.Error == "" {
			continue
		}
		f, ok := byTool[exec.ToolName]
		if !ok {
			f = &toolFailure{Tool: exec.ToolName, Errors: map[string]int{}, Sample: map[string]string{}}
			byTool[exec.ToolName] = f
		}
		key := normalizeError(exec.Error)
		f.Errors[key]++
		if _, seen := f.Sample[key]; !seen {
			f.Sample[key] = oneLine(exec.Error, maxIntentChars)
		}
	}
}

func normalizeError(err string) string {
	return digitRun.ReplaceAllString(strings.ToLower(strings.TrimSpace(err)), "N")
}

func totalErrors(f toolFailure) int {
	n := 0
	for _, c := range f.Errors {
		n += c
	}
	return n
}

// buildDigest renders the prompt payload; pure, so the tests target it directly.
func buildDigest(sessions []sessionDigest, failures []toolFailure, tools []telemetry.ToolStat) string {
	var b strings.Builder

	b.WriteString("SESSIONS (most recent first)\n")
	for _, s := range sessions {
		fmt.Fprintf(&b, "- [%s] %s\n  intent: %s\n  tools: %s\n",
			s.Project, s.Title, firstNonEmpty(s.Intent, "(none recorded)"), strings.Join(s.Tools, ", "))
		if b.Len() > maxDigestChars {
			break
		}
	}

	if len(tools) > 0 {
		b.WriteString("\nTOOL CALL TOTALS (calls/failures/avg ms)\n")
		for _, t := range tools {
			fmt.Fprintf(&b, "- %s: %d/%d/%d\n", t.Name, t.Calls, t.Failures, t.AvgMs)
		}
	}

	if len(failures) > 0 {
		b.WriteString("\nFAILED TOOL CALLS (verbatim errors, most frequent first)\n")
		for _, f := range failures {
			for _, key := range topErrors(f) {
				fmt.Fprintf(&b, "- %s x%d: %s\n", f.Tool, f.Errors[key], f.Sample[key])
			}
		}
	}

	return truncate(b.String(), maxDigestChars)
}

// topErrors returns a tool's error keys, most frequent first, capped.
func topErrors(f toolFailure) []string {
	keys := make([]string, 0, len(f.Errors))
	for k := range f.Errors {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if f.Errors[a] != f.Errors[b] {
			return f.Errors[b] - f.Errors[a]
		}
		return strings.Compare(a, b)
	})
	return keys[:min(len(keys), maxErrorsPerTool)]
}

// insightsPrompt asks for interpretation only; the report already carries the
// numbers. The skill-name constraints mirror the skills loader's, so a
// suggestion can be pasted straight into ~/.infer/skills/<name>/SKILL.md.
const insightsPrompt = `You are reviewing a developer's past CLI agent sessions to tell them what to change.

Answer with GitHub-flavored markdown under exactly these two headings, nothing else:

### Repeatable workflows worth a skill
Workflows the user repeats across sessions that would be better as a reusable skill.
For each: a name matching ^[a-z0-9-]+$ (max 64 chars), a one-paragraph description
(max 1024 chars) written as instructions to an agent, and the sessions that evidence it.
If nothing repeats often enough to justify one, say so plainly instead of inventing one.

### Tool calls that keep failing
For each recurring failure: what is actually going wrong and the concrete fix
(a config change, a different tool, a changed argument). Quote the error verbatim.
Skip tools whose failures look incidental rather than systematic.

Do not restate the counts below as a table - they are already in the report.
Be specific and short. No preamble, no closing summary.

DATA
----
`

func (g *InsightsGenerator) analyze(ctx context.Context, digest string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, insightsTimeout)
	defer cancel()

	var model string
	if g.models != nil {
		model = g.models.GetCurrentModel()
	}
	return callLLM(ctx, g.client, model, insightsPrompt+digest, insightsMaxTokens)
}

// renderReport assembles the file: exact numbers from Go, interpretation from
// the model.
func renderReport(since time.Time, sessions []sessionDigest, failures []toolFailure, tools []telemetry.ToolStat, analysis string) string {
	var b strings.Builder

	window := "all time"
	if !since.IsZero() {
		window = "since " + since.Format("2006-01-02 15:04")
	}
	projects := map[string]bool{}
	for _, s := range sessions {
		projects[s.Project] = true
	}

	fmt.Fprintf(&b, "# Insights - %s\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "Window: %s - %d sessions across %d projects\n\n", window, len(sessions), len(projects))

	if len(tools) > 0 {
		b.WriteString("## Tool reliability\n\n")
		b.WriteString("| Tool | Calls | Failures | Fail% | Avg |\n")
		b.WriteString("|------|-------|----------|-------|-----|\n")
		for _, t := range tools {
			fmt.Fprintf(&b, "| %s | %d | %d | %s | %dms |\n",
				t.Name, t.Calls, t.Failures, formatFailRate(t.Calls, t.Failures), t.AvgMs)
		}
		b.WriteString("\n")
	}

	if len(failures) > 0 {
		b.WriteString("## Recurring errors\n\n")
		for _, f := range failures {
			for _, key := range topErrors(f) {
				fmt.Fprintf(&b, "- **%s** x%d - %s\n", f.Tool, f.Errors[key], f.Sample[key])
			}
		}
		b.WriteString("\n")
	}

	b.WriteString(insightsAnalysisHead + "\n\n")
	b.WriteString(strings.TrimSpace(analysis))
	b.WriteString("\n")
	return b.String()
}

// writeReport saves under ~/.infer/insights, a directory /reset leaves alone.
func writeReport(markdown string) (string, error) {
	dir := config.InsightsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405")+".md")
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// oneLine collapses a message to a single truncated line. A failed shell command
// carries its whole output in Error, which would otherwise break out of the
// markdown bullet it is rendered into.
func oneLine(s string, limit int) string {
	return truncate(strings.Join(strings.Fields(s), " "), limit)
}

// InsightsShortcut is the standalone entry point; /reset insights runs the same
// generator before offering to delete everything it read.
type InsightsShortcut struct {
	generator *InsightsGenerator
}

func NewInsightsShortcut(generator *InsightsGenerator) *InsightsShortcut {
	return &InsightsShortcut{generator: generator}
}

func (s *InsightsShortcut) GetName() string { return "insights" }
func (s *InsightsShortcut) GetDescription() string {
	return "Analyze past sessions for repeatable workflows and recurring tool failures"
}
func (s *InsightsShortcut) GetUsage() string              { return "/insights [since]" }
func (s *InsightsShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (s *InsightsShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	sinceStr := ""
	if len(args) == 1 {
		sinceStr = args[0]
	}
	since, err := telemetry.ParseSince(sinceStr)
	if err != nil {
		return ShortcutResult{Output: err.Error(), Success: false}, nil
	}

	markdown, path, err := s.generator.Generate(ctx, since)
	if err != nil {
		return ShortcutResult{Output: fmt.Sprintf("Failed to generate insights: %v", err), Success: false}, nil
	}

	return ShortcutResult{
		Output:  markdown + "\nSaved to " + path + "\n",
		Success: true,
	}, nil
}

func (s *InsightsShortcut) GetSubcommands() []Subcommand {
	return []Subcommand{
		{Name: "24h", Description: "Only sessions from the last 24 hours"},
		{Name: "7d", Description: "Only sessions from the last 7 days"},
		{Name: "30d", Description: "Only sessions from the last 30 days"},
	}
}
