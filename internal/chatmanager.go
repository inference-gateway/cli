package internal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// ChatManagerModel manages the overall chat interface including file selection
type ChatManagerModel struct {
	chatInput    *ChatInputModel
	fileSelector *FileSelectorModel
	showingFiles bool
	width        int
	height       int
}

// NewChatManagerModel creates a new chat manager
func NewChatManagerModel() *ChatManagerModel {
	return &ChatManagerModel{
		chatInput:    NewChatInputModel(),
		showingFiles: false,
		width:        80,
		height:       20,
	}
}

func (m *ChatManagerModel) Init() tea.Cmd {
	return m.chatInput.Init()
}

func (m *ChatManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chatInput.Update(msg)
		if m.fileSelector != nil {
			m.fileSelector.Update(msg)
		}
		return m, nil

	case FileSelectionRequestMsg:
		files, err := m.scanProjectFiles()
		if err != nil {
			return m.chatInput.Update(SetStatusMsg{
				Message: fmt.Sprintf("❌ Error scanning files: %v", err),
				Spinner: false,
			})
		}

		if len(files) == 0 {
			return m.chatInput.Update(SetStatusMsg{
				Message: "❌ No files found in current directory",
				Spinner: false,
			})
		}

		maxFiles := 200
		if len(files) > maxFiles {
			files = files[:maxFiles]
		}

		m.fileSelector = NewFileSelectorModel(files)
		m.showingFiles = true
		return m, nil

	default:
		if m.showingFiles && m.fileSelector != nil {
			updatedSelector, cmd := m.fileSelector.Update(msg)
			m.fileSelector = updatedSelector.(*FileSelectorModel)

			if m.fileSelector.IsDone() {
				m.showingFiles = false
				if m.fileSelector.IsSelected() && !m.fileSelector.IsCancelled() {
					selectedFile := m.fileSelector.GetSelected()
					return m.chatInput.Update(FileSelectedMsg{FilePath: selectedFile})
				}

				m.fileSelector = nil
			}

			return m, cmd
		} else {
			updatedChat, cmd := m.chatInput.Update(msg)
			m.chatInput = updatedChat.(*ChatInputModel)
			return m, cmd
		}
	}
}

func (m *ChatManagerModel) View() string {
	if m.showingFiles && m.fileSelector != nil {
		return m.fileSelector.View()
	}
	return m.chatInput.View()
}

// Delegate methods to chat input
func (m *ChatManagerModel) HasInput() bool {
	return m.chatInput.HasInput()
}

func (m *ChatManagerModel) GetInput() string {
	return m.chatInput.GetInput()
}

func (m *ChatManagerModel) IsCancelled() bool {
	return m.chatInput.IsCancelled()
}

func (m *ChatManagerModel) ResetCancellation() {
	m.chatInput.ResetCancellation()
}

func (m *ChatManagerModel) IsQuitRequested() bool {
	return m.chatInput.IsQuitRequested()
}

func (m *ChatManagerModel) IsApprovalPending() bool {
	return m.chatInput.IsApprovalPending()
}

func (m *ChatManagerModel) GetApprovalResponse() int {
	return m.chatInput.GetApprovalResponse()
}

func (m *ChatManagerModel) ResetApproval() {
	m.chatInput.ResetApproval()
}

// GetChatInput returns the underlying chat input model
func (m *ChatManagerModel) GetChatInput() *ChatInputModel {
	return m.chatInput
}

// scanProjectFiles recursively scans the current directory for files
func (m *ChatManagerModel) scanProjectFiles() ([]string, error) {
	var files []string

	excludeDirs := map[string]bool{
		".git":         true,
		".github":      true,
		"node_modules": true,
		".infer":       true,
		"vendor":       true,
		".flox":        true,
		"dist":         true,
		"build":        true,
		"bin":          true,
		".vscode":      true,
		".idea":        true,
	}

	excludeExts := map[string]bool{
		".exe":   true,
		".bin":   true,
		".dll":   true,
		".so":    true,
		".dylib": true,
		".a":     true,
		".o":     true,
		".obj":   true,
		".pyc":   true,
		".class": true,
		".jar":   true,
		".war":   true,
		".zip":   true,
		".tar":   true,
		".gz":    true,
		".rar":   true,
		".7z":    true,
		".png":   true,
		".jpg":   true,
		".jpeg":  true,
		".gif":   true,
		".bmp":   true,
		".ico":   true,
		".svg":   true,
		".pdf":   true,
		".mov":   true,
		".mp4":   true,
		".avi":   true,
		".mp3":   true,
		".wav":   true,
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if excludeDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		if excludeExts[ext] {
			return nil
		}

		if info, err := d.Info(); err == nil && info.Size() > 1024*1024 {
			return nil
		}

		if d.Type().IsRegular() {
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	sort.Strings(files)

	return files, nil
}
