# git-a2a schema 2 conformance suite

This language-neutral corpus exercises the public schema 2 CLI through an external executable. It
does not import git-a2a internals and it uses no public network services.

```sh
go build -o /tmp/git-a2a ./cmd/git-a2a
CONFORMANCE_BIN=/tmp/git-a2a go run ./conformance/runner
```

On PowerShell, set `CONFORMANCE_BIN` to an absolute `.exe` path before running the same Go command.
Pass case directory names to run a subset; `--list` lists the selected cases without executing them.

## Version 2 case protocol

Each numbered directory under `cases/` contains:

- `manifest/`: the complete initial working directory;
- `command`: a JSON array of argv arrays, invoked in order in that same working directory;
- `expected/exit-code`: the expected exit code (execution stops at the first non-zero command).

Optional expectations are `stdout` and `stderr` (one Go regular expression per non-comment line,
with `!` negating a pattern), byte-exact `files/`, `file-patterns.json`, and `absent`. An optional
`env.json` adds environment variables.

`git-fixture.json` creates a local source commit and bare repository. Its `files` map is the tracked
source tree and `initWorktree` initializes the case working directory as a Git repository. The
runner replaces `{{GIT_URL}}` and `{{GIT_COMMIT}}` in the initial tree and command arguments. It also
supports `<ROOT>` and `<CORPUS_ROOT>` replacement tokens. Output normalization is limited to CRLF,
the temporary root, and the corpus root; result files are never normalized.

Protocol version 2 intentionally replaces version 1's single argv array and removes HTTP/contact,
cache, shell-fixture, and platform-skip facilities that belonged to the removed schema 1 product.

## Coverage

The corpus executes all five domain commands end to end against a local bare Git repository,
including agent metadata and a published surface. It also verifies offline text and JSON listing,
schema 1 rejection without mutation, dual-manifest rejection, and rejection of representative
removed commands (`install`, `who`, and `validate`).
