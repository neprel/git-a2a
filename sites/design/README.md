# Handoff: git-a2a landing page

## Overview
The marketing site for **git-a2a** (git-a2a.com): a single-page landing plus two small utility
pages. git-a2a is an open standard (`a2amodule.yml`) plus a CLI for a micro-agent architecture —
every git repository is a module owned by one or more AI agents, and dependencies are taken on
agents at developer time.

The page has to make three things clear in 20 seconds:
1. You depend on a module *and its owner* — code, how to import it, and whom to ask.
2. It is language-agnostic: one command wires npm, PyPI, Go, … at one commit.
3. It plugs into what you already use: git, package managers, AGENTS.md, the A2A protocol.

Audience: developers and people running fleets of coding agents across many repositories.
Voice: precise, calm, engineering-grade, a little dry. No hype words, no exclamation marks.
Code is first-class content, not decoration.

## About the Design Files
The two HTML files in this bundle are **design references**. They are prototypes showing the
intended look and behavior — not production code to copy verbatim. The task is to **recreate
these designs in the target environment** using its established patterns. The design is
deliberately buildable as **static HTML + CSS** with a small amount of vanilla JS; if there is no
existing codebase, plain static files (or a static-site generator) is the right choice. Do not
introduce a framework for this page.

Note: the prototypes are authored in a component format that renders through a small runtime and
uses inline styles throughout. That is an artifact of the design tool, not a recommendation.
**In the real implementation, use one external stylesheet with the CSS custom properties below
and normal classes.**

## Fidelity
**High-fidelity.** Final colors, typography, spacing, copy and interactions. Recreate the UI
faithfully. Every hex value, font size, line-height and letter-spacing in this document is the
intended value.

---

## Screens / Views

### 1. Landing page — `/`

Container: content max-width **1120px**, centered, horizontal gutter **24px** (20px at 375px).
Sections are separated by a **1px `--line` top border**; alternating sections use `--bg2` as their
background (Idea, Manifest, Install, footer) and the rest use `--bg`. Vertical section padding
**72px** (48px on mobile). Prose max-width **620px**.

#### Header (sticky)
- Height **60px**, `position:sticky; top:0; z-index:50`, background `--bg` at 88% opacity with
  `backdrop-filter: blur(10px)`, 1px `--line` bottom border.
- Left: logo mark (20×20 SVG) + wordmark, gap 9px. Wordmark is JetBrains Mono 15px,
  `letter-spacing:-.02em`: `git-` at weight 400 in `--fg3`, `a2a` at weight 700 in `--fg`.
- Center: nav links, 13.5px, `--fg2`, gap 20px — Idea, Commands, Manifest, Install, Extension.
  The first four are same-page anchors (`#idea`, `#do`, `#manifest`, `#install`) with
  `html { scroll-behavior: smooth }`. Extension links to `/ext/module/v1`.
- Right: "GitHub" link, 13.5px, `--fg2` → `https://github.com/neprel/git-a2a`.

#### Hero
- Padding: 88px top, 72px bottom. Two stacked blocks, gap 56px.
- Eyebrow pill: inline-flex, 5px/11px padding, 1px `--line` border, radius 999px, JetBrains Mono
  11.5px, `--fg2`; leading 5px `--acc` dot. Text: "an open standard + a CLI · micro-agent architecture".
- H1: `clamp(34px, 5.2vw, 58px)`, line-height **1.06**, `letter-spacing:-.035em`, weight 600,
  max-width 720px.
- Sub-line: 19px / 1.55, `--fg2`, max-width 600px.
- Actions: gap 12px. Primary "Install" (anchor to `#install`) — height 42px, padding 0 20px,
  radius 8px, background `--acc`, color `--accfg`, 14.5px weight 500, hover
  `filter:brightness(1.08)`. Secondary "Read the spec" — same metrics, 1px `--line2` border,
  color `--fg`, hover `background:--bg2`; links to the spec on GitHub.
- Terminal block: see the Terminal component below. Radius 12px, 1px `--line` border,
  shadow `0 1px 2px rgba(0,0,0,.04), 0 12px 32px -12px rgba(0,0,0,.25)`, min-height 236px.

#### Section 2 — "The idea in one diagram"
H2 + a 15.5px `--fg2` lead, then a panel: `--bg`, 1px `--line`, radius 14px, padding
`clamp(20px, 3vw, 36px)`.

