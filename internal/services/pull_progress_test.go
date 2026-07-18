package services

import "testing"

func TestPullProgress_Parse(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		wantDone  int
		wantTotal int
	}{
		{
			name: "docker fresh pull",
			lines: []string{
				"latest: Pulling from library/nginx",
				"e1caac4eb9d2: Pulling fs layer",
				"88a3f2f760d3: Pulling fs layer",
				"e1caac4eb9d2: Verifying Checksum",
				"e1caac4eb9d2: Download complete",
				"e1caac4eb9d2: Pull complete",
				"88a3f2f760d3: Already exists",
				"Digest: sha256:abcdef",
				"Status: Downloaded newer image for nginx:latest",
			},
			wantDone:  2,
			wantTotal: 2,
		},
		{
			name:      "docker hex-looking tag header is not a layer",
			lines:     []string{"deadbeef01: Pulling from repo/image"},
			wantDone:  0,
			wantTotal: 0,
		},
		{
			name: "podman pull",
			lines: []string{
				"Trying to pull docker.io/library/nginx:latest...",
				"Getting image source signatures",
				"Copying blob 1f7ce2fa46ab done",
				"Copying blob aabbccddeeff",
				"Copying config 605c77e624 done",
				"Writing manifest to image destination",
			},
			wantDone:  1,
			wantTotal: 2,
		},
		{
			name:      "garbage lines are ignored",
			lines:     []string{"random noise", "another: thing that is not hex"},
			wantDone:  0,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPullProgress()
			var done, total int
			for _, line := range tt.lines {
				done, total, _ = p.parse(line)
			}
			if done != tt.wantDone || total != tt.wantTotal {
				t.Errorf("got done=%d total=%d, want done=%d total=%d", done, total, tt.wantDone, tt.wantTotal)
			}
		})
	}
}

func TestPullProgress_ReportsOnlyOnChange(t *testing.T) {
	p := newPullProgress()

	if _, _, changed := p.parse("e1caac4eb9d2: Pulling fs layer"); !changed {
		t.Fatal("first layer registration should report a change")
	}
	if _, _, changed := p.parse("e1caac4eb9d2: Downloading"); changed {
		t.Error("a status update that does not affect counts should not report a change")
	}
	if _, _, changed := p.parse("e1caac4eb9d2: Pull complete"); !changed {
		t.Error("layer completion should report a change")
	}
	if _, _, changed := p.parse("e1caac4eb9d2: Pull complete"); changed {
		t.Error("repeated completion should not report a change")
	}
}
