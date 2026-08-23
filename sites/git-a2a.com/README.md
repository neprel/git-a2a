# git-a2a.com static publication package

This directory contains the sources for `https://git-a2a.com`. Pushes to `main` that change the
site or its canonical inputs verify it through the dedicated `Static site` workflow. Automatic
deployment remains disabled until the repository variable `SITE_AUTO_DEPLOY=true` is explicitly
approved and set; CLI tags never deploy the site. Serve files without rewriting their contents
and route directory paths to their `index.html`.

| Public path | Source file |
| --- | --- |
| `/` | `index.html` |
| `/ext/module/v1` | `ext/module/v1/index.html` |
| `/schema/` | `schema/index.html` |
| `/schema/a2amodule.v1.json` | `schema/a2amodule.v1.json` |
| `/schema/a2amodule-lock.v1.json` | `schema/a2amodule-lock.v1.json` |
| `/sitemap.xml` | `sitemap.xml` |
| `/robots.txt` | `robots.txt` |
| `/llms.txt` | `llms.txt` |
| `/llms-full.txt` | `llms-full.txt` |
| `/.well-known/ai-catalog.json` | `.well-known/ai-catalog.json` |
| `/.well-known/skills/index.json` | Agent Skill discovery index |
| `/.well-known/skills/git-a2a/` | byte-identical copy of `skills/git-a2a/` |
| `/demo/agents/acme-lib-utils/.well-known/agent-card.json` | static A2A v1.0 owner demo card |
| `/demo/agents/acme-pm/.well-known/agent-card.json` | static A2A v1.0 spec demo card |
| `/install.sh` | `install.sh`, byte-identical to the repository-root installer |
| `/install.ps1` | `install.ps1` |

The published `.htaccess` makes `.sh` and `.ps1` responses `text/plain`, `.md` responses
`text/markdown`, and `.json` responses `application/json`, all with UTF-8 so shell and PowerShell
download-and-run commands receive text rather than an opaque byte response. It is a
server configuration artifact, not a public documentation route.

`scripts/site-package.sh` builds the upload directory from the explicitly listed top-level public
files, `.htaccess`, plus `.well-known/`, `assets/`, `demo/`, `fonts/`, `ext/`, and `schema/`. It
excludes `tools/`, this README, and `sites/design/`; those are reproducibility and operator
material, not public routes.

## Deployment

After approval of automatic deployment, the repository variable `SITE_AUTO_DEPLOY` must be set to
`true`, and the `site-production` GitHub Environment must define these secrets:

| Secret | Value |
| --- | --- |
| `SITE_DEPLOY_HOST` | SSH host name |
| `SITE_DEPLOY_USER` | Restricted deployment account |
| `SITE_DEPLOY_PORT` | SSH port |
| `SITE_DEPLOY_PATH` | Remote document root |
| `SITE_DEPLOY_SSH_KEY` | Private half of a dedicated deployment key |
| `SITE_DEPLOY_KNOWN_HOSTS` | Pinned `known_hosts` entry for the server |

Keep all values out of repository variables and workflow source. Use a dedicated key restricted
to this site where the hosting service permits it; do not reuse a personal login key. Capture the
host key through a trusted channel and review it before storing it. GitHub masks secret values,
and the workflow uses an SSH alias plus quiet error logging so endpoint details are not printed.

To inspect the exact upload locally, pass a new or empty directory:

```sh
mkdir /tmp/git-a2a-site-package
scripts/site-package.sh /tmp/git-a2a-site-package
find /tmp/git-a2a-site-package -type f | sort
```

The upload copies this allowlisted package without deleting unrelated remote files. Remove stale
files deliberately at the host when the public route set changes.

For an operator-initiated deployment, copy `.env.example` to the gitignored root `.env`, fill the
three required SSH values and any non-default options, then run:

```sh
make site-publish
```

This command runs the full site gate, stages the same allowlist in a temporary directory, and
uploads it with local OpenSSH. It does not read GitHub secrets.