Inside, two symmetric card columns flank a flow plane. Each card column is `minmax(210px,1fr)`;
the flow plane is `minmax(180px,.9fr)`; gap is `clamp(14px, 2vw, 28px)`.

- **Library column:** a `LIBRARY REPO` card titled `acme-lib-utils`, with `a2amodule.yml` and
  `surface/` chips; a vertical rule labelled `owns`; then an `OWNING AGENT` card titled
  `acme-lib-utils · owner`, with A2A card, chat, and issue chips.
- **Consumer column:** a `CONSUMER REPO` card titled `consumer-app`, with `a2amodule.yml`,
  `package.json`, `pyproject.toml`, `go.mod`, and the dashed `a2amodule.lock · AGENTS.md` chip;
  a vertical rule labelled `owns`; then a `CONSUMER AGENT` card titled `consumer-app · agent`,
  with `AGENTS.md roster` and `whom to ask` chips.
- **Code flow:** one accent arrow points from consumer to library. Its label is
  `depends on · one locked commit`; the secondary line is
  `branch 'main' · tag 'v1.2.3' · @9f2c1ab`.
- **Agent flow:** two neutral arrows align with the agent cards. `ask: question / change / bug`
  points consumer → owner; `answer · deliver` points owner → consumer.
- **Caption:** "Repositories reference code. Agents talk. git-a2a keeps both wired."

Boxes retain the established 1px `--line2`, 10px radius, `--bg2`, 14px padding treatment; chips
retain the established file and agent tokens. Flow labels are centered JetBrains Mono 10px.

Accessibility: the grid wrapper carries `role="img"` and this `aria-label` verbatim —
"Diagram: consumer-app depends on one locked commit of acme-lib-utils. Each repository is owned
by an agent. The consumer agent uses the AGENTS.md roster to ask the owning agent questions,
request changes, report bugs, and receive answers or delivered changes."

**Responsive:** at 760px and below, stack the library pair, the flow plane, then the consumer pair.
Arrows become vertical connectors and the diagram must not create horizontal scrolling at 375px.

#### Section 3 — "What you can do"
Six cards in `grid-template-columns: repeat(auto-fit, minmax(288px, 1fr))` with **`gap:1px` over a
`--line` background** and a 1px `--line` border, radius 12px, `overflow:hidden` — so the gaps read
as hairline rules. Each card: `--bg`, padding 24px, hover `background:--bg2`. Body copy 15px / 1.5;
below it a command block — `display:block`, mono 12.5px, `--fg2`, `--code` background, 1px `--line`,
radius 6px, padding 8px 10px, `overflow-x:auto`, `white-space:pre`.

Cards, in order (copy and commands verbatim):
1. "Wire a git dependency into every package manager you use, at one commit." — `git-a2a add ssh://git@github.com/acme/lib-utils.git`
2. "Know who owns a dependency and how they want to be asked." — `git-a2a who acme-lib-utils --intent change`
3. "Put a generated roster of dependencies and owners into AGENTS.md." — `git-a2a sync`
4. "Send the request where the owner said to." — `git-a2a contact acme-lib-utils --intent change`
5. "Publish and verify A2A agent cards and an ARD catalog." — `git-a2a card export` / `git-a2a catalog export` (two lines in one block)
6. "See liveness and drift in one line per dependency." — `git-a2a status`

#### Section 4 — "The manifest"
H2 + lead: "One file at the repository root. What the module is, how to import it, who owns it,
and what consumers may do."

Two columns, `repeat(auto-fit, minmax(300px, 1fr))`, gap 28px, `align-items:start`:

- **Left:** the code block. 1px `--line`, radius 12px, `--code` background. Header bar: 9px/14px
  padding, 1px `--line` bottom border, filename `a2amodule.yml` in mono 11.5px `--fg2`, copy button
  right-aligned. `<pre>` padding 16px, mono **12.5px / 1.75**, `overflow-x:auto`.
  Syntax colors: keys `--fg3`, values `--fg`, prose/URLs `--fg2`. The three callout keys
  (`exports:`, `agents:`, `contacts:`, `policy:`) are `--acc` at weight 500 on an `--accsoft`
  background with radius 3px. Content is the manifest listed under **Assets & content** below.
