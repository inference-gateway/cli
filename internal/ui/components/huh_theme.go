package components

import (
	huh "charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// huhTheme maps the active style-provider colors onto a huh theme so huh
// forms match the rest of the TUI. Forms capture the theme at build time, so
// rebuild the form (the standard per-phase pattern) after a theme switch.
func huhTheme(p *styles.Provider) huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		if p == nil {
			return huh.ThemeBase(isDark)
		}
		accent := lipgloss.Color(p.GetThemeColor("accent"))
		errColor := lipgloss.Color(p.GetThemeColor("error"))
		dim := lipgloss.Color(p.GetThemeColor("dim"))
		success := lipgloss.Color(p.GetThemeColor("success"))

		t := huh.ThemeBase(isDark)

		t.Focused.Title = t.Focused.Title.Foreground(accent).Bold(true)
		t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accent).Bold(true)
		t.Focused.Description = t.Focused.Description.Foreground(dim)
		t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(errColor)
		t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(errColor)
		t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
		t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(accent)
		t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(accent)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(accent)
		t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(accent)
		t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(success)
		t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(accent).Bold(true)
		t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(dim)
		t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(accent)
		t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(accent)
		t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(dim)
		t.Focused.Directory = t.Focused.Directory.Foreground(accent)
		t.Focused.File = t.Focused.File.Foreground(lipgloss.NoColor{})

		t.Blurred = t.Focused
		t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Title = t.Blurred.Title.Bold(false).Foreground(dim)

		t.Help.ShortKey = t.Help.ShortKey.Foreground(dim)
		t.Help.ShortDesc = t.Help.ShortDesc.Foreground(dim)
		t.Help.FullKey = t.Help.FullKey.Foreground(dim)
		t.Help.FullDesc = t.Help.FullDesc.Foreground(dim)

		return t
	})
}

// approvalHuhTheme is huhTheme with the inline Select's focused option styled as
// a solid button (accent background, white text) so the approval CTA stands out.
func approvalHuhTheme(p *styles.Provider) huh.Theme {
	base := huhTheme(p)
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := base.Theme(isDark)
		if p == nil {
			return t
		}
		accent := lipgloss.Color(p.GetThemeColor("accent"))
		button := t.Focused.SelectedOption.
			Foreground(lipgloss.Color("15")).
			Background(accent).
			Bold(true).
			Padding(0, 1)
		t.Focused.SelectedOption = button
		t.Blurred.SelectedOption = button
		return t
	})
}
