package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
	"strings"

	"github.com/neprel/git-a2a/internal/reference"
)

var referenceHeading = regexp.MustCompile(`(?m)^## ` + "`" + `([^` + "`" + `]+)` + "`" + `$`)

type explainOutput struct {
	Path     string `json:"path"`
	Markdown string `json:"markdown"`
}

func (a *App) explain(args []string) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOutput := fs.Bool("json", false, "JSON output")
	_ = fs.Bool("yes", false, "accept inputs (no-op)")
	ordered, orderErr := interspersedArgs(args, nil)
	if orderErr != nil || fs.Parse(ordered) != nil || fs.NArg() != 1 {
		fmt.Fprintln(a.Err, "explain: exactly one manifest field path is required")
		return 2
	}
	path, section, ok := referenceSection(fs.Arg(0))
	if !ok {
		fmt.Fprintf(a.Err, "explain: unknown manifest field %s\n", fs.Arg(0))
		return 2
	}
	if *jsonOutput {
		body, _ := json.MarshalIndent(explainOutput{Path: path, Markdown: section}, "", "  ")
		fmt.Fprintf(a.Out, "%s\n", body)
	} else {
		fmt.Fprint(a.Out, section)
		if !strings.HasSuffix(section, "\n") {
			fmt.Fprintln(a.Out)
		}
	}
	return 0
}

func referenceSection(wanted string) (string, string, bool) {
	matches := referenceHeading.FindAllStringSubmatchIndex(reference.Manifest, -1)
	normalized := strings.ReplaceAll(wanted, "[]", "")
	for i, match := range matches {
		path := reference.Manifest[match[2]:match[3]]
		if path != wanted && strings.ReplaceAll(path, "[]", "") != normalized {
			continue
		}
		start := match[0]
		end := len(reference.Manifest)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		return path, strings.TrimRight(reference.Manifest[start:end], "\n") + "\n", true
	}
	return "", "", false
}