- **Right:** three callouts, gap 14px. Each is a **2px `--acc` left border** with 16px left padding:
  a mono 12.5px `--acc` title, then 14.5px / 1.55 `--fg2` body.
<!-- generated-facts:design-export:start -->
  - `exports` — "How to import the module in each ecosystem. Native ecosystems today: npm, uv/PyPI, Go, Cargo, SwiftPM, Pub, Bundler, Composer, Mix, Cabal/Stack, Zig, Clojure, Nix."
<!-- generated-facts:design-export:end -->
  - `agents + contacts` — "Which agents own which part of the module, and how each one wants to be contacted per kind of request — a question, a change request, a bug."
  - `policy` — "What consumers may and may not do, and which agent handles which intent. `a2amodule.lock` records what was resolved."

**Responsive:** the code block sits above the callouts at 375px. It must scroll horizontally
inside itself; the page must never scroll sideways.

#### Section 5 — "Works with what you have"
<!-- generated-facts:design-works:start -->
Generated chip rows use a 24px gap. Row labels use the existing mono micro style; chips use the existing pill tokens.
- **NATIVE ECOSYSTEMS** — npm · uv/PyPI · Go · Cargo · SwiftPM · Pub · Bundler · Composer · Mix · Cabal/Stack · Zig · Clojure · Nix
- **BUILD SYSTEMS** — CMake · Gradle · Maven · MSBuild · Meson
- **AGENT HARNESSES** — Claude Code · Codex · Cursor · GitHub Copilot · Gemini CLI · OpenCode · Hermes Agent · OpenClaw
- **DISTRIBUTION CHANNELS** — Go · Go zero-install · macOS/Linux installer · Windows installer · Homebrew · Scoop · npm · PyPI with uv · PyPI with pipx · Container · Nix flake
- **CONTACT KINDS** — a2a · github-issue · gitlab-issue · gitea-issue · bitbucket-issue · azure-boards · http · exec · email · jira · mattermost · slack · discord · telegram · teams · url
- **STANDARDS** — A2A · AGENTS.md · Agent Skills · ARD catalogs · MCP · MCP Registry
<!-- generated-facts:design-works:end -->

Text only. No third-party logos anywhere on the page.

#### Section 6 — "Install"
H2 + lead: "One static binary, no runtime. Checksums and SBOM on every release."
Panel max-width 820px, `--bg`, 1px `--line`, radius 12px, `overflow:hidden`.
- Tab strip: `--bg2` background, 1px `--line` bottom border, `overflow-x:auto`. Each tab: mono
  12.5px, padding 12px 16px, `white-space:nowrap`, 2px bottom border — `--acc` + color `--fg` when
  selected, transparent + `--fg3` otherwise.
- Panel body: padding 20px. Each command is a code row (see Code block component), 10px apart.
  A 13.5px `--fg2` note sits 14px below.

Tabs, commands and notes (all commands verbatim):
| Tab | Commands | Note |
| --- | --- | --- |
| `curl / irm` | `curl -fsSL https://git-a2a.com/install.sh | bash` · `irm https://git-a2a.com/install.ps1 | iex` | "The second line is for Windows PowerShell." |
| `go` | `go install github.com/neprel/git-a2a/cmd/git-a2a@latest` · `go run github.com/neprel/git-a2a/cmd/git-a2a@latest` | "One static binary, no runtime." |
| `brew / scoop` | `brew install neprel/tap/git-a2a` · `scoop install git-a2a` | "One static binary, no runtime." |
| `npx / uvx` | `npx git-a2a` · `uvx git-a2a` | "Runs without installing." |
| `deb / rpm / apk / Docker` | `docker run --rm ghcr.io/neprel/git-a2a status` | "deb, rpm and apk packages are on the releases page. Checksums and SBOM on every release." |

The last tab's note should link "releases page" to `https://github.com/neprel/git-a2a/releases`.

#### Section 7 — "How it relates"
Three columns, `repeat(auto-fit, minmax(260px, 1fr))`, gap 32px. H3 15px weight 600, body
14.5px / 1.6 `--fg2`.
- **A2A protocol** — "git-a2a uses native A2A agent cards and adds one extension. It is not affiliated with the A2A project."
- **AGENTS.md** — "git-a2a writes a managed block into it. It does not replace it."
- **Package managers** — "git-a2a drives them. It never replaces them."