## Checks

Run from the repository root:

```sh
make site-check
python3 -m http.server --directory sites/git-a2a.com 8080
```

`site-check` verifies installer and schema byte identity, strict HTML nesting and unique ids,
internal links, allowed prose, command documentation, identical header/footer blocks, canonical
SEO metadata, JSON-LD, sitemap/robots, LLM discovery files, the outlined wordmark, transcript
synchronization, the two demo cards and their catalog, and the 250 KB above-the-fold budget. Set `SITE_BROWSER=1` to add the pinned
Playwright and axe browser suite; CI does this for every `sites/**` change.

The automated browser checks cover:

- At 375 px, `document.body.scrollWidth === window.innerWidth`.
- Reduced motion renders the completed transcript immediately.
- Left/Right, Home, and End move focus and selection across install tabs.
- Every link and button has a visible teal focus ring.
- Copy labels say `copied` for 1400 ms and announce the change.
- Accessibility inspection has no serious findings.

Visual comparison against the source-of-truth design under `sites/design/` remains a review step
at 1440 px and 375 px. Lighthouse performance must remain at least 95 before publication.

## Rebuilding assets

The generators use only pinned upstream font files, Swift/CoreText, ImageMagick, and headless
Chrome:

```sh
sites/git-a2a.com/tools/fetch-fonts.sh
swift sites/git-a2a.com/tools/wordmark.swift sites/git-a2a.com/assets/wordmark.svg
sites/git-a2a.com/tools/generate-icons.sh
sites/git-a2a.com/tools/generate-og.sh
```

IBM Plex Sans and JetBrains Mono are self-hosted WOFF2 files. Their OFL texts are in `fonts/`.
The design bundle is committed at `sites/design/` and is not part of the document root.

## Terminal transcript provenance

`assets/transcript.json` contains real output from a locally built binary at the repository's
canonical VERSION. It was produced
against a fresh bare repository from `tools/transcript-fixture/library/` and a fresh
`consumer-app` from `tools/transcript-fixture/consumer/` with these four displayed commands:

```sh
git-a2a init
git-a2a add https://github.com/neprel/git-a2a-demo-acme-lib
git-a2a who acme-lib-utils --intent change
git-a2a status
```

`tools/transcript-generate.sh` creates the bare repository and uses process-scoped Git
`insteadOf` settings for npm's SSH form and the displayed HTTPS module URL. npm, uv, and Go resolve
the dependency from that local bare repository: npm and uv refresh their lock files, and an
offline `go mod tidy` verifies the Go module path. No machine path reaches the output. During the
run, `python3 -m http.server` serves the two committed v1.0 cards under
`tools/transcript-fixture/cards/`, making the `AGENTS` result genuinely `up`.

The JSON stores each command's stdout and stderr verbatim, including line endings and alignment
spaces. Rendering splits only the newline delimiters and gives every output line
`white-space: pre` in JetBrains Mono. The verdict-only `who` stderr remains captured but is not
displayed, as required by the design. The HTML contains the same finished transcript for
no-JavaScript and `file://` operation. `site-check` runs the fixture afresh and compares every
captured line, normalizing only the dependency commit hash; it also rejects warnings or an
unhealthy result.

Set `SITE_NET=1` when running the transcript generator to reproduce the same four-command session
against the public demo repositories. The default remains the byte-equivalent local bare fixture
so ordinary CI does not depend on the network.

## External links

The site links only to `https://git-a2a.com` and these GitHub project locations:

- `https://github.com/neprel/git-a2a`
- `https://github.com/neprel/git-a2a/blob/main/spec/README.md`
- `https://github.com/neprel/git-a2a/blob/main/docs/cli.md`
- `https://github.com/neprel/git-a2a/releases`
- `https://github.com/neprel/git-a2a-demo-acme-lib`
- `https://github.com/neprel/git-a2a-demo-acme-app`
