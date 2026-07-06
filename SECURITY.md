# Security & responsible use

ReconDeps is a **passive** supply-chain reconnaissance tool: it fetches a target's public
JavaScript and extracts referenced npm package scopes to surface dependency-confusion / supply-chain
attack surface. It does not exploit anything.

- Use it **only** against assets you are authorized to assess (your own, or a bug-bounty program's
  in-scope targets).
- **Never commit scan output or target lists.** Discovered private package names and target domains
  reveal a third party's attack surface — keep them out of the repo (see `.gitignore`).
- No API keys are stored in this repository.
