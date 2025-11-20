package components

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	history "github.com/inference-gateway/cli/internal/ui/history"
	keys "github.com/inference-gateway/cli/internal/ui/keys"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	xclipboard "golang.design/x/clipboard"
)

// InputView handles user input with history and autocomplete
type InputView struct {
	text                string
	cursor              int
	placeholder         string
	width               int
	height              int
	modelService        domain.ModelService
	stateManager        domain.StateManager
	Autocomplete        shared.AutocompleteInterface
	historyManager      *history.HistoryManager
	isTextSelectionMode bool
	themeService        domain.ThemeService
	styleProvider       *styles.Provider
	imageAttachments    []domain.ImageAttachment // Pending image attachments
}

func NewInputView(modelService domain.ModelService) *InputView {
	return NewInputViewWithConfigDir(modelService, "")
}

func NewInputViewWithConfigDir(modelService domain.ModelService, configDir string) *InputView {
	if configDir == "" {
		configDir = ".infer"
	}

	historyManager, err := history.NewHistoryManagerWithDir(5, configDir)
	if err != nil {
		historyManager = history.NewMemoryOnlyHistoryManager(5)
	}

	return &InputView{
		text:                "",
		cursor:              0,
		placeholder:         "Type your message... (Press Enter to send, Alt+Enter or Ctrl+J for newline, ? for help)",
		width:               80,
		height:              5,
		modelService:        modelService,
		Autocomplete:        nil,
		historyManager:      historyManager,
		isTextSelectionMode: false,
		themeService:        nil,
		imageAttachments:    []domain.ImageAttachment{},
	}
}

// SetThemeService sets the theme service for this input view
func (iv *InputView) SetThemeService(themeService domain.ThemeService) {
	iv.themeService = themeService
	iv.styleProvider = styles.NewProvider(themeService)
}

// SetStateManager sets the state manager for this input view
func (iv *InputView) SetStateManager(stateManager domain.StateManager) {
	iv.stateManager = stateManager
}

func (iv *InputView) GetInput() string {
	return iv.text
}

func (iv *InputView) ClearInput() {
	iv.text = ""
	iv.cursor = 0
	iv.imageAttachments = []domain.ImageAttachment{}
	iv.historyManager.ResetNavigation()
	if iv.Autocomplete != nil {
		iv.Autocomplete.Hide()
	}
}

func (iv *InputView) SetPlaceholder(text string) {
	iv.placeholder = text
}

func (iv *InputView) GetCursor() int {
	return iv.cursor
}

func (iv *InputView) SetCursor(position int) {
	if position >= 0 && position <= len(iv.text) {
		iv.cursor = position
	}
}

func (iv *InputView) SetText(text string) {
	iv.text = text
}

func (iv *InputView) SetWidth(width int) {
	iv.width = width
	if iv.Autocomplete != nil {
		iv.Autocomplete.SetWidth(width)
	}
}

func (iv *InputView) SetHeight(height int) {
	iv.height = height
}

func (iv *InputView) Render() string {
	isToolsMode := strings.HasPrefix(iv.text, "!!")
	isBashMode := strings.HasPrefix(iv.text, "!") && !isToolsMode
	displayText := iv.renderDisplayText()

	inputContent := fmt.Sprintf("> %s", displayText)

	focused := isBashMode || isToolsMode
	borderedInput := iv.styleProvider.RenderInputField(inputContent, iv.width-4, focused)

	components := []string{borderedInput}

	components = iv.addModeIndicatorBelowInput(components, isBashMode, isToolsMode)
	components = iv.addAutocomplete(components)
	components = iv.addModelDisplayWithMode(components, isBashMode, isToolsMode)

	return iv.styleProvider.JoinVertical(components...)
}

func (iv *InputView) renderDisplayText() string {
	if iv.text == "" {
		return iv.renderPlaceholder()
	}
	return iv.renderTextWithCursor()
}

func (iv *InputView) renderPlaceholder() string {
	return iv.styleProvider.RenderInputPlaceholder(iv.placeholder)
}

