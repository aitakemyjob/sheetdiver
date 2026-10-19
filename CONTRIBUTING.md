# Contributing

Thanks for helping improve SheetDiver! By participating you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md), and contributions are licensed under
the [MIT License](LICENSE).

## Workflow

1. For anything non-trivial, open or comment on an issue first so we can
   agree on the approach.
2. Fork, create a branch (`feat/...`, `fix/...`, `docs/...`), and keep PRs
   focused.
3. Add tests with your change: new functionality and bug fixes come with
   tests in the automated suite that fail without the change.
4. Run `make test` and `make lint` before pushing. CI must be green and a
   maintainer review is required before merging into `main`; PRs are
   squash-merged.
5. Sign off every commit (`git commit -s`). This adds a `Signed-off-by:` line
   certifying, under the [Developer Certificate of Origin](https://developercert.org),
   that you have the right to submit the change. The DCO check fails without
   it; fix existing commits with `git rebase --signoff main`.
6. Use clear commit/PR titles, e.g. `fix: trace graph misses method values`.
   Labels on the PR decide where it appears in the release notes; area labels
   are added automatically from the files you change.

## AI-assisted contributions

You're welcome to use AI tools (SheetDiver is built for them), but you are the
author of your pull request:

- **Read and understand every line you submit**, and be ready to explain it
  in review.
- **Run it before opening the PR:** `make test` and `make lint` must pass
  locally. PRs that are clearly unreviewed output (placeholder text,
  truncated files, deleted tests, code that doesn't compile) will be closed.
- **Keep the change to the issue.** Don't let a tool rewrite or reformat files
  it didn't need to touch, and never remove tests to make something pass.
- **Write the PR description yourself,** and make sure it matches the diff.
- Signing off a commit (`git commit -s`) certifies you have the right to
  submit it, whoever or whatever helped write it.

## Finding something to work on

Issues move through these labels:

| Label | Meaning |
|---|---|
| `triage` | New; a maintainer hasn't looked at it yet. Issue forms add it automatically. |
| `needs repro` | Triaged, but we need steps or details to reproduce it before anyone can fix it. |
| `ready` | Triaged and specified: [pick one up](https://github.com/aitakemyjob/sheetdiver/issues?q=is%3Aopen+label%3Aready). |

`good first issue` marks small, well-scoped `ready` issues. `area: …` labels
say which part of the app is involved, and a milestone (e.g. `v0.1.1`) means
we'd like it in that release.

To take a `ready` issue, comment that you're working on it so nobody
duplicates the effort. Open questions in an issue can be settled in the
issue thread before or during the PR.

## Setup

```sh
make tools deps   # Wails CLI, golangci-lint, govulncheck and frontend packages
make test         # everything CI runs (Go with -race, frontend typecheck + tests)
make dev          # desktop app with hot reload
```

Without WebKitGTK headers on Linux you can still develop everything: build the
headless binary (`make headless`), run `sheetdiver serve`, and run the Vite dev
server (`cd frontend && npm run dev`); it proxies `/api` and `/mcp` to
`127.0.0.1:47480`.

To work against real datasheets, `make corpus` downloads the test corpus
(`testdata/corpus/manifest.json`: TI, ST, Microchip, Diodes, Infineon, AOS,
Espressif…) into your cache, and `make test-corpus` checks extraction against
it. `sheetdiver inspect some.pdf` prints everything extraction finds in a PDF
without importing it, which is the fastest loop when improving the extractor.
Point a development build at a scratch library with
`SHEETDIVER_LIBRARY=/tmp/lib SHEETDIVER_CONFIG=/tmp/c.json`.

## Conventions

- **Go:** `make lint` clean (golangci-lint, configured in `.golangci.yml`). Business logic belongs in
  `internal/core`; `api` and `mcpserver` stay thin. `internal/extract` is
  pure (it works on `pdf.Document` values) and `internal/calc` is pure SI
  math, so both are unit tested without the PDF engine. Integration tests use
  the synthetic datasheet from `internal/testutil`; never commit
  manufacturer PDFs.
- **Extraction changes:** add the datasheet that motivated the change to
  `testdata/corpus/manifest.json` (link, checksum, and expectations any
  revision should meet), and keep `make test-corpus` green.
- **TypeScript:** strict mode, `npm run lint` (ESLint) clean with no warnings. Keep pure logic in `src/lib`
  with `*.test.ts` beside it; views consume the store and `api`.
- **Documentation:** every Go function and method has a doc comment that
  starts with its name (`docs_test.go` enforces this in CI), and every
  TypeScript function, component and API/store method has a JSDoc comment.
- **JSON contract:** Go structs in `core`/`library`/`calc` are mirrored in
  `frontend/src/lib/types.ts`. Change both together, and return empty slices
  rather than `nil` for lists the UI iterates.
- **MCP tools:** every tool needs a description written for an LLM, a test in
  `internal/mcpserver/mcpserver_test.go` and an entry in `docs/MCP.md`.
- **Calculators:** add the input struct and function in `internal/calc`
  with hand-checked tests, register the kind in `core.CalcKinds`, and map
  inputs to parameter keys in `core.fills` so they fill from datasheets.

## Branding

The mark is drawn in code, twice: `frontend/src/components/Logo.tsx` (the
UI) and `scripts/brand/main.go` (raster assets). After changing it, update
both and run `make icon` to regenerate `build/appicon.png` (desktop icons and
Linux packages), `frontend/public/logo.png`, `frontend/public/favicon.png`
and `docs/assets/logo.png` (README).

## Releases

Move the *Unreleased* entries in `CHANGELOG.md` under the new version, then
run `make release VERSION=vX.Y.Z` from a clean `main`. It runs the tests,
tags and pushes; the Release workflow builds every platform with
`scripts/build.sh` / `scripts/build.ps1` and publishes the GitHub release.
See the README's *Releasing* section for signing secrets.