#### Footer
`--bg2`, 1px `--line` top border, padding 48px 24px,
`repeat(auto-fit, minmax(220px, 1fr))`, gap 32px, `align-items:start`.
- Col 1: wordmark (mono 14px) + 13px / 1.6 `--fg3` line, max-width 280px:
  "A2A is a Linux Foundation project; git-a2a is an independent open-source tool."
- Col 2 (13.5px links, gap 9px): GitHub · Spec · CLI reference
- Col 3: Releases · A2A extension · Schema
- Col 4: "License MIT" in `--fg3`

Link targets: `https://github.com/neprel/git-a2a`, `…/blob/main/SPEC.md`,
`…/blob/main/docs/cli.md`, `…/releases`, `/ext/module/v1`, `/schema/`.

---

### 2. Utility page — `/ext/module/v1`
Single column, max-width 760px, padding 72px 24px 96px. Same header/footer as the landing.
- Back link "← git-a2a" (mono 12.5px, `--fg3`).
- H1 34px, `letter-spacing:-.03em`, weight 600: "A2A extension".
- Lead 16px / 1.6 `--fg2`: "One extension on native A2A agent cards. It declares which module an
  agent speaks for, and in what capacity."
- URI block: `--code`, 1px `--line`, radius 10px, padding 14px 16px. Micro-label "URI" (mono 11px
  `--fg3`), then `https://git-a2a.com/ext/module/v1` in mono 13.5px.
- H2 18px "Params", then a `<dl>` as a 2-column grid `minmax(0,140px) minmax(0,1fr)` with
  `gap:1px` over `--line`, 1px `--line` border, radius 10px, `overflow:hidden`. Cells: `--bg`,
  padding 12px 14px. `<dt>` mono 13px in `--acc`; `<dd>` 14.5px `--fg2`.
  - `module` — "The module id, as declared in `a2amodule.yml`."
  - `repository` — "The git URL the module lives at."
  - `role` — "What the agent is for this module — for example `owner` or `spec`."
  - `scope` — "The part of the module the agent answers for."
  - `ref` — "The commit or ref the declaration applies to."
- Closing line, 14.5px `--fg2`: "Export and verify cards with `git-a2a card export`. [JSON schema](/schema/)."

### 3. Utility page — `/schema/`
Same shell and metrics.
- H1 "Schema"; lead: "JSON Schema for the manifest and the lock file. Point your editor at these
  for completion and validation."
- A list of two link rows in a `gap:1px` over `--line` container, radius 10px: each row is
  `display:flex; justify-content:space-between; align-items:baseline`, `--bg`, padding 16px, hover
  `--bg2`. Left: filename in mono 13.5px, color `--fg`. Right: description in 13.5px `--fg3`.
  - `a2amodule.v1.json` → "the manifest" → `https://git-a2a.com/schema/a2amodule.v1.json`
  - `a2amodule-lock.v1.json` → "what was resolved" → `https://git-a2a.com/schema/a2amodule-lock.v1.json`
- Closing line, 14.5px `--fg2`: "Add `# yaml-language-server: $schema=https://git-a2a.com/schema/a2amodule.v1.json` to the top of your manifest."

### Non-pages
`/install.sh` and `/install.ps1` are plain files served as `text/plain`, not HTML pages.

---

## Interactions & Behavior

**Only three things need JavaScript. Nothing else on this site does.**

### 1. Hero terminal playthrough
Types the four commands once on load, revealing output lines after each command. This is the one
piece of motion on the page and it earns its place — it tells the whole product story.

Sequence (prompt `$ ` in `#6f6f66`, `user-select:none`; command text `--termfg`; comments `#6f6f66`;
plain output `#d9d9d1`; dim output `#8f8f85`; the resolved commit line in `--acc`):

1. `git-a2a init`  + trailing comment `# describe this repository`
   - `wrote      a2amodule.yml` (plain)
   - `module     consumer-app` (dim)
2. `git-a2a add ssh://git@github.com/acme/lib-utils.git`
   - `fetched    manifest  acme-lib-utils` (dim)
   - `resolved   9f2c1ab` (**accent**)
   - `wired      package.json  pyproject.toml  go.mod` (plain)
   - `updated    a2amodule.lock  AGENTS.md` (dim)
