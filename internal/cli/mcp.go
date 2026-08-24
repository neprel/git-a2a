package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	lockfile "github.com/neprel/git-a2a/internal/lock"
	"github.com/neprel/git-a2a/internal/manifest"
	"github.com/neprel/git-a2a/internal/reference"
	"github.com/neprel/git-a2a/internal/render"
)

type mcpCommandResult struct {
	ExitCode    int      `json:"exitCode" jsonschema:"git-a2a exit code: 0 success, 1 failure or drift, 2 invalid or unresolved input"`
	Data        any      `json:"data,omitempty" jsonschema:"structured command result for read tools"`
	Records     []string `json:"records,omitempty" jsonschema:"ordered stdout records for write tools"`
	Diagnostics []string `json:"diagnostics,omitempty" jsonschema:"ordered stderr verdict and advisory lines"`
}

type mcpWhoInput struct {
	Root   string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	ID     string `json:"id,omitempty" jsonschema:"module dependency id; omit for this module"`
	Intent string `json:"intent,omitempty" jsonschema:"routing intent; defaults to question"`
	Path   string `json:"path,omitempty" jsonschema:"optional repository path for scope matching"`
}
type mcpShowInput struct {
	Root    string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	ID      string `json:"id,omitempty" jsonschema:"module dependency id; omit for this module"`
	Surface bool   `json:"surface,omitempty" jsonschema:"fetch and list the owner-published surface"`
}
type mcpStatusInput struct {
	Root    string   `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	IDs     []string `json:"ids,omitempty" jsonschema:"dependency ids; omit for all"`
	Offline bool     `json:"offline,omitempty" jsonschema:"do not contact remotes or card URLs"`
	Verbose bool     `json:"verbose,omitempty" jsonschema:"include detailed findings"`
}
type mcpValidateInput struct {
	Root  string   `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	Files []string `json:"files,omitempty" jsonschema:"manifest or lock paths; omit for current module files"`
}
type mcpDoctorInput struct {
	Root string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
}
type mcpFetchInput struct {
	Root    string   `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	IDs     []string `json:"ids,omitempty" jsonschema:"dependency ids; omit for all locked dependencies"`
	Surface bool     `json:"surface,omitempty" jsonschema:"also restore the owner-published surface recorded in the lock"`
}
type mcpExplainInput struct {
	Path string `json:"path" jsonschema:"manifest field path such as agents.contacts.kind"`
}
type mcpUsageInput struct {
	Prompt bool `json:"prompt,omitempty" jsonschema:"include the full fresh-agent briefing"`
}

type mcpAddInput struct {
	Root       string   `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	URL        string   `json:"url" jsonschema:"Git repository URL"`
	ID         string   `json:"id,omitempty" jsonschema:"override the dependency module id"`
	Path       string   `json:"path,omitempty" jsonschema:"module path inside a monorepo"`
	Track      string   `json:"track,omitempty" jsonschema:"locked or floating"`
	Wire       []string `json:"wire,omitempty" jsonschema:"ecosystems to wire"`
	NoWire     bool     `json:"noWire,omitempty" jsonschema:"record the dependency without ecosystem wiring"`
	Vendor     string   `json:"vendor,omitempty" jsonschema:"materialise locally as submodule or copy"`
	VendorPath string   `json:"vendorPath,omitempty" jsonschema:"consumer-root-relative vendor path"`
	NoRefresh  bool     `json:"noRefresh,omitempty" jsonschema:"skip package-manager Refresh"`
}
type mcpUpdateInput struct {
	Root        string   `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	IDs         []string `json:"ids,omitempty" jsonschema:"dependency ids; omit for all"`
	Check       bool     `json:"check,omitempty" jsonschema:"report updates without changing files"`
	Review      *bool    `json:"review,omitempty" jsonschema:"show or suppress manifest and surface diffs"`
	FollowMoves bool     `json:"followMoves,omitempty" jsonschema:"follow a declared moved-to source"`
	NoRefresh   bool     `json:"noRefresh,omitempty" jsonschema:"skip package-manager Refresh"`
}
type mcpSetInput struct {
	Root       string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	ID         string `json:"id" jsonschema:"dependency id to change"`
	Git        string `json:"git,omitempty" jsonschema:"replacement Git URL"`
	Ref        string `json:"ref,omitempty" jsonschema:"replacement branch, tag, or commit"`
	Path       string `json:"path,omitempty" jsonschema:"replacement module path"`
	Track      string `json:"track,omitempty" jsonschema:"locked or floating"`
	NewID      string `json:"newId,omitempty" jsonschema:"replacement dependency id"`
	Vendor     string `json:"vendor,omitempty" jsonschema:"materialise locally as submodule or copy"`
	VendorPath string `json:"vendorPath,omitempty" jsonschema:"replacement consumer-root-relative vendor path"`
	NoVendor   bool   `json:"noVendor,omitempty" jsonschema:"remove local materialisation and restore Git wiring"`
	Force      bool   `json:"force,omitempty" jsonschema:"allow replacement of dirty or drifted vendored content"`
	DryRun     bool   `json:"dryRun,omitempty" jsonschema:"print the proposed change without writing"`
	NoRefresh  bool   `json:"noRefresh,omitempty" jsonschema:"skip package-manager Refresh"`
}
type mcpWireInput struct {
	Root      string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	ID        string `json:"id,omitempty" jsonschema:"dependency id; omit for all"`
	Ecosystem string `json:"ecosystem,omitempty" jsonschema:"require one ecosystem adapter"`
	NoRefresh bool   `json:"noRefresh,omitempty" jsonschema:"skip package-manager Refresh"`
}
type mcpSyncInput struct {
	Root   string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	Check  bool   `json:"check,omitempty" jsonschema:"report stale blocks without writing"`
	Brief  bool   `json:"brief,omitempty" jsonschema:"render one contact per route"`
	Target string `json:"target,omitempty" jsonschema:"additional instruction file to update"`
}
type mcpContactInput struct {
	Root    string `json:"root,omitempty" jsonschema:"repository root; must be inside an allowed root (startup dir, --roots, client roots)"`
	ID      string `json:"id" jsonschema:"dependency id"`
	Intent  string `json:"intent" jsonschema:"routing intent"`
	Message string `json:"message" jsonschema:"complete request body to deliver"`
	Wait    bool   `json:"wait,omitempty" jsonschema:"wait for an A2A task terminal state"`
}

func (a *App) mcp(args []string) int {
	allowWrite := false
	anyRoot := false
	printRoots := false
	var rootFlags []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--allow-write":
			allowWrite = true
		case "--any-root":
			anyRoot = true
		case "--print-roots":
			printRoots = true
		case "--roots":
			if index+1 >= len(args) {
				fmt.Fprintln(a.Err, "mcp: --roots requires a directory list")
				return 2
			}
			index++
			rootFlags = append(rootFlags, args[index])
		default:
			fmt.Fprintf(a.Err, "mcp: unknown option %s\n", arg)
			return 2
		}
	}
	roots := newMCPRoots(a.root(), splitMCPRootFlags(rootFlags), anyRoot)
	if printRoots {
		fmt.Fprintln(a.Out, roots.line())
		return 0
	}
	fmt.Fprintln(a.Err, roots.line())
	server := a.newMCPServerWithRoots(allowWrite, roots)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(a.Err, "mcp: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) newMCPServer(allowWrite bool) *mcp.Server {
	return a.newMCPServerWithRoots(allowWrite, newMCPRoots(a.root(), nil, false))
}

func (a *App) newMCPServerWithRoots(allowWrite bool, roots *mcpRoots) *mcp.Server {
	instructions := compactBriefing + "\n" + roots.line()
	server := mcp.NewServer(&mcp.Implementation{Name: "git-a2a", Version: Version}, &mcp.ServerOptions{
		Instructions: instructions,
		Capabilities: &mcp.ServerCapabilities{},
		InitializedHandler: func(ctx context.Context, req *mcp.InitializedRequest) {
			refreshMCPClientRoots(ctx, req.Session, roots, a.Err)
		},
		RootsListChangedHandler: func(ctx context.Context, req *mcp.RootsListChangedRequest) {
			refreshMCPClientRoots(ctx, req.Session, roots, a.Err)
		},
	})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPointer(false)}
	addTool := func(tool *mcp.Tool) { tool.Annotations = readOnly }

	tool := &mcp.Tool{Name: "who", Description: "Resolve an intent to the owning agents and their declared contacts."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpWhoInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"who"}
		if in.ID != "" {
			args = append(args, in.ID)
		}
		if in.Intent != "" {
			args = append(args, "--intent", in.Intent)
		}
		if in.Path != "" {
			args = append(args, "--path", in.Path)
		}
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, true))
	})
	tool = &mcp.Tool{Name: "show", Description: "Read this module or a locked dependency and optionally its published surface."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpShowInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"show"}
		if in.ID != "" {
			args = append(args, in.ID)
		}
		if in.Surface {
			args = append(args, "--surface")
		}
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, true))
	})
	tool = &mcp.Tool{Name: "status", Description: "Check dependency, wiring, card, trust, and roster health."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpStatusInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := append([]string{"status"}, in.IDs...)
		if in.Offline {
			args = append(args, "--offline")
		}
		if in.Verbose {
			args = append(args, "-v")
		}
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, true))
	})
	tool = &mcp.Tool{Name: "validate", Description: "Validate manifest and lock files against the git-a2a standard."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpValidateInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := append([]string{"validate"}, in.Files...)
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, true))
	})
	tool = &mcp.Tool{Name: "doctor", Description: "Report Git and ecosystem tool prerequisites without installing anything."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpDoctorInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, []string{"doctor", "--json"}, true))
	})
	tool = &mcp.Tool{Name: "fetch", Description: "Restore disposable dependency cache content from exact lock coordinates."}
	tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPointer(false), IdempotentHint: true, OpenWorldHint: boolPointer(true)}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpFetchInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := append([]string{"fetch"}, in.IDs...)
		if in.Surface {
			args = append(args, "--surface")
		}
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, true))
	})
	tool = &mcp.Tool{Name: "explain", Description: "Read the normative generated reference entry for one manifest field."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpExplainInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		return mcpResult(a.runMCPCommand(ctx, roots, "", nil, []string{"explain", in.Path, "--json"}, true))
	})
	tool = &mcp.Tool{Name: "usage", Description: "Read the compact or full deterministic briefing for a coding agent."}
	addTool(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUsageInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"usage"}
		if in.Prompt {
			args = append(args, "--prompt")
		}
		args = append(args, "--json")
		return mcpResult(a.runMCPCommand(ctx, roots, "", nil, args, true))
	})

	if allowWrite {
		a.addMCPWriteTools(server, roots)
	}
	a.addMCPResources(server)
	return server
}

func (a *App) addMCPWriteTools(server *mcp.Server, roots *mcpRoots) {
	annotations := &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false), IdempotentHint: true}
	register := func(tool *mcp.Tool) { tool.Annotations = annotations }
	tool := &mcp.Tool{Name: "add", Description: "Import a Git module, resolve one commit, and wire declared ecosystems."}
	register(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAddInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"add", in.URL}
		if in.ID != "" {
			args = append(args, "--id", in.ID)
		}
		if in.Path != "" {
			args = append(args, "--path", in.Path)
		}
		if in.Track != "" {
			args = append(args, "--track", in.Track)
		}
		for _, wire := range in.Wire {
			args = append(args, "--wire", wire)
		}
		if in.NoWire {
			args = append(args, "--no-wire")
		}
		if in.Vendor != "" {
			args = append(args, "--vendor", in.Vendor)
		}
		if in.VendorPath != "" {
			args = append(args, "--vendor-path", in.VendorPath)
		}
		if in.NoRefresh {
			args = append(args, "--no-refresh")
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, false))
	})
	tool = &mcp.Tool{Name: "update", Description: "Resolve tracked refs and transactionally update selected dependencies."}
	register(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUpdateInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := append([]string{"update"}, in.IDs...)
		if in.Check {
			args = append(args, "--check")
		}
		if in.Review != nil {
			if *in.Review {
				args = append(args, "--review")
			} else {
				args = append(args, "--no-review")
			}
		}
		if in.FollowMoves {
			args = append(args, "--follow-moves")
		}
		if in.NoRefresh {
			args = append(args, "--no-refresh")
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, false))
	})
	tool = &mcp.Tool{Name: "set", Description: "Transactionally change a dependency source, ref, path, tracking, or id."}
	register(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSetInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"set", in.ID}
		for _, pair := range [][2]string{{"--git", in.Git}, {"--ref", in.Ref}, {"--path", in.Path}, {"--track", in.Track}, {"--id", in.NewID}, {"--vendor", in.Vendor}, {"--vendor-path", in.VendorPath}} {
			if pair[1] != "" {
				args = append(args, pair[0], pair[1])
			}
		}
		if in.DryRun {
			args = append(args, "--dry-run")
		}
		if in.NoVendor {
			args = append(args, "--no-vendor")
		}
		if in.Force {
			args = append(args, "--force")
		}
		if in.NoRefresh {
			args = append(args, "--no-refresh")
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, false))
	})
	tool = &mcp.Tool{Name: "wire", Description: "Repair native ecosystem dependency entries from manifest and lock."}
	register(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpWireInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"wire"}
		if in.ID != "" {
			args = append(args, in.ID)
		}
		if in.Ecosystem != "" {
			args = append(args, "--ecosystem", in.Ecosystem)
		}
		if in.NoRefresh {
			args = append(args, "--no-refresh")
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, false))
	})
	tool = &mcp.Tool{Name: "sync", Description: "Render or check the bounded git-a2a roster in instruction files."}
	register(tool)
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSyncInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"sync"}
		if in.Check {
			args = append(args, "--check")
		}
		if in.Brief {
			args = append(args, "--brief")
		}
		if in.Target != "" {
			args = append(args, "--target", in.Target)
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, nil, args, false))
	})
	contactAnnotations := *annotations
	contactAnnotations.OpenWorldHint = boolPointer(true)
	contactAnnotations.IdempotentHint = false
	tool = &mcp.Tool{Name: "contact", Description: "Deliver a request through the owner's first supported declared contact.", Annotations: &contactAnnotations}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpContactInput) (*mcp.CallToolResult, mcpCommandResult, error) {
		args := []string{"contact", in.ID, "--intent", in.Intent, "--message", "-"}
		if in.Wait {
			args = append(args, "--wait")
		}
		return mcpResult(a.runMCPCommand(ctx, roots, in.Root, strings.NewReader(in.Message), args, false))
	})
}

func (a *App) addMCPResources(server *mcp.Server) {
	type resourceSpec struct {
		uri, name, description, mime string
		read                         func() (string, error)
	}
	resources := []resourceSpec{
		{"a2amodule://manifest", "manifest", "Current a2amodule.yml module contract.", "application/yaml", func() (string, error) {
			body, err := os.ReadFile(filepath.Join(a.root(), "a2amodule.yml"))
			return string(body), err
		}},
		{"a2amodule://lock", "lock", "Current deterministic a2amodule.lock resolutions.", "application/yaml", func() (string, error) {
			body, err := os.ReadFile(filepath.Join(a.root(), "a2amodule.lock"))
			return string(body), err
		}},
		{"a2amodule://roster", "roster", "Freshly rendered git-a2a AGENTS.md managed block.", "text/markdown", func() (string, error) {
			own, err := manifest.Load(filepath.Join(a.root(), "a2amodule.yml"))
			if err != nil {
				return "", err
			}
			locked, err := lockfile.Load(a.root())
			if err != nil {
				return "", err
			}
			text, err := render.Build(a.root(), own, locked, false)
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("%w; run git-a2a fetch", err)
			}
			return text, err
		}},
		{"a2amodule://reference", "reference", "Generated normative manifest field reference.", "text/markdown", func() (string, error) { return reference.Manifest, nil }},
	}
	for _, spec := range resources {
		spec := spec
		server.AddResource(&mcp.Resource{URI: spec.uri, Name: spec.name, Title: spec.name, Description: spec.description, MIMEType: spec.mime}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			text, err := spec.read()
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", spec.name, err)
			}
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: spec.mime, Text: text}}}, nil
		})
	}
}

func (a *App) runMCPCommand(ctx context.Context, roots *mcpRoots, root string, in *strings.Reader, args []string, jsonOutput bool) mcpCommandResult {
	var out, diagnostics bytes.Buffer
	resolvedRoot, err := roots.resolveRoot(root)
	if err != nil {
		return mcpCommandResult{ExitCode: 2, Diagnostics: []string{err.Error()}}
	}
	if err := guardMCPPathArguments(roots, resolvedRoot, args); err != nil {
		return mcpCommandResult{ExitCode: 2, Diagnostics: []string{err.Error()}}
	}
	command := New(&out, &diagnostics)
	command.Root = resolvedRoot
	command.Home = a.Home
	command.Timeout = a.Timeout
	command.Runner = a.Runner
	command.ctx = ctx
	command.mcpInvocation = true
	if in != nil {
		command.In = in
	}
	code := command.Run(args)
	result := mcpCommandResult{ExitCode: code, Diagnostics: nonemptyLines(diagnostics.String())}
	if jsonOutput && strings.TrimSpace(out.String()) != "" {
		if err := json.Unmarshal(out.Bytes(), &result.Data); err != nil {
			result.Diagnostics = append(result.Diagnostics, "MCP JSON decode: "+err.Error())
			result.ExitCode = 1
		}
	} else {
		result.Records = nonemptyLines(out.String())
	}
	return result
}

func mcpResult(result mcpCommandResult) (*mcp.CallToolResult, mcpCommandResult, error) {
	return &mcp.CallToolResult{IsError: result.ExitCode != 0}, result, nil
}
func nonemptyLines(text string) []string {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}
func boolPointer(value bool) *bool { return &value }
