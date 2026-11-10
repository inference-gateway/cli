package directexec

import (
	"encoding/json"
	"testing"
)

func TestBashArgumentsKeepTheCommandAsTyped(t *testing.T) {
	commands := []string{
		`ls -la`,
		`echo "x" | sed 's/\./,/'`,
		`printf 'a\nb'`,
		"echo one\necho two",
		`grep -r "a\"b" C:\tmp`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var args map[string]string
			if err := json.Unmarshal([]byte(bashArguments(command)), &args); err != nil {
				t.Fatalf("arguments are not valid JSON: %v", err)
			}
			if args["command"] != command {
				t.Fatalf("command = %q, want %q", args["command"], command)
			}
		})
	}
}
