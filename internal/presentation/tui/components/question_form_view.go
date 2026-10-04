package components

import (
	"fmt"
	"slices"
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// minQuestionOptionWidth is the floor for the form width so a narrow terminal
// still shows a usable amount of each label/description.
const minQuestionOptionWidth = 20

// otherOptionLabel is the synthesized free-text choice appended to every
// question. It is not one of the model-provided options.
const otherOptionLabel = "Other…"

// otherSentinel is the option value standing in for the synthesized "Other"
// row; real options are their index into question.Options.
const otherSentinel = -1

// QuestionFormView drives the interactive AskUserQuestion form behind an accent
// rail floating above the input. It owns the
// answer-in-progress state as one huh form per question; the state store only
// carries the questions, the overlay-active flag, and the response channel.
// The agent loop is blocked in the tool goroutine until the answers are sent
// on the channel (or it is closed, signalling cancellation).
type QuestionFormView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	stateManager  tui.UserQuestionPrompt

	// active is the state this form was built for; if the state store's
	// state no longer matches (cancelled externally), the form is stale.
	active  *tui.UserQuestionUIState
	idx     int
	form    *huh.Form
	answers []agentdomain.UserQuestionAnswer

	// Form-bound values, reset per question.
	single int
	multi  []int
	other  string
}

func NewQuestionFormView(styleProvider *styles.Provider, stateManager tui.UserQuestionPrompt) *QuestionFormView {
	return &QuestionFormView{
		width:         80,
		styleProvider: styleProvider,
		stateManager:  stateManager,
	}
}

func (qv *QuestionFormView) SetWidth(width int) {
	qv.width = width
}

func (qv *QuestionFormView) SetHeight(height int) {
	qv.height = height
}

// Begin starts a new form for the questions currently in the state store.
// Call it when a UserQuestionRequestedEvent has set up the state.
func (qv *QuestionFormView) Begin() tea.Cmd {
	state := qv.stateManager.GetUserQuestionUIState()
	if state == nil || len(state.Questions) == 0 {
		return nil
	}
	qv.active = state
	qv.idx = 0
	qv.answers = nil
	qv.buildForm()
	return qv.form.Init()
}

// Forward delegates a message to the active form and reacts to completion or
// abort. All messages (keys and huh's internal command messages) must be
// routed here while the form is up, or group/field navigation breaks.
func (qv *QuestionFormView) Forward(msg tea.Msg) tea.Cmd {
	state := qv.stateManager.GetUserQuestionUIState()
	if state == nil || state != qv.active || qv.form == nil {
		qv.reset()
		return nil
	}

	model, cmd := qv.form.Update(msg)
	if f, ok := model.(*huh.Form); ok {
		qv.form = f
	}

	switch qv.form.State {
	case huh.StateCompleted:
		return qv.advance()
	case huh.StateAborted:
		qv.stateManager.ClearUserQuestionUIState()
		qv.reset()
		return nil
	default:
		return cmd
	}
}

// advance records the completed question's answer and moves to the next one;
// after the last question it delivers the answers and clears the state.
func (qv *QuestionFormView) advance() tea.Cmd {
	qv.answers = append(qv.answers, qv.buildAnswer())
	qv.idx++
	if qv.idx < len(qv.active.Questions) {
		qv.buildForm()
		return qv.form.Init()
	}

	if qv.active.ResponseChan != nil {
		qv.active.ResponseChan <- qv.answers
	}
	qv.stateManager.ClearUserQuestionUIState()
	qv.reset()
	return nil
}

func (qv *QuestionFormView) reset() {
	qv.active = nil
	qv.form = nil
	qv.answers = nil
}

