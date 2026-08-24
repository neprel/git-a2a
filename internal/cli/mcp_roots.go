package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpRootEntry struct {
	display  string
	resolved string
	exists   bool
}

// mcpRoots is the process-local path boundary for the stdio MCP server.
// Client roots are replaced as one snapshot when roots/list changes.
type mcpRoots struct {
	mu      sync.RWMutex
	startup mcpRootEntry
	flags   []mcpRootEntry
	client  []mcpRootEntry
	any     bool
}

func newMCPRoots(startup string, flags []string, anyRoot bool) *mcpRoots {
	return &mcpRoots{
		startup: makeMCPRootEntry(startup, startup),
		flags:   makeMCPRootEntries(flags),
		any:     anyRoot,
	}
}

func makeMCPRootEntries(paths []string) []mcpRootEntry {
	entries := make([]mcpRootEntry, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		entry := makeMCPRootEntry(path, path)
		key := filepath.Clean(entry.resolved)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	return entries
}

func makeMCPRootEntry(display, path string) mcpRootEntry {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return mcpRootEntry{display: display, resolved: abs}
	}
	return mcpRootEntry{display: display, resolved: filepath.Clean(resolved), exists: true}
}

func (roots *mcpRoots) setClient(paths []string) {
	roots.mu.Lock()
	defer roots.mu.Unlock()
	roots.client = makeMCPRootEntries(paths)
}

func (roots *mcpRoots) entries() []mcpRootEntry {
	roots.mu.RLock()
	defer roots.mu.RUnlock()
	result := make([]mcpRootEntry, 0, 1+len(roots.flags)+len(roots.client))
	result = append(result, roots.startup)
	result = append(result, roots.flags...)
	result = append(result, roots.client...)
	return result
}

func (roots *mcpRoots) displays() []string {
	entries := roots.entries()
	result := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.display] {
			continue
		}
		seen[entry.display] = true
		result = append(result, entry.display)
	}
	return result
}

func (roots *mcpRoots) line() string {
	if roots.any {
		return "effective roots: any (--any-root)"
	}
	return "effective roots: " + strings.Join(roots.displays(), ", ")
}

func (roots *mcpRoots) resolveRoot(input string) (string, error) {
	path := input
	if path == "" {
		path = roots.startup.resolved
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(roots.startup.resolved, path)
	}
	return roots.resolvePath(path)
}

func (roots *mcpRoots) resolveInside(root, input string) (string, error) {
	path := input
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return roots.resolvePath(path)
}

func (roots *mcpRoots) resolvePath(path string) (string, error) {
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		cleaned = filepath.Clean(path)
	}
	resolved := resolveExistingPrefix(cleaned)
	if roots.any || roots.contains(resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("root outside allowed roots: %s; allowed: %s; start the server with --roots or --any-root to widen", cleaned, strings.Join(roots.displays(), ", "))
}

func resolveExistingPrefix(path string) string {
	current := filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(path)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func (roots *mcpRoots) contains(path string) bool {
	for _, entry := range roots.entries() {
		if !entry.exists {
			resolved, err := filepath.EvalSymlinks(entry.resolved)
			if err != nil {
				continue
			}
			entry.resolved = filepath.Clean(resolved)
		}
		if pathEqual(entry.resolved, path) {
			return true
		}
		rel, err := filepath.Rel(entry.resolved, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func pathEqual(left, right string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func fileRootPath(uri string) (string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		if runtime.GOOS == "windows" {
			return filepath.FromSlash("//" + parsed.Host + parsed.Path), true
		}
		return "", false
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", false
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), path != ""
}

func splitMCPRootFlags(values []string) []string {
	var result []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	sort.Strings(result)
	return result
}

func refreshMCPClientRoots(ctx context.Context, session *mcp.ServerSession, roots *mcpRoots, diagnostics io.Writer) {
	params := session.InitializeParams()
	if params == nil || params.Capabilities == nil || (params.Capabilities.RootsV2 == nil && !params.Capabilities.Roots.ListChanged) {
		return
	}
	listed, err := session.ListRoots(ctx, nil)
	if err != nil {
		fmt.Fprintf(diagnostics, "mcp: client roots unavailable: %v\n", err)
		return
	}
	paths := make([]string, 0, len(listed.Roots))
	for _, root := range listed.Roots {
		if root == nil {
			continue
		}
		path, ok := fileRootPath(root.URI)
		if !ok {
			fmt.Fprintf(diagnostics, "mcp: ignored non-file client root %s\n", root.URI)
			continue
		}
		paths = append(paths, path)
	}
	roots.setClient(paths)
}

func guardMCPPathArguments(roots *mcpRoots, root string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "validate":
		for _, value := range args[1:] {
			if strings.HasPrefix(value, "-") {
				continue
			}
			if _, err := roots.resolveInside(root, value); err != nil {
				return err
			}
		}
	case "who":
		return guardMCPFlagPath(roots, root, args, "--path")
	case "sync":
		return guardMCPFlagPath(roots, root, args, "--target")
	case "add", "set":
		return guardMCPFlagPath(roots, root, args, "--vendor-path")
	}
	return nil
}

func guardMCPFlagPath(roots *mcpRoots, root string, args []string, flag string) error {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag {
			_, err := roots.resolveInside(root, args[index+1])
			return err
		}
	}
	return nil
}
