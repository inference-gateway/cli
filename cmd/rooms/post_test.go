package rooms

import (
	"bytes"
	"testing"

	require "github.com/stretchr/testify/require"

	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

func TestPrintPosted(t *testing.T) {
	tests := []struct {
		name   string
		posted postedView
		want   string
	}{
		{
			name: "pinged and routed members with a notice",
			posted: postedView{
				Room: "release",
				Post: roomsdomain.Post{
					Mentions: []string{"tester"},
					Routes:   []roomsdomain.Route{{Member: "tester", Probability: 1}, {Member: "writer", Probability: 0.8}},
				},
				Notices: []string{"@reviewer is not in room release."},
			},
			want: "Posted to release.\nRouted to tester (pinged), writer (0.80).\n@reviewer is not in room release.\n",
		},
		{
			name:   "routed to nobody",
			posted: postedView{Room: "release", Notices: []string{"No member of room release was addressed. Mention one: @tester, @writer"}},
			want:   "Posted to release.\nRouted to nobody.\nNo member of room release was addressed. Mention one: @tester, @writer\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printPosted(&out, tt.posted)
			require.Equal(t, tt.want, out.String())
		})
	}
}
