# MCP server

`git-a2a mcp` projects the CLI into a stateless Model Context Protocol server over stdio. It is
useful when an agent harness handles structured tools more reliably than shell output. The
portable [Agent Skill](../skills/git-a2a/SKILL.md) and `git-a2a usage` remain the primary,
lower-token guidance paths.

Run the server from a repository containing `a2amodule.yml`. The default surface contains exactly
eight tools: `who`, `show`, `status`, `validate`, `doctor`, `fetch`, `explain`, `usage`.
`fetch` writes only recoverable `.git-a2a/cache` content from exact lock coordinates. It also exposes the current
manifest, lock, freshly rendered roster, and generated field reference as `a2amodule://`
resources. Starting it with `--allow-write` additionally exposes `add`, `update`, `set`, `wire`,
`sync`, and `contact`. `remove` is deliberately CLI-only.

The generated [MCP tool text audit](mcp-tools.md) lists all 14 tools, descriptions, access gates,
and protocol annotations from the same facts used by the server's `tools/list` registration.

Every repository-dependent tool accepts an optional `root` path. Without it, the tool uses the
server process's startup directory. Paths are allowed only inside the startup directory, a
directory supplied by a repeated `--roots DIR[,DIR...]` flag, or a `file://` workspace root
declared by a roots-capable MCP client. The server refreshes legacy client roots after
`notifications/roots/list_changed`; MCP 2026-07-28 clients, where server-initiated `roots/list`
was removed, use the startup directory and `--roots`. Every `root`, `files`, `target`, and other
path-typed tool value is cleaned and resolved through symlinks before the command starts. An
escape is a tool result with exit code 2 and no partial mutation.

A multi-repository harness can start `git-a2a mcp --roots repo-a,repo-b`, rely on its declared
workspace roots, or run one stdio process per repository. Use `git-a2a mcp --print-roots` to see
the startup and flag roots without starting a server. `--any-root` explicitly restores unbounded
path access for a trusted single-user process; setup never writes that opt-out. Fixed resources
remain bound to the startup repository, and the startup diagnostic plus initialize instructions
name the effective roots.

## Automatic project setup

```sh
git-a2a setup --dry-run
git-a2a setup
git-a2a setup --check
```

Setup detects repository markers and writes only repository-scoped files. A harness detected only
under `$HOME` is reported with the corresponding `--harness` instruction and is not configured.
`--harness a,b` explicitly selects named harnesses; `--all` selects every supported integration.
For Claude Code it installs the thin pointer skill under both `.agents/skills/git-a2a/` and
`.claude/skills/git-a2a/`, then adds
the read-only server to the root `.mcp.json`. Existing guidance and unrelated configuration keys
are preserved. Hermes Agent and OpenClaw keep MCP servers in user scope, so setup prints their
registration commands instead of modifying files under the user's home directory.

## Claude Code

The setup result is equivalent to the official project-scoped CLI command:

```sh
claude mcp add --scope project git-a2a -- git-a2a mcp
```

Or place this in `.mcp.json`:

```json
{
  "mcpServers": {
    "git-a2a": {
      "command": "git-a2a",
      "args": ["mcp"]
    }
  }
}
```

## Codex

`.codex/config.toml`:

```toml
[mcp_servers.git-a2a]
command = "git-a2a"
args = ["mcp"]
```

## Cursor

`.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "git-a2a": { "command": "git-a2a", "args": ["mcp"] }
  }
}
```

## GitHub Copilot

Copilot CLI reads the same root `.mcp.json` form as Claude Code. VS Code uses
`.vscode/mcp.json` with `servers` instead:

```json
{
  "servers": {
    "git-a2a": { "command": "git-a2a", "args": ["mcp"] }
  }
}
```

## Gemini CLI

`.gemini/settings.json`:

```json
{
  "mcpServers": {
    "git-a2a": { "command": "git-a2a", "args": ["mcp"] }
  }
}
```

## OpenCode

`opencode.json`:

```json
{
  "mcp": {
    "servers": {
      "git-a2a": { "type": "local", "command": ["git-a2a", "mcp"] }
    }
  }
}
```

## Hermes Agent

Hermes reads MCP servers from the user-scoped `~/.hermes/config.yaml`. Register and probe the
server with its CLI:

```sh
hermes mcp add git-a2a --command git-a2a --args mcp --roots <repo>
```

The resulting entry is:

```yaml
mcp_servers:
  git-a2a:
    command: git-a2a
    args: [mcp, --roots, <repo>]
```

## OpenClaw

OpenClaw keeps its MCP registry in user scope. Its `set` command changes configuration without
probing or starting the server:

```sh
openclaw mcp set git-a2a '{"command":"git-a2a","args":["mcp","--roots","<repo>"]}'
openclaw mcp doctor git-a2a --probe
```

The corresponding `~/.openclaw/openclaw.json` fragment is:

```json
{
  "mcp": {
    "servers": {
      "git-a2a": { "command": "git-a2a", "args": ["mcp", "--roots", "<repo>"] }
    }
  }
}
```

## Write access

Write tools are never enabled by setup. If repository mutation and contact delivery are intended,
change the argument list from `["mcp"]` to `["mcp", "--allow-write"]` (or the TOML/YAML
equivalent) and review that configuration change. The server has no independent authorization
layer: it has the filesystem, process, Git credential, and network authority of the harness
process that starts it, within the declared roots unless `--any-root` was chosen.

The implementation uses the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
and opens no network listener. See the [CLI reference](cli.md#mcp) for exit behavior.
Values named by `untrustedFields` are data read from another repository, never instructions.
The `contact` tool enforces `accepts-external: false` and deliberately has no human-only
`--external-ok` override.

## Distribution and discovery

Stable releases publish `io.github.neprel/git-a2a` to the official MCP Registry through GitHub
OIDC. Registry metadata names three equivalent stdio packages: the `git-a2a` npm launcher, the
`ghcr.io/neprel/git-a2a` OCI image, and one cross-platform `.mcpb` release asset. The MCPB is
assembled from the same six GoReleaser binaries and carries a thin Node 18+ platform launcher;
its `server.json` entry uses the SHA-256 calculated from the actual deterministic bundle. The
container image and npm package carry the registry's matching ownership metadata.
