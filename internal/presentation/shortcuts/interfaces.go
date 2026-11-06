package shortcuts

import (
	"context"
)

// Shortcut interface represents a chat shortcut that can be executed
type Shortcut interface {
	GetName() string
	GetDescription() string
	GetUsage() string
	Execute(ctx context.Context, args []string) (ShortcutResult, error)
	CanExecute(args []string) bool
}

// Kinds of a registry entry, the badge a "/" menu shows next to it.
const (
	KindCommand  = "command"
	KindShortcut = "shortcut"
)

// Kind labels a registry entry: a YAML-defined custom shortcut is a "shortcut",
// every other registered entry is a built-in "command".
func Kind(shortcut Shortcut) string {
	if _, custom := shortcut.(*CustomShortcut); custom {
		return KindShortcut
	}
	return KindCommand
}

// ShortcutResult represents the result of a shortcut execution
type ShortcutResult struct {
	Output     string
	Success    bool
	SideEffect SideEffectType
	Data       any
}

// SideEffectType defines the types of side effects a shortcut can have
type SideEffectType int

const (
	SideEffectNone SideEffectType = iota
	SideEffectClearConversation
	SideEffectExit
	SideEffectSwitchModel
	SideEffectSwitchTheme
	SideEffectShowHelp
	SideEffectReloadConfig
	SideEffectSaveConversation
	SideEffectShowConversationSelection
	SideEffectStartNewConversation
	SideEffectShowA2ATaskManagement
	SideEffectSetInput
	SideEffectGenerateSnippet
	SideEffectCompactConversation
	SideEffectEmbedImages
	SideEffectSendMessageWithModel
	// SideEffectSendMessage submits Data (a string) as a regular chat message.
	SideEffectSendMessage
	SideEffectShowDiffViewer
	SideEffectShowExplorer
	SideEffectShowToolsList
	SideEffectShowAgents
	SideEffectShowWorkspaces
	SideEffectShowArtifacts
	// SideEffectTeleportSession moves the session, Data is a tui.TeleportRequestedEvent.
	SideEffectTeleportSession
)
