package main

import (
	"fmt"
	"os"
	_ "time/tzdata"

	root "github.com/inference-gateway/cli/cmd/root"
	accessibility "github.com/inference-gateway/cli/internal/computer/infrastructure/accessibility"
)

func main() {
	if accessibility.IsHelperProcess() {
		if err := accessibility.RunHelper(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	root.Execute()
}