func (iv *InputView) renderTextWithCursor() string {
	before := iv.text[:iv.cursor]
	after := iv.text[iv.cursor:]
	availableWidth := iv.width - 8

	if availableWidth > 0 {
		return iv.renderWrappedText(before, after, availableWidth)
	}
	return iv.renderUnwrappedText(before, after)
}

func (iv *InputView) renderWrappedText(before, after string, availableWidth int) string {
	wrappedBefore := iv.preserveTrailingSpaces(before, availableWidth)
	wrappedAfter := shared.WrapText(after, availableWidth)
	return iv.buildTextWithCursor(wrappedBefore, wrappedAfter)
}

func (iv *InputView) renderUnwrappedText(before, after string) string {
	return iv.buildTextWithCursor(before, after)
}

func (iv *InputView) buildTextWithCursor(before, after string) string {
	if len(after) == 0 {
		cursorChar := iv.createCursorChar(" ")
		return fmt.Sprintf("%s%s", before, cursorChar)
	}

	cursorChar := iv.createCursorChar(string(after[0]))
	restAfter := ""
	if len(after) > 1 {
		restAfter = after[1:]
	}
	return fmt.Sprintf("%s%s%s", before, cursorChar, restAfter)
}

func (iv *InputView) createCursorChar(char string) string {
	return iv.styleProvider.RenderTextSelectionCursor(char)
}

// getAgentModeIndicator returns a compact mode indicator for display on the right side
func (iv *InputView) getAgentModeIndicator() string {
	if iv.stateManager == nil {
		return ""
	}

	agentMode := iv.stateManager.GetAgentMode()
	if agentMode == domain.AgentModeStandard {
		return ""
	}

	var modeText string
	switch agentMode {
	case domain.AgentModePlan:
		modeText = "▶ PLAN"
	case domain.AgentModeAutoAccept:
		modeText = "▸ AUTO"
	}

	return iv.styleProvider.RenderStyledText(
		modeText,
		styles.StyleOptions{
			Foreground: iv.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		},
	)
}

