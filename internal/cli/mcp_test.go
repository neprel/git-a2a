package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unsafe"

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
	if got, want := toolNames(readTools), []string{"doctor", "explain", "fetch", "show", "status", "usage", "validate", "who"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("read tools = %v, want %v", got, want)
	}
	for _, tool := range readTools {
		if tool.Name != "fetch" {
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint || !tool.Annotations.IdempotentHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Fatalf("fetch annotations = %#v", tool.Annotations)
		}
	}
	for name, arguments := range map[string]map[string]any{
		"who": {}, "show": {}, "status": {"offline": true}, "validate": {}, "doctor": {}, "fetch": {},
		"explain": {"path": "module.id"}, "usage": {},
	} {
		if name == "fetch" {
			continue // the fixture has no dependencies; fetch behavior has its own e2e coverage
		}
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
	if len(writeTools) != 14 {
		t.Fatalf("write-enabled tool count = %d", len(writeTools))
	}
	for _, tool := range writeTools {
		if tool.Name == "contact" && (tool.Annotations == nil || tool.Annotations.IdempotentHint) {
			t.Fatalf("contact must be non-idempotent: %#v", tool.Annotations)
		}
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

func TestMCPFetchRestoresCacheThenWhoWorks(t *testing.T) {
	root, remote, commit, manifestRaw, surfaceTree := fetchFixture(t)
	writeFetchConsumer(t, root, remote, commit, manifestRaw, surfaceTree)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app := New(os.Stdout, os.Stderr)
	app.Root = root
	session := connectMCP(t, ctx, app.newMCPServer(false))
	defer session.Close()
	fetched := callMCPTool(t, ctx, session, "fetch", map[string]any{"ids": []string{"acme-lib"}})
	assertMCPExitCode(t, "fetch", fetched, 0)
	who := callMCPTool(t, ctx, session, "who", map[string]any{"id": "acme-lib", "intent": "question"})
	assertMCPExitCode(t, "who", who, 0)
	assertMCPDataContains(t, "who after fetch", who, "acme-lib-owner")
	assertMCPDataContains(t, "who origin", who, "untrustedFields")
	assertMCPDataContains(t, "who origin", who, "acme-lib@"+commit[:7])
}

func TestMissingCacheErrorsRecommendFetch(t *testing.T) {
	root, remote, commit, manifestRaw, surfaceTree := fetchFixture(t)
	writeFetchConsumer(t, root, remote, commit, manifestRaw, surfaceTree)
	for _, command := range [][]string{{"who", "acme-lib"}, {"show", "acme-lib"}, {"sync"}} {
		var out, errOut strings.Builder
		app := New(&out, &errOut)
		app.Root = root
		if code := app.Run(command); code == 0 {
			t.Fatalf("%v unexpectedly succeeded", command)
		}
		if !strings.HasSuffix(errOut.String(), "run git-a2a fetch\n") {
			t.Errorf("%v error lacks fetch repair suffix: %q", command, errOut.String())
		}
	}
	var out, errOut strings.Builder
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"status", "--offline", "-v"}); code != 1 || !strings.Contains(out.String(), "cache missing — run git-a2a fetch") {
		t.Fatalf("status exit/output = %d, %q, %q", code, out.String(), errOut.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session := connectMCP(t, ctx, app.newMCPServer(false))
	defer session.Close()
	_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "a2amodule://roster"})
	if err == nil || !strings.Contains(err.Error(), "run git-a2a fetch") {
		t.Fatalf("roster cache error = %v", err)
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
	firstSession := connectMCP(t, ctx, firstApp.newMCPServerWithRoots(false, newMCPRoots(first, []string{second}, false)))
	defer firstSession.Close()
	secondApp := New(os.Stdout, os.Stderr)
	secondApp.Root = second
	secondSession := connectMCP(t, ctx, secondApp.newMCPServer(false))
	defer secondSession.Close()

	assertMCPDataContains(t, "first instance", callMCPTool(t, ctx, firstSession, "show", map[string]any{}), "consumer-one")
	assertMCPDataContains(t, "second instance", callMCPTool(t, ctx, secondSession, "show", map[string]any{}), "consumer-two")
	assertMCPDataContains(t, "root switch", callMCPTool(t, ctx, firstSession, "show", map[string]any{"root": second}), "consumer-two")
}

func TestMCPRootsDenyOutsideAndAllowFlags(t *testing.T) {
	startup := mcpFixtureWithID(t, "startup")
	allowed := mcpFixtureWithID(t, "allowed")
	outside := mcpFixtureWithID(t, "outside")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app := New(os.Stdout, os.Stderr)
	app.Root = startup

	guarded := connectMCP(t, ctx, app.newMCPServerWithRoots(false, newMCPRoots(startup, []string{allowed}, false)))
	defer guarded.Close()
	assertMCPDataContains(t, "flag root", callMCPTool(t, ctx, guarded, "show", map[string]any{"root": allowed}), "allowed")
	denied := callMCPTool(t, ctx, guarded, "show", map[string]any{"root": outside})
	assertMCPExitCode(t, "outside root", denied, 2)
	if !denied.IsError || !strings.Contains(mcpDiagnostics(t, denied), "root outside allowed roots:") || !strings.Contains(mcpDiagnostics(t, denied), "--roots or --any-root") {
		t.Fatalf("outside result = %#v", denied)
	}

	unrestricted := connectMCP(t, ctx, app.newMCPServerWithRoots(false, newMCPRoots(startup, nil, true)))
	defer unrestricted.Close()
	assertMCPDataContains(t, "any root", callMCPTool(t, ctx, unrestricted, "show", map[string]any{"root": outside}), "outside")
}

func TestMCPRootsRejectSymlinkAndEscapingFileArguments(t *testing.T) {
	startup := mcpFixtureWithID(t, "startup")
	outside := mcpFixtureWithID(t, "outside")
	link := filepath.Join(startup, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	app := New(os.Stdout, os.Stderr)
	app.Root = startup
	session := connectMCP(t, ctx, app.newMCPServer(false))
	defer session.Close()
	for name, arguments := range map[string]map[string]any{
		"show root":      {"root": link},
		"validate files": {"files": []string{filepath.Join("..", filepath.Base(outside), "a2amodule.yml")}},
	} {
		tool := strings.Fields(name)[0]
		result := callMCPTool(t, ctx, session, tool, arguments)
		assertMCPExitCode(t, name, result, 2)
		if !result.IsError || !strings.Contains(mcpDiagnostics(t, result), "root outside allowed roots:") {
			t.Fatalf("%s result = %#v", name, result)
		}
	}
}

func TestMCPClientRootsAndListChanged(t *testing.T) {
	startup := mcpFixtureWithID(t, "startup")
	first := mcpFixtureWithID(t, "client-first")
	second := mcpFixtureWithID(t, "client-second")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var diagnostics strings.Builder
	app := New(os.Stdout, &diagnostics)
	app.Root = startup
	roots := newMCPRoots(startup, nil, false)
	server := app.newMCPServerWithRoots(false, roots)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "git-a2a-roots-test", Version: "1"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{RootsV2: &mcp.RootCapabilities{ListChanged: true}},
	})
	client.AddRoots(&mcp.Root{URI: fileURI(first)})
	session, err := client.Connect(ctx, clientTransport, legacyMCPClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	waitForMCPRoot(t, roots, first, &diagnostics)
	assertMCPDataContains(t, "first client root", callMCPTool(t, ctx, session, "show", map[string]any{"root": first}), "client-first")
	client.AddRoots(&mcp.Root{URI: fileURI(second)})
	waitForMCPRoot(t, roots, second, &diagnostics)
	assertMCPDataContains(t, "changed client root", callMCPTool(t, ctx, session, "show", map[string]any{"root": second}), "client-second")
}

func TestMCPPrintRoots(t *testing.T) {
	root := t.TempDir()
	var out, errOut strings.Builder
	app := New(&out, &errOut)
	app.Root = root
	if code := app.Run([]string{"mcp", "--roots", "one,two", "--roots", "three", "--print-roots"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, value := range []string{root, "one", "two", "three"} {
		if !strings.Contains(out.String(), value) {
			t.Errorf("print roots lacks %q: %q", value, out.String())
		}
	}
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

func mcpDiagnostics(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	data, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content = %T", result.StructuredContent)
	}
	body, _ := json.Marshal(data["diagnostics"])
	return string(body)
}

func fileURI(path string) string {
	slash := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func waitForMCPRoot(t *testing.T, roots *mcpRoots, want string, diagnostics *strings.Builder) {
	t.Helper()
	want = resolveExistingPrefix(want)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range roots.entries() {
			if pathEqual(entry.resolved, want) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client root %s was not received: %v; diagnostics: %s", want, roots.displays(), diagnostics.String())
}

func legacyMCPClientOptions() *mcp.ClientSessionOptions {
	options := &mcp.ClientSessionOptions{}
	field := reflect.ValueOf(options).Elem().FieldByName("protocolVersion")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().SetString("2025-11-25")
	return options
}