3. `git-a2a who acme-lib-utils --intent change`
   - `acme-pm    role spec` (plain)
   - `change ->  github-issue  acme/lib-utils  [change-request]` (dim)
4. `git-a2a status`
   - `acme-lib-utils   upstream up   wiring ok   agents live   @9f2c1ab` (plain)

Timing: 300ms initial delay; **19ms per character** while typing; 260ms pause after a command
completes; **170ms between output lines**; 420ms blank line between command groups; a blinking
caret is left at a final empty prompt when the sequence finishes.
Caret: 7px × 15px `--acc` block, `animation: blink 1.05s step-end infinite` (0–49% opaque,
50–100% transparent), `vertical-align:-3px`.

- A **replay** button in the terminal header re-runs the sequence from empty.
- Under `prefers-reduced-motion: reduce`, **skip the animation entirely** and render the finished
  transcript immediately.
- The finished transcript is also the correct no-JS fallback: render it server-side/statically and
  let the script clear and re-type it on load. That way the block is never empty.
- Reserve the height (min-height 236px) so nothing shifts as lines appear.

### 2. Copy buttons
`navigator.clipboard.writeText()`. The button label swaps from `copy` to `copied` for **1400ms**,
then reverts. Wrap the label in `aria-live="polite"`. The `$ ` prompt is `user-select:none` so a
manual selection yields the bare command. The terminal's copy button copies the four commands,
newline-separated, without output. The manifest's copy button copies the full YAML.
Copy affordances are **always visible** — never hover-only.

### 3. Install tabs
Show/hide panels. `role="tablist"` on the strip, `role="tab"` + `aria-selected` on each button,
`role="tabpanel"` on the body, roving `tabindex` with left/right arrow-key movement. Default tab is
`curl / irm`. Reflecting the active tab in the URL (`?install=go`) is optional.

### Everything else
Hover states are pure CSS. Anchor navigation is native + `scroll-behavior:smooth`. There is no
theme toggle (see below), no scroll-triggered animation, no carousel, no modal.

### Theme
**Light only.** The site ships one theme. Do not build a toggle. Dark-theme token values are
documented below because the design was explored in both — treat them as a future option, not part
of this build.

### Responsive
Breakpoints are handled by `auto-fit minmax()` rather than media queries wherever possible. Two
places need real attention at 375px:
- the diagram (three columns stack; the arrow column becomes vertical),
- the manifest block (moves above its callouts; scrolls horizontally inside itself).
Long commands scroll inside their block. `body` must never scroll sideways — verify
`document.body.scrollWidth === window.innerWidth` at 375px.

### Accessibility
- Contrast AA in both themes: `--fg` on `--bg` ≈ 15:1; accent 5.6:1 on light.
- Focus ring on every interactive element: `outline: 2px solid var(--acc); outline-offset: 2px`,
  via `:focus-visible`. Do not remove it.
- The diagram carries `role="img"` and the full `aria-label` quoted above.
- Selection color: `::selection { background: rgba(10,108,129,.16) }`.
- Buttons are real `<button>`s; links are real `<a>`s.

## State Management
Trivial — three pieces of local UI state, no data fetching, no router:
- `terminalLines: Line[]` — driven by the playthrough timers; `Line = { prefix, text, comment, color, caret }`.
- `activeInstallTab: string` — one of the five tab ids; default `sh`.
- `copiedKey: string | null` — which copy button is currently showing `copied`; cleared after 1400ms.

Clear all pending timers on teardown so a replay never races a previous run.

---

## Design Tokens

Ship these as CSS custom properties on `:root` in one stylesheet.

### Colour — light (shipping theme)
| Token | Value | Use |
| --- | --- | --- |
| `--bg` | `#ffffff` | page background |
| `--bg2` | `#f7f7f5` | alternating sections, boxes, hover |
| `--bg3` | `#efefec` | reserved, deepest surface |
| `--fg` | `#111110` | headings, body |
| `--fg2` | `#4b4b46` | secondary body, nav |
| `--fg3` | `#7a7a72` | micro-labels, meta |
| `--line` | `#e3e3de` | default hairline |
| `--line2` | `#cdcdc5` | emphasised hairline, box borders |
| `--code` | `#fbfbf9` | code-block background |
| `--term` | `#171715` | terminal background (dark in both themes) |
| `--termfg` | `#ecece6` | terminal text |
| `--acc` | `#0a6c81` | links, primary button, arrows, callouts, caret |
| `--accfg` | `#ffffff` | text on `--acc` |
| `--accsoft` | `rgba(10,108,129,.09)` | callout key highlight |
| selection | `rgba(10,108,129,.16)` | `::selection` |

