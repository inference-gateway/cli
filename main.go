package main

import (
	_ "time/tzdata"

	cmd "github.com/inference-gateway/cli/cmd"
)

func main() {
	cmd.Execute()
}
