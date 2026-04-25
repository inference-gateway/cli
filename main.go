package main

import (
	_ "time/tzdata"

	"github.com/inference-gateway/cli/cmd"
)

func main() {
	cmd.Execute()
}
