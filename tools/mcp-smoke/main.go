// Command mcp-smoke verifies MCP discovery through a released git-a2a binary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	binary := flag.String("binary", "git-a2a", "path to the git-a2a binary")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.Command(*binary, "mcp")
	client := mcp.NewClient(&mcp.Implementation{Name: "git-a2a-release-smoke", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to MCP server: %v\n", err)
		os.Exit(1)
	}
	defer session.Close()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "list MCP tools: %v\n", err)
			os.Exit(1)
		}
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"doctor", "explain", "fetch", "show", "status", "usage", "validate", "who"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		fmt.Fprintf(os.Stderr, "MCP tools/list = %v, want %v\n", names, want)
		os.Exit(1)
	}
	fmt.Printf("MCP tools/list: %d tools (%s)\n", len(names), strings.Join(names, ", "))
}
