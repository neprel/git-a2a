package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolDiscoveryAndRoundTrips(t *testing.T) {
	root := mcpFixture(t)
	app := New(os.Stdout, os.Stderr)
	app.Root = root
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	readSession := connectMCP(t, ctx, app.newMCPServer(false))
	defer readSession.Close()
	readTools := listMCPTools(t, ctx, readSession)
	if got, want := toolNames(readTools), []string{"doctor", "explain", "show", "status", "usage", "validate", "who"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("read tools = %v, want %v", got, want)
	}
	for name, arguments := range map[string]map[string]any{
		"who": {}, "show": {}, "status": {"offline": true}, "validate": {}, "doctor": {},
		"explain": {"path": "module.id"}, "usage": {},
	} {
		result := callMCPTool(t, ctx, readSession, name, arguments)
		if result.IsError {
			t.Errorf("%s unexpectedly failed: %v", name, result.Content)
		}
		assertMCPExitCode(t, name, result, 0)
	}
	for _, uri := range []string{"a2amodule://manifest", "a2amodule://lock", "a2amodule://roster", "a2amodule://reference"} {
		result, err := readSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Errorf("read %s: %v", uri, err)
			continue
		}
		if len(result.Contents) != 1 || result.Contents[0].Text == "" {
			t.Errorf("read %s returned no text", uri)
		}
	}

	writeSession := connectMCP(t, ctx, app.newMCPServer(true))
	defer writeSession.Close()
	writeTools := listMCPTools(t, ctx, writeSession)
	if len(writeTools) != 13 {
		t.Fatalf("write-enabled tool count = %d", len(writeTools))
	}
	if os.Getenv("GITA2A_UPDATE_GOLDEN") == "1" {
		body, _ := json.MarshalIndent(writeTools, "", "  ")
		if err := os.WriteFile(filepath.Join("testdata", "mcp-tools.golden.json"), append(body, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filepath.Join("testdata", "mcp-tools.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.MarshalIndent(writeTools, "", "  ")
	if string(append(got, '\n')) != string(want) {
		t.Fatal("tools/list differs from testdata/mcp-tools.golden.json; review schemas and regenerate intentionally")
	}

	writeCalls := map[string]map[string]any{
		"add":     {},
		"update":  {},
		"set":     {},
		"wire":    {},
		"sync":    {"check": true},
		"contact": {"id": "unknown", "intent": "question", "message": "Question"},
	}
	for name, arguments := range writeCalls {
		result := callMCPTool(t, ctx, writeSession, name, arguments)
		if result == nil {
			t.Errorf("%s returned nil", name)
		}
	}
}

func TestMCPCommandTransport(t *testing.T) {
	if os.Getenv("GITA2A_MCP_HELPER") == "1" {
		app := New(os.Stdout, os.Stderr)
		app.Root = os.Getenv("GITA2A_MCP_ROOT")
		os.Exit(app.Run([]string{"mcp"}))
	}
	root := mcpFixtureWithID(t, "command-one")
	secondRoot := mcpFixtureWithID(t, "command-two")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	firstSession := connectMCPCommand(t, ctx, root)
	defer firstSession.Close()
	secondSession := connectMCPCommand(t, ctx, secondRoot)
	defer secondSession.Close()
	assertMCPDataContains(t, "first command transport", callMCPTool(t, ctx, firstSession, "show", map[string]any{}), "command-one")
	assertMCPDataContains(t, "second command transport", callMCPTool(t, ctx, secondSession, "show", map[string]any{}), "command-two")
}

func connectMCPCommand(t *testing.T, ctx context.Context, root string) *mcp.ClientSession {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=TestMCPCommandTransport")
	command.Env = append(os.Environ(), "GITA2A_MCP_HELPER=1", "GITA2A_MCP_ROOT="+root)
	client := mcp.NewClient(&mcp.Implementation{Name: "git-a2a-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestMCPRepositoryRootSwitchAndConcurrentInstances(t *testing.T) {
	first := mcpFixtureWithID(t, "consumer-one")
	second := mcpFixtureWithID(t, "consumer-two")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	firstApp := New(os.Stdout, os.Stderr)
	firstApp.Root = first
	firstSession := connectMCP(t, ctx, firstApp.newMCPServer(false))
	defer firstSession.Close()
	secondApp := New(os.Stdout, os.Stderr)
	secondApp.Root = second
	secondSession := connectMCP(t, ctx, secondApp.newMCPServer(false))
	defer secondSession.Close()

	assertMCPDataContains(t, "first instance", callMCPTool(t, ctx, firstSession, "show", map[string]any{}), "consumer-one")
	assertMCPDataContains(t, "second instance", callMCPTool(t, ctx, secondSession, "show", map[string]any{}), "consumer-two")
	assertMCPDataContains(t, "root switch", callMCPTool(t, ctx, firstSession, "show", map[string]any{"root": second}), "consumer-two")
}

func mcpFixture(t *testing.T) string {
	return mcpFixtureWithID(t, "consumer-app")
}

func mcpFixtureWithID(t *testing.T, id string) string {
	t.Helper()
	root := t.TempDir()
	manifest := `schema: 1
module:
  id: ` + id + `
agents:
  - name: acme-app-cli
    role: owner
    contacts:
      - intents: [question]
        kind: url
        url: https://example.com/contact
policy:
  intents:
    question: owner
`
	if err := os.WriteFile(filepath.Join(root, "a2amodule.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a2amodule.lock"), []byte("schema: 1\ndependencies: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func connectMCP(t *testing.T, ctx context.Context, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "git-a2a-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func listMCPTools(t *testing.T, ctx context.Context, session *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, tool)
	}
	return tools
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}
func callMCPTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}
func assertMCPExitCode(t *testing.T, name string, result *mcp.CallToolResult, want int) {
	t.Helper()
	data, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s structured content = %T %#v", name, result.StructuredContent, result.StructuredContent)
	}
	code, ok := data["exitCode"].(float64)
	if !ok || int(code) != want {
		t.Fatalf("%s exitCode = %#v", name, data["exitCode"])
	}
}

func assertMCPDataContains(t *testing.T, name string, result *mcp.CallToolResult, value string) {
	t.Helper()
	data, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s structured content = %T", name, result.StructuredContent)
	}
	body, err := json.Marshal(data["data"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), value) {
		t.Fatalf("%s data does not contain %q: %s", name, value, body)
	}
}
