# recondeps

JavaScript **supply-chain reconnaissance** for authorized bug bounty / pentest.

It mines a target's JavaScript for package references, then verifies them against
npm to surface **dependency-confusion candidates** — and, crucially, answers the
real question the old tool never did: *is the `@scope` actually claimable?*

## Design goals

Naive dependency scanners manufacture false targets at scale: they ignore source
maps, don't really decode obfuscated `require`s, and label *every* npm non-200
(including 429 rate-limits) as "private". recondeps is built to avoid those traps —
it decodes what it claims to decode, distinguishes *published / 404 / inconclusive*,
and only flags a scope when it can reason about claimability. See `CHANGELOG.md`.

## How it finds what others miss

| Where a private dep can hide | How recondeps gets it |
|---|---|
| Readable `import` / `require` | regex extraction with context |
| Minified bundle | `node_modules/@org/pkg` path strings |
| Lazy code-split chunk | bounded chunk-following BFS |
| **Only in the `.map`** | source-map `sources[]` mining (external + inline) |
| `require(atob(...))` / `fromCharCode` | real decoding |
| Exposed `package.json` blob | dependency-object parsing |

Then for each candidate: query npm (404 vs published vs *inconclusive*), check
whether the **scope** is occupied or claimable, and emit an exploitability verdict.

## Install

```bash
go install github.com/hix3-io/recondeps@latest
```

Or build from source:

```bash
make build            # injects VERSION via ldflags -> ./recondeps
make install          # copies to ~/bin
```

## Usage

```bash
# single target
./recondeps -url https://target.com
./recondeps -url https://target.com -json -output out.json
./recondeps -url https://target.com -resolve=false      # offline, no npm calls

# mass scan (bug bounty scope)
./recondeps -mass domains.txt -workers 100 -npm-rate 8
./recondeps -mass domains.txt -resume                   # continue an interrupted run
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
