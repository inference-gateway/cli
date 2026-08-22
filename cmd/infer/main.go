package main

import (
	_ "time/tzdata"

	root "github.com/inference-gateway/cli/cmd/root"
)

func main() {
	root.Execute()
}
