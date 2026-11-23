package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	configutils "github.com/inference-gateway/cli/config/utils"
)

const ComposerFileName = "composer.yaml"

// Composer contexts a trigger table can be scoped to. A plain chat session
// reads the chat table and a room session the room one.
const (
	ComposerContextChat = "chat"
	ComposerContextRoom = "room"
)

// The sources a trigger can open. An @ file row still inserts @<path>, an
// agent row @<agent> and an issue row #<number>: the trigger only picks which
// menu opens and in which order its sections are listed.
const (
	ComposerSourceFiles  = "files"
	ComposerSourceAgents = "agents"
	ComposerSourceIssues = "issues"
)

// The known contexts and sources, and the sigils the composer parses itself.
// Mapping a reserved sigil or naming anything unknown fails the load.
var (
	composerContexts         = []string{ComposerContextChat, ComposerContextRoom}
	composerSources          = []string{ComposerSourceFiles, ComposerSourceAgents, ComposerSourceIssues}
	reservedComposerTriggers = []string{"/", "!", "!!"}
)

// ComposerConfig holds the composer's trigger table per context and the status
// bar toggles that used to live in chat.status_bar. It is composer.yaml, the
// split file the TUI and the desktop read alike.
type ComposerConfig struct {
	Triggers  map[string]map[string][]string `yaml:"triggers" mapstructure:"triggers"`
	StatusBar StatusBarConfig                `yaml:"status_bar" mapstructure:"status_bar"`
}

// DefaultComposerConfig returns the default composer config. The chat context
// opens files with agents after them on @ and issues on #, and the room context
// puts agents first, since a room's @ reaches the room's members.
func DefaultComposerConfig() *ComposerConfig {
	return &ComposerConfig{
		Triggers: map[string]map[string][]string{
			ComposerContextChat: {
				"@": {ComposerSourceFiles, ComposerSourceAgents},
				"#": {ComposerSourceIssues},
			},
			ComposerContextRoom: {
				"@": {ComposerSourceAgents, ComposerSourceFiles},
				"#": {ComposerSourceIssues},
			},
		},
		StatusBar: GetDefaultStatusBarConfig(),
	}
}

// Validate checks the trigger table and the status bar's subagent linger.
func (c ComposerConfig) Validate() error {
	if err := ValidateComposerTriggers(c.Triggers); err != nil {
		return err
	}
	if c.StatusBar.SubagentLingerSeconds < 0 {
		return fmt.Errorf("invalid %s status_bar.subagent_linger_seconds %d: must be >= 0",
			ComposerFileName, c.StatusBar.SubagentLingerSeconds)
	}
	return nil
}

// ValidateComposerTriggers checks a trigger table: contexts, triggers and
// sources must all be known, with /, ! and !! reserved. The key is named so
// the load error points at the line to fix.
func ValidateComposerTriggers(triggers map[string]map[string][]string) error {
	for _, scope := range slices.Sorted(maps.Keys(triggers)) {
		if !slices.Contains(composerContexts, scope) {
			return fmt.Errorf("invalid %s triggers.%s: unknown context: contexts are %s",
				ComposerFileName, scope, strings.Join(composerContexts, ", "))
		}
		for _, trigger := range slices.Sorted(maps.Keys(triggers[scope])) {
			if err := validateComposerTrigger(scope, trigger); err != nil {
				return err
			}
			for _, source := range triggers[scope][trigger] {
				if !slices.Contains(composerSources, source) {
					return fmt.Errorf("invalid %s triggers.%s.%q: unknown source %q: sources are %s",
						ComposerFileName, scope, trigger, source, strings.Join(composerSources, ", "))
				}
			}
		}
	}
	return nil
}

func validateComposerTrigger(scope, trigger string) error {
	if slices.Contains(reservedComposerTriggers, trigger) {
		return fmt.Errorf("invalid %s triggers.%s.%q: %s is reserved: %s always open the slash menu or run bash and tools directly and cannot be mapped",
			ComposerFileName, scope, trigger, trigger, strings.Join(reservedComposerTriggers, ", "))
	}
	if !isTriggerCharacter(trigger) {
		return fmt.Errorf("invalid %s triggers.%s.%q: a trigger is one special character such as @, #, ^ or §",
			ComposerFileName, scope, trigger)
	}
	return nil
}

// isTriggerCharacter reports whether trigger is one punctuation or symbol
// character, ASCII or not. A letter, digit or space would open a menu on
// ordinary typing.
func isTriggerCharacter(trigger string) bool {
	r, size := utf8.DecodeRuneInString(trigger)
	if r == utf8.RuneError || size != len(trigger) {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// SaveComposer writes the composer configuration to disk, creating any
// missing parent directories.
func SaveComposer(path string, cfg *ComposerConfig) error {
	return configutils.SaveYAML(path, "composer", cfg)
}