Accent contrast on `--bg`: **5.6:1** — AA for body text, AAA for large text.
The accent appears only on: links, the primary button, the three commit-pin arrows, the manifest
callouts, the terminal's `resolved` line, the terminal caret, and the eyebrow dot.

### Colour — dark (documented, not shipping)
`--bg #0d0d0c` · `--bg2 #151514` · `--bg3 #1d1d1b` · `--fg #f3f3ef` · `--fg2 #b3b3ab` ·
`--fg3 #83837a` · `--line #262623` · `--line2 #37372f` · `--code #131312` · `--term #080807` ·
`--acc #4ec7de` · `--accfg #171715` · `--accsoft rgba(78,199,222,.12)`.
An amber alternative was explored and rejected: light `#9a5200`, dark `#f0a54a`.

### Type
Two families only. `display=swap`, latin subset. No third family, no icon font.
- **IBM Plex Sans** — 400, 500, 600 — all prose.
- **JetBrains Mono** — 400, 500, 700 — code, micro-labels, chips, the wordmark.

| Role | Size / line-height / tracking / weight |
| --- | --- |
| h1 | `clamp(34px, 5.2vw, 58px)` / 1.06 / `-.035em` / 600 |
| h1 (utility pages) | 34px / `-.03em` / 600 |
| h2 | 26px / `-.02em` / 600 |
| h3 | 15px / 600 |
| lead | 19px / 1.55 — `--fg2` |
| section lead | 15.5px — `--fg2` |
| body | 15px / 1.5 |
| small | 13.5px / 1.6 |
| nav / footer link | 13.5px |
| code block (terminal) | 13px / 1.85 |
| code block (single command) | 13px / 1.6 |
| code block (YAML) | 12.5px / 1.75 |
| inline code, chip | 12.5px |
| micro label | 11px / `.06em` / uppercase |
| arrow pin label | 10px — `--acc` |

### Spacing — 4px base
`4` hairline gaps · `8` chip and button gaps · `10` diagram arrow gaps · `12` stacked code rows ·
`16` code padding · `24` card padding and page gutter · `32` heading→content · `44` intra-section ·
`56` hero stack · `72` section padding (48 on mobile) · `88` hero top.
Content max-width `1120`; utility-page max-width `760`; prose max-width `620`; install panel `820`.

### Radii
`5` small chip · `6` inline code / file chip · `7` small control · `8` button, single code row ·
`10` inner panel · `12` panel / card grid · `14` diagram panel · `999` pill.

### Shadows
Only one, on the hero terminal: `0 1px 2px rgba(0,0,0,.04), 0 12px 32px -12px rgba(0,0,0,.25)`.
Everything else is defined by hairlines. Do not add shadows.

### Code-block spec
- Terminal: bg `--term` (fixed dark in both themes), radius 12px, header bar 10px/14px with three
  9px `#3a3a36` dots and the title `consumer-app — git-a2a` in mono 11.5px `#8a8a80`; body padding
  18px 18px 22px; min-height 236px. Header buttons: 1px `rgba(255,255,255,.12)`, color `#a8a89e`,
  mono 11px, padding 3px 9px, radius 6px; hover color `#ecece6`, border `rgba(255,255,255,.28)`.
- Single command row: flex, 1px `--line`, radius 8px, `--code` bg; `<pre>` `flex:1 1 auto; min-width:0`,
  padding 12px 14px, `overflow-x:auto`; copy button is a flex-none sibling with a 1px `--line`
  left border, mono 11px, `--fg3`, padding 0 12px, hover `--fg` on `--bg2`.
- `overflow-x:auto` on every block. Never wrap a command. Never let the page scroll sideways.
- `min-width:0` on the `<pre>` inside any flex parent, or the block will blow out its container.

---

## Assets & content

### Wordmark and mark
`mark.svg` — 20×20 grid, two commit nodes merging into one owned node:

