# MCP server

`git-a2a mcp` projects the CLI into a stateless Model Context Protocol server over stdio. It is
useful when an agent harness handles structured tools more reliably than shell output. The
portable [Agent Skill](../skills/git-a2a/SKILL.md) and `git-a2a usage` remain the primary,
lower-token guidance paths.

Run the server from a repository containing `a2amodule.yml`. The default surface is read-only:
`who`, `show`, `status`, `validate`, `doctor`, `explain`, and `usage`. It also exposes the current
manifest, lock, freshly rendered roster, and generated field reference as `a2amodule://`
resources. Starting it with `--allow-write` additionally exposes `add`, `update`, `set`, `wire`,
`sync`, and `contact`. `remove` is deliberately CLI-only.

Every repository-dependent tool accepts an optional `root` path. Without it, the tool uses the
server process's startup directory. A multi-repository harness may therefore keep one server and
pass a different absolute or startup-relative `root` per call, or start one stdio process per
repository. The processes do not share ports, mutable server state, or cache directories; each
repository's recoverable cache remains under that repository's `.git-a2a/`. The fixed MCP
resources describe the startup repository, so use the read tools with `root` when switching.

## Automatic project setup

```sh
git-a2a setup --dry-run
git-a2a setup
git-a2a setup --check
```

Setup detects supported harnesses and writes only repository-scoped files. For Claude Code it
installs the skill under both `.agents/skills/git-a2a/` and `.claude/skills/git-a2a/`, then adds
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
hermes mcp add git-a2a --command git-a2a --args mcp
```

The resulting entry is:

```yaml
mcp_servers:
  git-a2a:
    command: git-a2a
    args: [mcp]
```

## OpenClaw

OpenClaw keeps its MCP registry in user scope. Its `set` command changes configuration without
probing or starting the server:

```sh
openclaw mcp set git-a2a '{"command":"git-a2a","args":["mcp"]}'
openclaw mcp doctor git-a2a --probe
```

The corresponding `~/.openclaw/openclaw.json` fragment is:

```json
{
  "mcp": {
    "servers": {
      "git-a2a": { "command": "git-a2a", "args": ["mcp"] }
    }
  }
}
```

## Write access

Write tools are never enabled by setup. If repository mutation and contact delivery are intended,
change the argument list from `["mcp"]` to `["mcp", "--allow-write"]` (or the TOML/YAML
equivalent) and review that configuration change. The server has no independent authorization
layer: it has the filesystem, process, Git credential, and network authority of the harness
process that starts it.

The implementation uses the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
and opens no network listener. See the [CLI reference](cli.md#mcp) for exit behavior.

## Distribution and discovery

Stable releases publish `io.github.neprel/git-a2a` to the official MCP Registry through GitHub
OIDC. Registry metadata names three equivalent stdio packages: the `git-a2a` npm launcher, the
`ghcr.io/neprel/git-a2a` OCI image, and one cross-platform `.mcpb` release asset. The MCPB is
assembled from the same six GoReleaser binaries and carries a thin Node 18+ platform launcher;
its `server.json` entry uses the SHA-256 calculated from the actual deterministic bundle. The
container image and npm package carry the registry's matching ownership metadata.
