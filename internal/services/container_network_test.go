package services

import (
	"reflect"
	"testing"
)

func TestIsAddressPoolExhausted(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "exact daemon message",
			msg:  "all predefined address pools have been fully subnetted",
			want: true,
		},
		{
			name: "wrapped in create error",
			msg:  "failed to create Docker network: exit status 1, output: Error response from daemon: all predefined address pools have been fully subnetted\n",
			want: true,
		},
		{
			name: "unrelated error",
			msg:  "failed to create Docker network: permission denied",
			want: false,
		},
		{
			name: "empty",
			msg:  "",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAddressPoolExhausted(tt.msg); got != tt.want {
				t.Errorf("isAddressPoolExhausted(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestNetworksToPrune(t *testing.T) {
	current := InferNetworkPrefix

	tests := []struct {
		name string
		all  []string
		want []string
	}{
		{
			name: "drops current shared network",
			all:  []string{InferNetworkPrefix, "infer-network-abc123", "infer-network-def456"},
			want: []string{"infer-network-abc123", "infer-network-def456"},
		},
		{
			name: "drops blanks and trims whitespace",
			all:  []string{"", "  infer-network-abc123  ", "\t"},
			want: []string{"infer-network-abc123"},
		},
		{
			name: "ignores foreign networks",
			all:  []string{"bridge", "host", "some-infer-network-lookalike", "infer-network-keep"},
			want: []string{"infer-network-keep"},
		},
		{
			name: "empty input",
			all:  []string{},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networksToPrune(tt.all, current)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("networksToPrune(%v) = %v, want %v", tt.all, got, tt.want)
			}
		})
	}
}

func TestInterpretNetworkRm(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantGone  bool
		wantInUse bool
	}{
		{name: "not found", output: "Error: network infer-network not found", wantGone: true},
		{name: "no such network", output: "Error response from daemon: No such network: infer-network", wantGone: true},
		{name: "in use", output: "Error response from daemon: network infer-network is in use", wantInUse: true},
		{name: "active endpoints", output: "error while removing network: network infer-network has active endpoints", wantInUse: true},
		{name: "unexpected", output: "Error response from daemon: permission denied", wantGone: false, wantInUse: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gone, inUse := interpretNetworkRm(tt.output)
			if gone != tt.wantGone || inUse != tt.wantInUse {
				t.Errorf("interpretNetworkRm(%q) = (gone=%v, inUse=%v), want (gone=%v, inUse=%v)", tt.output, gone, inUse, tt.wantGone, tt.wantInUse)
			}
		})
	}
}
