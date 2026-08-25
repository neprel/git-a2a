# a2amodule conformance suite

This corpus is the executable public contract for an implementation of a2amodule schema 1. It
does not import git-a2a internals. Point it at any CLI-compatible executable:

```sh
go build -o /tmp/git-a2a ./cmd/git-a2a
CONFORMANCE_BIN=/tmp/git-a2a go run ./conformance/runner
```

PowerShell uses the same contract:

```powershell
go build -o "$env:RUNNER_TEMP\git-a2a.exe" ./cmd/git-a2a
$env:CONFORMANCE_BIN = "$env:RUNNER_TEMP\git-a2a.exe"
go run ./conformance/runner
```

Pass case directory names as arguments to run a subset. `--list` prints them without running.
The process exits 0 only when every selected case passes.

## Case format

Each numbered directory under `cases/` contains:

- `manifest/`: the complete initial working directory;
- `cache/`: optional disposable dependency cache, materialized as `.git-a2a/cache/`;
- `command`: a JSON array of arguments after the executable name;
- `expected/exit-code`: one integer.

Optional files:

- `expected/stdout` and `expected/stderr`: one Go regular expression per line; blank lines and
  `#` comments are ignored, and `!pattern` requires absence;
- `expected/files/`: files that must match the final working directory byte-for-byte;
- `expected/file-patterns.json`: repository-relative files mapped to required Go regular
  expressions (for syntax forms whose unrelated formatter whitespace is not normative);
- `expected/absent`: relative paths that must not exist afterward;
- `env.json`: environment additions; `<ROOT>` expands to the temporary working directory;
- `platform`: `all`, `posix`, or `windows`;
- `fixture.sh`: a POSIX setup script run with `ROOT` and `CORPUS_ROOT` before the command;
- `http-fixture.json`: one local TLS response plus the exact expected request. `{{HTTP_URL}}` in
  the initial repository, command, and environment becomes the fixture origin. The runner sets
  `SSL_CERT_FILE` to its temporary CA certificate.
- `git-fixture.json`: files for a local source and bare repository; `{{GIT_URL}}`,
  `{{GIT_COMMIT}}`, and `{{GIT_TREE}}` become its resolved values.

The runner replaces the temporary directory with `<ROOT>` and the TLS origin with `<HTTP_URL>`
before matching output. It preserves every other byte, including spaces and line boundaries.
Result files are never normalized.

`<CORPUS_ROOT>` in command/environment values resolves to the checkout containing this suite. It
is used only when a case intentionally validates a canonical source fixture such as every file in
`spec/examples/`; mutation cases remain isolated under `<ROOT>`.

## Coverage

Version 1 covers every checked-in valid and invalid specification example, validation error
classes, optional-feature reporting, roster/routing output, declared-contact placeholder and
consent rules, forge deep links, and every registered ecosystem wiring form. Git-backed lifecycle
cases use local bare repositories and never depend on a public host.