// addModeIndicatorBelowInput adds mode indicators below the input field (for bash/tools modes)
func (iv *InputView) addModeIndicatorBelowInput(components []string, isBashMode bool, isToolsMode bool) []string {
	if iv.height >= 2 {
		if iv.isTextSelectionMode {
			indicator := iv.styleProvider.RenderStyledText(
				"TEXT SELECTION MODE - Use vim keys to navigate and select text (Escape to exit)",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("accent"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		} else if isBashMode {
			indicator := iv.styleProvider.RenderStyledText(
				"BASH MODE - Command will be executed directly",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("status"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		} else if isToolsMode {
			indicator := iv.styleProvider.RenderStyledText(
				"TOOLS MODE - !!ToolName(arg=\"value\") - Tab for autocomplete",
				styles.StyleOptions{
					Foreground: iv.styleProvider.GetThemeColor("accent"),
					Bold:       true,
					Width:      iv.width,
				},
			)
			components = append(components, indicator)
		}
	}
	return components
}

func (iv *InputView) addAutocomplete(components []string) []string {
	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() && iv.height >= 3 {
		autocompleteContent := iv.Autocomplete.Render()
		if autocompleteContent != "" {
			components = append(components, autocompleteContent)
		}
	}
	return components
}

func (iv *InputView) addModelDisplayWithMode(components []string, isBashMode bool, isToolsMode bool) []string {
	if !iv.shouldShowModelDisplay(isBashMode, isToolsMode) {
		return components
	}

	currentModel := iv.modelService.GetCurrentModel()
	displayText := iv.buildModelDisplayText(currentModel)
	modeIndicator := iv.getAgentModeIndicator()

	if modeIndicator != "" {
		return iv.addModelWithModeIndicator(components, displayText, modeIndicator)
	}

	return iv.addModelOnly(components, displayText)
}

func (iv *InputView) shouldShowModelDisplay(isBashMode bool, isToolsMode bool) bool {
	if iv.modelService == nil {
		return false
	}

	currentModel := iv.modelService.GetCurrentModel()
	return currentModel != "" && iv.height >= 2 && !isBashMode && !isToolsMode
}

func (iv *InputView) buildModelDisplayText(currentModel string) string {
	displayText := fmt.Sprintf("  Model: %s", currentModel)

	if iv.themeService != nil {
		currentTheme := iv.themeService.GetCurrentThemeName()
		displayText = fmt.Sprintf("  Model: %s • Theme: %s", currentModel, currentTheme)
	}

	return displayText
}

func (iv *InputView) addModelWithModeIndicator(components []string, displayText string, modeIndicator string) []string {
	modelText := iv.styleProvider.RenderStyledText(displayText, styles.StyleOptions{
		Foreground: iv.styleProvider.GetThemeColor("dim"),
	})

	combinedLine := iv.buildCombinedLine(modelText, modeIndicator)
	return append(components, combinedLine)
}

func (iv *InputView) buildCombinedLine(modelText string, modeIndicator string) string {
	inputRightEdge := iv.width - 4
	modelWidth := iv.styleProvider.GetWidth(modelText)
	modeWidth := iv.styleProvider.GetWidth(modeIndicator)
	availableWidth := inputRightEdge - modelWidth - modeWidth

	if availableWidth > 0 {
		return modelText + strings.Repeat(" ", availableWidth) + modeIndicator
	}
	return modelText + " " + modeIndicator
}

func (iv *InputView) addModelOnly(components []string, displayText string) []string {
	modelDisplay := iv.styleProvider.RenderStyledText(displayText, styles.StyleOptions{
		Foreground: iv.styleProvider.GetThemeColor("dim"),
		Width:      iv.width,
	})
	return append(components, modelDisplay)
}

// NavigateHistoryUp moves up in history (to older messages) - public method for interface
func (iv *InputView) NavigateHistoryUp() {
	iv.navigateHistoryUp()
}

// NavigateHistoryDown moves down in history (to newer messages) - public method for interface
func (iv *InputView) NavigateHistoryDown() {
	iv.navigateHistoryDown()
}

// navigateHistoryUp moves up in history (to older messages)
func (iv *InputView) navigateHistoryUp() {
	newText := iv.historyManager.NavigateUp(iv.text)
	iv.text = newText
	iv.cursor = len(iv.text)
}

// navigateHistoryDown moves down in history (to newer messages)
func (iv *InputView) navigateHistoryDown() {
	newText := iv.historyManager.NavigateDown(iv.text)
	iv.text = newText
	iv.cursor = len(iv.text)
}

// AddToHistory adds the current input to the history
func (iv *InputView) AddToHistory(text string) error {
	if text == "" {
		return nil
	}
	return iv.historyManager.AddToHistory(text)
}

// Bubble Tea interface
func (iv *InputView) Init() tea.Cmd { return nil }

func (iv *InputView) View() string { return iv.Render() }

func (iv *InputView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if windowMsg, ok := msg.(tea.WindowSizeMsg); ok {
		iv.SetWidth(windowMsg.Width)
	}

	switch msg := msg.(type) {
	case domain.ClearInputEvent:
		iv.ClearInput()
		return iv, nil
	case domain.SetInputEvent:
		iv.SetText(msg.Text)
		iv.SetCursor(len(msg.Text))
		return iv, nil
	}
	return iv, nil
}

func (iv *InputView) HandleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := key.String()

	if keyStr == "ctrl+v" {
		return iv.handlePaste()
	}

	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		if handled, completion := iv.Autocomplete.HandleKey(key); handled {
			return iv.handleAutocomplete(completion)
		}
	}

	if iv.Autocomplete == nil || !iv.Autocomplete.IsVisible() {
		switch keyStr {
		case "up":
			iv.navigateHistoryUp()
			if iv.Autocomplete != nil {
				iv.Autocomplete.Update(iv.text, iv.cursor)
			}
			return iv, nil
		case "down":
			iv.navigateHistoryDown()
			if iv.Autocomplete != nil {
				iv.Autocomplete.Update(iv.text, iv.cursor)
			}
			return iv, nil
		}
	}

	if keyStr != "up" && keyStr != "down" && keyStr != "left" && keyStr != "right" &&
		keyStr != "ctrl+a" && keyStr != "ctrl+e" && keyStr != "home" && keyStr != "end" {
		iv.historyManager.ResetNavigation()
	}

	if iv.Autocomplete != nil {
		iv.Autocomplete.Update(iv.text, iv.cursor)
	}
	return iv, nil
}

func (iv *InputView) handleAutocomplete(completion string) (tea.Model, tea.Cmd) {
	if completion != "" {
		iv.text = completion
		iv.setCursorPosition(completion)
		if iv.Autocomplete != nil {
			iv.Autocomplete.Hide()
		}
		return iv, nil
	}
	return iv, nil
}

// setCursorPosition sets the appropriate cursor position based on completion content
func (iv *InputView) setCursorPosition(completion string) {
	if strings.Contains(completion, `=""`) {
		if idx := strings.Index(completion, `=""`); idx != -1 {
			iv.cursor = idx + 2
		} else {
			iv.cursor = len(completion)
		}
	} else {
		iv.cursor = len(completion)
	}
}

func (iv *InputView) CanHandle(key tea.KeyMsg) bool {
	return keys.CanInputHandle(key)
}

func (iv *InputView) preserveTrailingSpaces(text string, availableWidth int) string {
	wrappedText := shared.WrapText(text, availableWidth)

	trailingSpaces := 0
	for i := len(text) - 1; i >= 0 && text[i] == ' '; i-- {
		trailingSpaces++
	}

	wrappedTrailingSpaces := 0
	for i := len(wrappedText) - 1; i >= 0 && wrappedText[i] == ' '; i-- {
		wrappedTrailingSpaces++
	}

	if trailingSpaces > wrappedTrailingSpaces {
		spacesToAdd := trailingSpaces - wrappedTrailingSpaces
		wrappedText += strings.Repeat(" ", spacesToAdd)
	}

	return wrappedText
}

// IsAutocompleteVisible returns whether autocomplete is currently visible
func (iv *InputView) IsAutocompleteVisible() bool {
	return iv.Autocomplete != nil && iv.Autocomplete.IsVisible()
}

// TryHandleAutocomplete attempts to handle autocomplete key input
func (iv *InputView) TryHandleAutocomplete(key tea.KeyMsg) (handled bool, completion string) {
	if iv.Autocomplete != nil && iv.Autocomplete.IsVisible() {
		return iv.Autocomplete.HandleKey(key)
	}
	return false, ""
}

// handlePaste handles clipboard paste operations
func (iv *InputView) handlePaste() (tea.Model, tea.Cmd) {
	// First, try to read image data from clipboard
	imageData := xclipboard.Read(xclipboard.FmtImage)
	if len(imageData) > 0 {
		logger.Debug("[InputView] clipboard contains binary image data", "size", len(imageData))

		// Try to decode and attach the image
		imageAttachment, err := loadImageFromBinary(imageData)
		if err == nil {
			logger.Debug("[InputView] successfully loaded image from clipboard", "mime_type", imageAttachment.MimeType)
			iv.AddImageAttachment(*imageAttachment)
			logger.Debug("[InputView] added binary image attachment to input view")
			return iv, nil
		}
		logger.Debug("[InputView] failed to load binary image", "error", err)
	} else {
		logger.Debug("[InputView] no binary image data in clipboard")
	}

	// Fall back to reading text from clipboard
	clipboardText := string(xclipboard.Read(xclipboard.FmtText))

	logger.Debug("[InputView] clipboard text content", "text", clipboardText, "length", len(clipboardText))

	if clipboardText == "" {
		logger.Debug("[InputView] clipboard text is empty")
		return iv, nil
	}

	cleanText := strings.ReplaceAll(clipboardText, "\r\n", "\n")
	cleanText = strings.ReplaceAll(cleanText, "\r", "\n")
	cleanText = strings.TrimSpace(cleanText)

	logger.Debug("[InputView] cleaned clipboard text", "text", cleanText, "length", len(cleanText))

	if cleanText == "" {
		logger.Debug("[InputView] cleaned text is empty")
		return iv, nil
	}

	// Check if clipboard contains a file path to an image
	isImage := isImageFilePath(cleanText)
	logger.Debug("[InputView] checking if clipboard contains image path", "path", cleanText, "is_image", isImage)

	if isImage {
		// Try to load the image from file
		imageAttachment, err := loadImageFromFile(cleanText)
		if err == nil {
			// Successfully loaded image - add as attachment
			logger.Debug("[InputView] successfully loaded image from file", "filename", imageAttachment.Filename, "mime_type", imageAttachment.MimeType)
			iv.AddImageAttachment(*imageAttachment)
			logger.Debug("[InputView] added file image attachment to input view")
			return iv, nil
		}
		logger.Debug("[InputView] failed to load image from file", "error", err)
		// If loading failed, fall through to treat as text
	}

	// Treat as text and paste it
	logger.Debug("[InputView] treating clipboard content as text paste")
	newText := iv.text[:iv.cursor] + cleanText + iv.text[iv.cursor:]
	newCursor := iv.cursor + len(cleanText)

	iv.text = newText
	iv.cursor = newCursor

	return iv, nil
}

func (iv *InputView) SetTextSelectionMode(enabled bool) {
	iv.isTextSelectionMode = enabled
}

func (iv *InputView) IsTextSelectionMode() bool {
	return iv.isTextSelectionMode
}

// AddImageAttachment adds an image attachment to the pending list
func (iv *InputView) AddImageAttachment(image domain.ImageAttachment) {
	// Assign display name based on current count
	image.DisplayName = fmt.Sprintf("Image #%d", len(iv.imageAttachments)+1)
	iv.imageAttachments = append(iv.imageAttachments, image)

	// Insert image token into text at cursor position
	imageToken := fmt.Sprintf("[%s]", image.DisplayName)
	iv.text = iv.text[:iv.cursor] + imageToken + iv.text[iv.cursor:]
	iv.cursor += len(imageToken)
}

// GetImageAttachments returns the list of pending image attachments
func (iv *InputView) GetImageAttachments() []domain.ImageAttachment {
	return iv.imageAttachments
}

// ClearImageAttachments clears all pending image attachments
func (iv *InputView) ClearImageAttachments() {
	iv.imageAttachments = []domain.ImageAttachment{}
}

// isImageFilePath checks if a file path is a supported image format
func isImageFilePath(filePath string) bool {
	// Get file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	supportedExts := []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}

	for _, supportedExt := range supportedExts {
		if ext == supportedExt {
			return true
		}
	}

	return false
}

// loadImageFromFile reads an image from a file path and returns it as a base64 attachment
func loadImageFromFile(filePath string) (*domain.ImageAttachment, error) {
	// Read image file
	imageData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	// Detect image format
	_, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to detect image format: %w", err)
	}

	// Convert to base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	// Determine MIME type
	mimeType := fmt.Sprintf("image/%s", format)

	return &domain.ImageAttachment{
		Data:     base64Data,
		MimeType: mimeType,
		Filename: filePath,
	}, nil
}

// loadImageFromBinary reads an image from binary data and returns it as a base64 attachment
func loadImageFromBinary(imageData []byte) (*domain.ImageAttachment, error) {
	// Detect image format
	_, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to detect image format: %w", err)
	}

	// Convert to base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	// Determine MIME type
	mimeType := fmt.Sprintf("image/%s", format)

	return &domain.ImageAttachment{
		Data:     base64Data,
		MimeType: mimeType,
		Filename: "clipboard-image.png", // Generic filename for clipboard images
	}, nil
}