// buildForm builds one huh form for the current question: a select (or
// multi-select) over the options plus the synthesized "Other" row, and a
// conditional free-text group shown only when "Other" is chosen.
func (qv *QuestionFormView) buildForm() {
	question := qv.active.Questions[qv.idx]

	options := make([]huh.Option[int], 0, len(question.Options)+1)
	for i, opt := range question.Options {
		label := opt.Label
		if opt.Description != "" {
			label = opt.Label + "\n  " + qv.styleProvider.RenderDimText(opt.Description)
		}
		options = append(options, huh.NewOption(label, i))
	}
	options = append(options, huh.NewOption(otherOptionLabel, otherSentinel))

	qv.other = ""

	var choiceField huh.Field
	if question.MultiSelect {
		qv.multi = nil
		choiceField = huh.NewMultiSelect[int]().
			Options(options...).
			Validate(func(v []int) error {
				if len(v) == 0 {
					return fmt.Errorf("select at least one option")
				}
				return nil
			}).
			Value(&qv.multi)
	} else {
		qv.single = defaultUserQuestionOption(question)
		choiceField = huh.NewSelect[int]().
			Options(options...).
			Value(&qv.single)
	}

	otherInput := huh.NewInput().
		Title("Your answer").
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("answer is required")
			}
			return nil
		}).
		Value(&qv.other)

	keymap := huh.NewDefaultKeyMap()
	keymap.Quit = key.NewBinding(key.WithKeys("esc"))

	qv.form = huh.NewForm(
		huh.NewGroup(choiceField),
		huh.NewGroup(otherInput).WithHideFunc(func() bool { return !qv.otherChosen(question) }),
	).
		WithShowHelp(true).
		WithWidth(qv.textBudget()).
		WithKeyMap(keymap).
		WithTheme(questionHuhTheme(qv.styleProvider))
}

// otherChosen reports whether the synthesized "Other" row is currently chosen.
func (qv *QuestionFormView) otherChosen(question agentdomain.UserQuestion) bool {
	if question.MultiSelect {
		return slices.Contains(qv.multi, otherSentinel)
	}
	return qv.single == otherSentinel
}

// buildAnswer materializes the completed current question's answer.
func (qv *QuestionFormView) buildAnswer() agentdomain.UserQuestionAnswer {
	question := qv.active.Questions[qv.idx]
	answer := agentdomain.UserQuestionAnswer{
		Header:   question.Header,
		Question: question.Question,
	}
	if question.MultiSelect {
		for _, i := range qv.multi {
			if i >= 0 && i < len(question.Options) {
				answer.SelectedLabels = append(answer.SelectedLabels, question.Options[i].Label)
			}
		}
	} else if qv.single >= 0 && qv.single < len(question.Options) {
		answer.SelectedLabels = []string{question.Options[qv.single].Label}
	}
	if qv.otherChosen(question) {
		answer.OtherText = strings.TrimSpace(qv.other)
	}
	return answer
}

// defaultUserQuestionOption returns the option index to pre-select for a
// single-select question: the first option whose label is marked
// "(Recommended)" (case-insensitive), otherwise the first option.
func defaultUserQuestionOption(q agentdomain.UserQuestion) int {
	for i, opt := range q.Options {
		if strings.Contains(strings.ToLower(opt.Label), "(recommended)") {
			return i
		}
	}
	return 0
}

func (qv *QuestionFormView) Render() string {
	state := qv.stateManager.GetUserQuestionUIState()
	if state == nil || state != qv.active || qv.form == nil {
		return ""
	}
	accentColor := qv.styleProvider.GetThemeColor("accent")
	question := qv.active.Questions[qv.idx]
	parts := []string{
		qv.renderHeader(question.Header, accentColor),
		qv.styleProvider.RenderBold(formatting.WrapText(question.Question, qv.textBudget())),
		qv.form.View(),
	}
	return qv.styleProvider.RenderRail(strings.Join(parts, "\n\n"), accentColor)
}

// renderHeader renders the question's header chip, followed by the progress
// counter when the prompt holds more than one question.
func (qv *QuestionFormView) renderHeader(header, chipColor string) string {
	chip := qv.styleProvider.RenderChip(strings.TrimSpace(header), chipColor)
	total := len(qv.active.Questions)
	if total == 1 {
		return chip
	}
	return chip + " " + qv.styleProvider.RenderDimText(fmt.Sprintf("%d of %d", qv.idx+1, total))
}

// textBudget is the display width available beside the rail (2), plus slack
// from the right edge.
func (qv *QuestionFormView) textBudget() int {
	budget := qv.width - 4
	if budget < minQuestionOptionWidth {
		return minQuestionOptionWidth
	}
	return budget
}

func (qv *QuestionFormView) Init() tea.Cmd {
	return nil
}

func (qv *QuestionFormView) View() tea.View {
	return tea.NewView("")
}

func (qv *QuestionFormView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		qv.SetWidth(windowMsg.Width)
	}
	return qv, nil
}
