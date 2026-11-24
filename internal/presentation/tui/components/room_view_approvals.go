package components

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// NewestOpenApproval is the newest approval post of the room nobody decided
// yet, the one the chat's approval window shows.
func (v *RoomView) NewestOpenApproval() (roomsdomain.Post, bool) {
	open := roomsdomain.OpenApprovals(v.posts)
	if len(open) == 0 {
		return roomsdomain.Post{}, false
	}
	return open[len(open)-1], true
}

// renderApproval draws the call a member waits on and the note it came with,
// such as the judge's reason and the member's justification.
func (v *RoomView) renderApproval(post roomsdomain.Post) string {
	width := max(v.width-roomViewGutter, 1)
	summary := truncateRunes("wants to run "+callSummary(*post.Approval), width)
	call := v.styleProvider.RenderWithColorAndBold(summary, v.styleProvider.GetThemeColor("warning"))
	if post.Text == "" {
		return call
	}
	note := lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color(v.styleProvider.GetThemeColor("dim"))).Render(post.Text)
	return call + "\n" + note
}

// callSummary is a call's tool and its arguments, as a member asked to run it.
func callSummary(approval roomsdomain.Approval) string {
	return strings.TrimSpace(approval.Tool + " " + approval.Arguments)
}
