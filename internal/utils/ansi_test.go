package utils

import "testing"

func TestStripANSI(t *testing.T) {
	got := StripANSI("\x1b[32mtask: [test] bun test\x1b[0m")
	want := "task: [test] bun test"
	if got != want {
		t.Errorf("StripANSI() = %q, want %q", got, want)
	}
}
