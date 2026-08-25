package main

import (
	"os"

	"github.com/neprel/git-a2a/internal/cli"
)

func main() {
	app := cli.New(os.Stdout, os.Stderr)
	app.In = os.Stdin
	os.Exit(app.Run(os.Args[1:]))
}
