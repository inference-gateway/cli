package components

import (
	"slices"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

const (
	approveButton = "[ Approve ]"
	rejectButton  = "[ Reject ]"
	buttonGap     = "  "
)

// approvalButton is where one choice of an open approval sits in the room's
// content, so a click can tell which call and which decision it lands on.
type approvalButton struct {
	row, from, to int
	post          roomsdomain.Post
	decision      roomsdomain.Decision
}

// SetApprovalKeys names the keys that decide the newest open approval, shown
// next to its buttons.
func (v *RoomView) SetApprovalKeys(approve, reject string) {
	v.approvalKeys = approve + " approve · " + reject + " reject"
	v.refresh()
}

// NewestOpenApproval is the newest approval post of the room nobody decided
// yet, the one the keys decide.
func (v *RoomView) NewestOpenApproval() (roomsdomain.Post, bool) {
	open := roomsdomain.OpenApprovals(v.posts)
	if len(open) == 0 {
		return roomsdomain.Post{}, false
	}
	return open[len(open)-1], true
}

// DecisionAt is the approval and the decision of the button at column x of row
// y of the room view, as last rendered, and false when no button is there.
func (v *RoomView) DecisionAt(x, y int) (roomsdomain.Post, roomsdomain.Decision, bool) {
	bannerRows := 0
	if v.banner != "" {
		bannerRows = lipgloss.Height(v.banner)
	}
	if y < bannerRows || y-bannerRows >= v.viewport.Height() {
		return roomsdomain.Post{}, "", false
	}
	row := y - bannerRows + v.viewport.YOffset()
	for _, button := range v.buttons {
		if button.row == row && x >= button.from && x < button.to {
			return button.post, button.decision, true
		}
	}
	return roomsdomain.Post{}, "", false
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

// approvalButtons draws the choices of an open approval on content row row
// and records where they sit. The newest open one also names its keys.
func (v *RoomView) approvalButtons(post roomsdomain.Post, row int, newest bool) string {
	approveTo := lipgloss.Width(approveButton)
	rejectFrom := approveTo + lipgloss.Width(buttonGap)
	v.buttons = append(v.buttons,
		approvalButton{row: row, from: 0, to: approveTo, post: post, decision: roomsdomain.DecisionApprove},
		approvalButton{row: row, from: rejectFrom, to: rejectFrom + lipgloss.Width(rejectButton), post: post, decision: roomsdomain.DecisionReject},
	)
	line := v.styleProvider.RenderWithColorAndBold(approveButton, v.styleProvider.GetThemeColor("success")) + buttonGap +
		v.styleProvider.RenderWithColorAndBold(rejectButton, v.styleProvider.GetThemeColor("error"))
	if newest && v.approvalKeys != "" {
		line += buttonGap + v.styleProvider.RenderDimText(v.approvalKeys)
	}
	return line
}

// isOpenApproval reports whether post is one of the open approvals.
func isOpenApproval(open []roomsdomain.Post, post roomsdomain.Post) bool {
	return post.Kind == roomsdomain.PostKindApproval &&
		slices.ContainsFunc(open, func(pending roomsdomain.Post) bool { return pending.ID == post.ID })
}

// callSummary is a call's tool and its arguments, as a member asked to run it.
func callSummary(approval roomsdomain.Approval) string {
	return strings.TrimSpace(approval.Tool + " " + approval.Arguments)
}
