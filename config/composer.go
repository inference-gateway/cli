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

// reservedComposerTriggers are the sigils the composer parses itself. Mapping
// one in composer.yaml fails the load.
var reservedComposerTriggers = []string{"/", "!", "!!"}

// ComposerConfig holds the composer's trigger table per context and the status
// bar toggles that used to live in chat.status_bar. It is composer.yaml, the
// split file the TUI and the desktop read alike.
type ComposerConfig struct {
	Triggers  map[string]map[string][]string `yaml:"triggers" mapstructure:"triggers"`
	StatusBar StatusBarConfig                `yaml:"status_bar" mapstructure:"status_bar"`
}

// DefaultComposerConfig returns the default composer config. The chat context
// reproduces today's behavior (@ opens files with agents after them, # opens
// issues) and the room context puts agents first, since a room's @ reaches the
// room's members.
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

// TriggerSources returns the configured source order for a trigger in a
// context: the order is the menu's section order. An unset trigger returns
// nil, so nothing opens.
func (c ComposerConfig) TriggerSources(context, trigger string) []string {
	table, ok := c.Triggers[context]
	if !ok {
		return nil
	}
	return table[trigger]
}

// Validate checks the trigger table: contexts, triggers and sources must all
// be known, with /, ! and !! reserved. The key is named so the load error
// points at the line to fix.
func (c ComposerConfig) Validate() error {
	for _, context := range slices.Sorted(maps.Keys(c.Triggers)) {
		if context != ComposerContextChat && context != ComposerContextRoom {
			return fmt.Errorf("invalid %s triggers.%s: unknown context: contexts are %s and %s",
				ComposerFileName, context, ComposerContextChat, ComposerContextRoom)
		}
		for _, trigger := range slices.Sorted(maps.Keys(c.Triggers[context])) {
			if err := validateComposerTrigger(context, trigger); err != nil {
				return err
			}
			for _, source := range c.Triggers[context][trigger] {
				if source != ComposerSourceFiles && source != ComposerSourceAgents && source != ComposerSourceIssues {
					return fmt.Errorf("invalid %s triggers.%s.%q: unknown source %q: sources are %s, %s and %s",
						ComposerFileName, context, trigger, source,
						ComposerSourceFiles, ComposerSourceAgents, ComposerSourceIssues)
				}
			}
		}
	}
	if c.StatusBar.SubagentLingerSeconds < 0 {
		return fmt.Errorf("invalid %s status_bar.subagent_linger_seconds %d: must be >= 0",
			ComposerFileName, c.StatusBar.SubagentLingerSeconds)
	}
	return nil
}

func validateComposerTrigger(context, trigger string) error {
	if slices.Contains(reservedComposerTriggers, trigger) {
		return fmt.Errorf("invalid %s triggers.%s.%q: %s is reserved: %s always open the slash menu or run bash and tools directly and cannot be mapped",
			ComposerFileName, context, trigger, trigger, strings.Join(reservedComposerTriggers, ", "))
	}
	if !isTriggerCharacter(trigger) {
		return fmt.Errorf("invalid %s triggers.%s.%q: a trigger is one special character such as @, #, ^ or §",
			ComposerFileName, context, trigger)
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
