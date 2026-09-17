# Native lifecycle runner

Run every registered family and concrete variant against pinned Linux toolchains:

```sh
python3 tools/native-lifecycle/run.py --group all --keep-going
```

Use `--group python`, or repeat `--case uv --case poetry`, for a smaller gate. `--list` prints the
complete matrix without starting Docker. Results are written under `build/native-lifecycle/` by
default; each run contains `build.log`, one log per selected row, `summary.json`, and `summary.md`.

The runner snapshots the current tracked and non-ignored untracked working tree, so staged and
unstaged implementation changes are tested without changing them. The snapshot and Linux binaries
are mounted read-only. Containers do not receive the Docker socket, SSH agent, host Git config, or
credentials. Tests create consumers, local Git remotes, locks, and environments in container-local
temporary directories.

Rows whose native toolchain needs a composed image build that image from its tracked, pinned
Dockerfile in the same snapshot. The case container still receives only the read-only snapshot and
binaries; the Docker socket is never mounted into it.

The image tag, observed manager version, exact test command, target OS/architecture, result, and log
path are recorded. An unavailable image/runtime is `BLOCKED`; a missing mandatory tool, skipped
test, missing expected case marker, or nonzero lifecycle command is `FAIL`. Rows outside a partial
selection, and selected rows not reached after fail-fast, remain `NOT RUN`. Unit mocks and
cross-compilation are not accepted as native `PASS` evidence.

When a correction run supersedes selected rows, consolidate it without erasing the original failed
attempts:

```sh
python3 tools/native-lifecycle/consolidate.py \
  --base build/native-lifecycle/FULL_RUN/summary.json \
  --overlay build/native-lifecycle/CORRECTION_RUN/summary.json \
  --output build/native-lifecycle/CONSOLIDATED_RUN
```

Reports are applied in command-line order. For each row, the latest executed result wins, including
`FAIL` and `BLOCKED`; `NOT RUN` does not replace the preceding result. Consolidated inputs retain
and flatten their complete attempt history. Run `make native-report-test` to verify these rules.
