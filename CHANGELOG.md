# Changelog

All notable changes to recondeps-ng. Versioning is semantic (MAJOR.MINOR.PATCH).

## v1.2.0 — 2026-07-06

The "L10" hardening round — three adversarial scenarios the tool previously
couldn't handle, now covered and tested (`make test-l10`, 11 assertions).

### Added
- **Source-map fallback.** When a JS file's declared `sourceMappingURL` is
  unreachable (dead host / 404) or missing, the scanner now tries the
  convention path `<jsurl>.map`. A cheap `looksLikeSourceMap` guard avoids
  parsing SPA HTML fallbacks served with 200. (L10a)
- **Internal-registry detection** (`registry.go`, new capability). Surfaces npm
  registries referenced in the target's assets and classifies them —
  `artifactory` / `nexus` / `verdaccio` / `github-packages` / `azure` /
  `gitlab` / `internal` — from `@scope:registry=` bindings (both npmrc and
  JSON-key forms), `registry=` / `"registry":` config, and bare URLs whose host
  strongly signals a registry. Public (`registry.npmjs.org`) is ignored. A
  private registry is a supply-chain finding in itself: it names where the
  private packages live and binds a scope to a host. Shown in the report, in
  JSON (`registries[]`), and now triggers `high_value` saving in mass mode. (L10c)
- **Deep chunk following** verified to depth N via `-depth` (L10b:
  entry→a→b→c, private dep at depth 3, found with `-depth 5`). Behaviour is
  honestly bounded — the default depth of 2 misses depth-3 chunks, and that is
  asserted, so coverage limits are explicit rather than silent.

## v1.1.0 — 2026-07-06

Real-time visibility + hardening found by real-world-scale testing.

### Added
- **Real-time `-debug` tracing** across the whole pipeline: every GET with status
  and size, declared source maps, chunk refs queued, each extraction hit (with
  method), the merged package count, every npm/scope lookup and its verdict
  (`[NET]` lines), and the final per-package verdict — all streamed to stderr as
  it happens, so JSON/human stdout stays clean.
- **Hardcore test suite** (`tests/gen_hardcore.py`, `tests/hardcore.sh`,
  `make test-hard`): downloads real production libraries and builds
  - L7: a 4.8 MB / 110k-line fat bundle of real libs (three, d3, tfjs, rxjs) —
    robustness/false-positive check (finds only the 3 genuine deps, 0 noise);
  - L8: needle-in-a-haystack — 6 private deps hidden among an **8,006-source**
    map + the fat bundle (all 6 found, ~0.47 s, ~26 MB RSS; all verdicted
    `confirmed-claimable` with resolution);
  - L9: a **500k-line / 22 MB monster** (1.8 s, ~67 MB RSS).

### Fixed (both surfaced by the real-world tests)
- **Source map picked the wrong comment in vendored bundles.** A concatenated /
  vendored bundle carries several `//# sourceMappingURL=` comments (each bundled
  lib keeps its own); we took the *first* (which 404s) and missed the real map.
  Now we take the *last*, per spec — L8's 8k-source map now parses.
- **Silent truncation.** Files larger than `-max-bytes` were cut without notice,
  dropping the tail where `sourceMappingURL` and late deps live. Now emits a loud
  `[WARN]` naming the file and the cap.

## v1.0.0 — 2026-07-06

First release of the ground-up rewrite. `recondeps-ng` replaces the legacy
`recondeps` (v0.6.7, regex-on-minified) whose value features were stubbed,
buggy, or unreliable at scale. Full analysis of the old tool's gaps is what
this release is built to close.

### Added — the features that make the tool actually work
- **Source-map mining.** Fetches external and inline (`data:` base64) source
  maps and extracts real dependency names from `sources[]` paths
  (`webpack://…/node_modules/@org/pkg/…`). This is the reliable signal: names
  survive here even when the emitted bundle is fully minified. Level-4 test
  proves detection of a package that appears *only* in the `.map`.
- **Chunk following.** Bounded BFS over JS assets follows webpack code-split
  chunk references, so private deps hidden in lazy chunks are found (level 3).
- **Real obfuscation decoding.** `require(atob("…"))` and
  `String.fromCharCode(…)` specifiers are decoded (the old `decodeBase64` was a
  `return ""` stub). Level 5.
- **Scope-claimability check.** Answers the actual dependency-confusion
  question — is `@org` claimable? — via the npm scope search, instead of only
  testing whether a single package 404s.
- **Exploitability verdict** per dependency: `confirmed-claimable` / `likely` /
  `unlikely` / `not-applicable` / `unknown`.
- **Honest npm resolution.** Distinguishes 404 (absent) from 429/5xx/network
  (`unknown`) — the old tool marked every non-200 as "private", manufacturing
  false high-value targets under rate limiting.
- **Global shared resolver** with a token-bucket rate limiter and shared
  npm/scope/github caches across all workers (the old mass mode created a fresh
  scanner per domain, so its cache never hit). Uses npm's abbreviated metadata
  doc for lighter requests.
- **Checkpoint / resume** for mass scans via streaming NDJSON; interrupted runs
  continue with `-resume` instead of restarting.
- Kept `Indicators` on every finding (the old code accumulated them into a
  discarded copy).

### Fixed (vs legacy recondeps)
- Human output no longer keys on a `dep.Type == "scoped"` value that was never
  set (the old single-URL display was always empty).
- Deduplicated, merged findings across all JS + maps, keyed by installable
  package root; strongest detection method wins.
- Filenames for high-value targets are sanitized.

### Testing
- 6 progressively adversarial fixture apps (`tests/gen_fixtures.py`) that hide a
  private dep behind: plain code, minification, code-splitting, source-map-only,
  base64/charCode obfuscation, inline maps — plus decoys (node builtins,
  relative imports, public scoped orgs, code fragments).
- `tests/e2e.sh`: 14 assertions, all passing, run via `make test`.
