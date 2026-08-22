# git-a2a.com static files

This directory is the complete static package for `https://git-a2a.com`. It is published
separately by the project owner; the release workflow does not deploy it.

The host must serve these files without rewriting their contents:

| Public path | Source file |
| --- | --- |
| `/install.sh` | `install.sh`, byte-identical to the repository-root installer |
| `/install.ps1` | `install.ps1` |
| `/schema/a2amodule.v1.json` | `schema/a2amodule.v1.json` |
| `/schema/a2amodule-lock.v1.json` | `schema/a2amodule-lock.v1.json` |
| `/ext/module/v1` | `ext/module/v1/index.html` |

The optional project catalog, when published, belongs at `/.well-known/ai-catalog.json`.
Run `make installers-check` after changing an installer. The complete site and its visual
verification are documented here once the design package is implemented.