```svg
<svg width="20" height="20" viewBox="0 0 20 20" fill="none">
  <path d="M3.5 6.5h5.2M3.5 13.5h5.2M11.3 10h5.2" stroke="var(--line2)" stroke-width="1.4"/>
  <path d="M8.7 6.5 11.3 10 8.7 13.5" stroke="var(--acc)" stroke-width="1.4"/>
  <circle cx="3" cy="6.5" r="2" fill="var(--fg)"/>
  <circle cx="3" cy="13.5" r="2" fill="var(--fg)"/>
  <circle cx="17" cy="10" r="2" fill="var(--acc)"/>
</svg>
```

`wordmark.svg` — the mark + "git-a2a" set in JetBrains Mono, `letter-spacing:-.03em`, `git-` at 400
in `--fg3` and `a2a` at 700 in `--fg`. Convert glyphs to paths for the standalone file so it does
not depend on the webfont. No robots, no brains, no sparkles.

### Favicon set
`favicon.svg`, `favicon.ico` (16 + 32), `apple-touch-icon.png` (180). Plate is `--fg` with the mark
knocked out in `--bg` and the merge stroke + owned node in `--acc`. **Simplify as it shrinks:** at
32px drop the connector edges and keep the merge chevron; at 16px keep only the three nodes
(radius 2.6 on the 20-unit grid).

### OG image — 1200×630, text only
Plate `#0d0d0c`. Safe padding **78px** (6.5%). Top-left: the mark at 20px + wordmark in JetBrains
Mono 15px (`git-` in `#83837a`, `a2a` at 700 in `#f3f3ef`). Bottom: the headline in IBM Plex Sans
600 at **56px / 64px**, `letter-spacing:-.03em`, `#f3f3ef` —
"A git repository you can import together with the agents that own it." — and beneath it, 10px
down, one accent line in JetBrains Mono **26px** in `--acc`:
`git-a2a add ssh://git@github.com/acme/lib-utils.git`.
Same layout at 1200×600 for Twitter. No photography, no illustration.

### The manifest content (section 4, verbatim)
```yaml
schema: 1
module:
  id: acme-lib-utils
  description: Shared string utilities for Acme.
  surface: surface/
  exports:
    - { ecosystem: npm,    name: "@acme/lib-utils" }
    - { ecosystem: pypi,   name: acme_lib_utils }
    - { ecosystem: golang, name: acme.dev/lib-utils }
agents:
  - name: acme-lib-utils
    role: owner
    card: https://agents.acme.example/lib-utils/.well-known/agent-card.json
    contacts:
      - { intents: [question], kind: a2a, url: https://agents.acme.example/lib-utils/ }
      - { intents: [bug],      kind: github-issue, repo: acme/lib-utils }
  - name: acme-pm
    role: spec
    contacts:
      - { intents: [change], kind: github-issue, repo: acme/lib-utils, labels: [change-request] }
policy:
  intents: { change: spec }
  consumers: { may: [read-surface, ask, open-issue], may-not: [commit] }
```

The inline column alignment inside the flow-map entries is intentional — keep it.

### Content rules — do not break these
- Only template names: `acme-*`, `agents.acme.example`, `consumer-app`, `neprel/git-a2a`.
- No third-party logos, no "trusted by" row, no testimonials, no metrics or numbers that are not
  in this document.
- Every command shown on the page must be one of the commands in this document, verbatim.
- No hype words, no exclamation marks.

---

## Performance budget
Two font families and nothing else. No web fonts beyond those two, no hero video, no illustration,
no icon library, no framework. The whole site should be static HTML + CSS plus roughly 100 lines
of vanilla JS for the terminal, copy buttons and install tabs. Target: one HTML document per page,
one stylesheet, one script.

---

## Files
| File | What it is |
| --- | --- |
| `git-a2a Landing.dc.html` | The landing page prototype, including the two utility pages (reachable via the Extension and Schema links). Open it in a browser. |
| `git-a2a Handoff.dc.html` | Visual spec sheet: token swatches, type scale, spacing and radii, the seven components in isolation, wordmark/favicon/OG image, and the static-vs-JS summary. |
| `support.js` | Runtime required by the two prototypes. Not part of the implementation. |

Both prototypes are design references. Implement from this README; use the files to check
proportion, rhythm and motion.
