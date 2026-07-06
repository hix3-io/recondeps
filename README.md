# recondeps-ng

JavaScript **supply-chain reconnaissance** for authorized bug bounty / pentest.

It mines a target's JavaScript for package references, then verifies them against
npm to surface **dependency-confusion candidates** — and, crucially, answers the
real question the old tool never did: *is the `@scope` actually claimable?*

## Why a rewrite

The legacy `recondeps` looked capable on paper but its value features were
stubbed or broken: base64 "obfuscation detection" returned `""`, the single-URL
display was always empty, source maps were ignored, and every npm non-200 (incl.
429 rate-limits) was labelled "private" — manufacturing false targets at scale.
`recondeps-ng` is built to close those gaps. See `CHANGELOG.md`.

## How it finds what others miss

| Where a private dep can hide | How recondeps-ng gets it |
|---|---|
| Readable `import` / `require` | regex extraction with context |
| Minified bundle | `node_modules/@org/pkg` path strings |
| Lazy code-split chunk | bounded chunk-following BFS |
| **Only in the `.map`** | source-map `sources[]` mining (external + inline) |
| `require(atob(...))` / `fromCharCode` | real decoding |
| Exposed `package.json` blob | dependency-object parsing |

Then for each candidate: query npm (404 vs published vs *inconclusive*), check
whether the **scope** is occupied or claimable, and emit an exploitability verdict.

## Build

```bash
make build            # injects VERSION via ldflags -> ./recondeps-ng
make install          # copies to ~/bin
```

## Usage

```bash
# single target
./recondeps-ng -url https://target.com
./recondeps-ng -url https://target.com -json -output out.json
./recondeps-ng -url https://target.com -resolve=false      # offline, no npm calls

# mass scan (bug bounty scope)
./recondeps-ng -mass domains.txt -workers 100 -npm-rate 8
./recondeps-ng -mass domains.txt -resume                   # continue an interrupted run
```

Key flags: `-depth` (chunk-follow depth), `-max-assets` (JS cap per target),
`-npm-rate` (global registry req/s), `-github-token` (or `GITHUB_TOKEN`),
`-all-hosts`, `-cookie` (authenticated scans).

## Output

- Human report grouped by exploitability (`confirmed-claimable` first).
- `-json`: strict, machine-consumable single result.
- Mass mode: `results.ndjson` (streamed, resumable) + `high_value/<domain>.json`.

## Testing

```bash
make test     # generates 6 adversarial fixture apps, serves them, runs 14 assertions
```

## Scope

Authorized security testing only — bug bounty programs within scope, engagements
you hold authorization for, or your own lab. Verify scope ownership before acting
on any "claimable" finding.
