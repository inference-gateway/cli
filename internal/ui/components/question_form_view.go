package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// minQuestionOptionWidth is the floor for a truncated option row so a narrow
// terminal still shows a usable amount of each label/description.
const minQuestionOptionWidth = 20

// otherOptionLabel is the synthesized free-text choice appended to every
// question. It is not one of the model-provided options.
const otherOptionLabel = "Other (type your own)"

// QuestionFormView renders the interactive AskUserQuestion form as a bordered
// box floating above the input (mirroring ApprovalBoxView). It reads the
// in-progress answer state from the StateManager and renders the current
// question's options, a checkbox/radio marker per option, the synthesized
// "Other" free-text row, and a one-line key hint.
type QuestionFormView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	stateManager  domain.StateManager
}

func NewQuestionFormView(styleProvider *styles.Provider, stateManager domain.StateManager) *QuestionFormView {
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

func (qv *QuestionFormView) Render() string {
	if qv.stateManager == nil {
		return ""
	}
	state := qv.stateManager.GetUserQuestionUIState()
	if state == nil || len(state.Questions) == 0 || state.CurrentIndex >= len(state.Questions) {
		return ""
	}
	return qv.renderForm(state)
}

// renderForm frames the current question, its options, and a key hint in a
// bordered box. The accent border echoes the focused input box below it.
func (qv *QuestionFormView) renderForm(state *domain.UserQuestionUIState) string {
	accentColor := qv.styleProvider.GetThemeColor("accent")
	dimColor := qv.styleProvider.GetThemeColor("dim")

	question := state.Questions[state.CurrentIndex]

	header := fmt.Sprintf(" %s ", strings.TrimSpace(question.Header))
	progress := fmt.Sprintf("(%d/%d)", state.CurrentIndex+1, len(state.Questions))
	title := qv.styleProvider.RenderWithColorAndBold(header, accentColor) + " " +
		qv.styleProvider.RenderWithColor(progress, dimColor)

	parts := []string{
		title,
		formatting.TruncateText(question.Question, qv.textBudget()),
		"",
	}
	parts = append(parts, qv.renderOptions(state, question)...)
	parts = append(parts, "", qv.styleProvider.RenderWithColor(qv.hint(state, question), dimColor))

	content := strings.Join(parts, "\n")
	return qv.styleProvider.RenderBorderedBox(content, accentColor, 0, 1)
}

// renderOptions renders one row per option plus the synthesized "Other" row.
func (qv *QuestionFormView) renderOptions(state *domain.UserQuestionUIState, question domain.UserQuestion) []string {
	rows := make([]string, 0, len(question.Options)+1)
	for i, opt := range question.Options {
		selected := state.Selected[state.CurrentIndex][i]
		label := opt.Label
		if opt.Description != "" {
			label = fmt.Sprintf("%s - %s", opt.Label, opt.Description)
		}
		rows = append(rows, qv.renderRow(label, i == state.OptionCursor, selected, question.MultiSelect))
	}

	otherCursor := state.OptionCursor == state.OtherRowIndex()
	otherText := strings.TrimSpace(state.OtherText[state.CurrentIndex])
	otherActive := state.OtherActive[state.CurrentIndex]
	otherLabel := otherOptionLabel
	switch {
	case otherActive:
		otherLabel = fmt.Sprintf("Other: %s▏", otherText)
	case otherText != "":
		otherLabel = fmt.Sprintf("Other: %s", otherText)
	}
	rows = append(rows, qv.renderRow(otherLabel, otherCursor, otherText != "" || otherActive, question.MultiSelect))
	return rows
}

// renderRow renders a single option line: a cursor caret, a checkbox (multi) or
// radio (single) marker, and the label. The highlighted row gets a background.
func (qv *QuestionFormView) renderRow(label string, cursor, selected, multi bool) string {
	caret := "  "
	if cursor {
		caret = "▸ "
	}

	var marker string
	switch {
	case multi && selected:
		marker = "[x] "
	case multi:
		marker = "[ ] "
	case selected:
		marker = "(•) "
	default:
		marker = "( ) "
	}

	text := formatting.TruncateText(label, qv.textBudget()-len(caret)-len(marker))
	line := caret + marker + text

	if cursor {
		return qv.styleProvider.RenderStyledText(line, styles.StyleOptions{
			Background: qv.styleProvider.GetThemeColor("selection_bg"),
			Bold:       true,
		})
	}
	return line
}

// hint returns the dim key-help line, adapted to the current input mode.
func (qv *QuestionFormView) hint(state *domain.UserQuestionUIState, question domain.UserQuestion) string {
	if state.OtherActive[state.CurrentIndex] {
		return "type your answer · enter continue · esc back"
	}
	if question.MultiSelect {
		return "↑/↓ move · space toggle · enter continue · esc cancel"
	}
	return "↑/↓ select · enter confirm · esc cancel"
}

// textBudget is the display width available for a row after reserving room for
// the box border (2) and horizontal padding (2), plus slack from the right edge.
func (qv *QuestionFormView) textBudget() int {
	budget := qv.width - 6
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
