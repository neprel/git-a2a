# Public polyglot demo

The public demo is a small library/consumer pair with no private infrastructure:

- [`neprel/git-a2a-demo-acme-lib`](https://github.com/neprel/git-a2a-demo-acme-lib) publishes
  the same three utility functions in TypeScript, Python, and Go. Its manifest declares all
  three exports, two agents, working GitHub Issue contacts, static A2A cards, and policy.
- [`neprel/git-a2a-demo-acme-app`](https://github.com/neprel/git-a2a-demo-acme-app) consumes one
  resolved library commit through npm, uv, and Go. Its lock, managed `AGENTS.md` roster, and CI
  are committed as an executable proof.

Inspect the library manifest and surface first, then the consumer manifest, lock, native package
files, roster, and CI. From a fresh consumer clone, run:

```sh
git-a2a who acme-lib-utils --intent change
git-a2a show acme-lib-utils --surface
git-a2a status
git-a2a update --check
```

The longer lifecycle, including `set`, `pin`, card/catalog export, and deliberate issue delivery,
is documented in each demo repository README.
