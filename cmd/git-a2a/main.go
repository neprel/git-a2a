package main

import (
	"os"

	"github.com/neprel/git-a2a/internal/cli"
)

func main() { os.Exit(cli.New(os.Stdout, os.Stderr).Run(os.Args[1:])) }
