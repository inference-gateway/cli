package sandboxexec

import (
	"slices"
	"testing"
)

func TestNewCommand_ParsesWritesBeforeTheProgram(t *testing.T) {
	command := NewCommand()
	if !command.Hidden {
		t.Fatal("the re-exec step must stay hidden")
	}

	if err := command.ParseFlags([]string{"--write", "/a", "--write", "/b", "--", "bash", "-c", "ls"}); err != nil {
		t.Fatal(err)
	}

	writes, err := command.Flags().GetStringArray("write")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/a", "/b"}; !slices.Equal(writes, want) {
		t.Errorf("--write = %q, want %q", writes, want)
	}
	if got, want := command.Flags().Args(), []string{"bash", "-c", "ls"}; !slices.Equal(got, want) {
		t.Errorf("program = %q, want %q", got, want)
	}
}
