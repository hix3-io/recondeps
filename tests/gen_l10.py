#!/usr/bin/env python3
"""L10 — the vicious level. Three sub-scenarios the earlier tool/logic can't handle:

  10a  declared sourceMappingURL points at a DEAD (404) host, but the real map
       sits next to the file at <url>.map  → requires convention fallback.
  10b  deeply NESTED chunks: entry → a → b → c, private dep only at depth 3.
  10c  exposed INTERNAL REGISTRIES (Artifactory / Nexus / Verdaccio / GH Packages)
       plus @scope:registry= bindings — a supply-chain finding in itself.
"""
import json, os

ROOT = os.path.join(os.path.dirname(__file__), "www-l10")

def w(path, content):
    full = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    open(full, "w").write(content)

# ---------- 10a: dead declared map, live fallback ----------
w("level10a/index.html", '<!doctype html><html><body><script src="app.js"></script></body></html>')
w("level10a/app.js",
  "(function(){var a=1})();\n"
  "//# sourceMappingURL=https://cdn.dead-host.invalid/does-not-exist.map\n")  # 404 host
w("level10a/app.js.map", json.dumps({
    "version": 3, "file": "app.js",
    "sources": [
        "webpack://app/./src/main.ts",
        "webpack://app/./node_modules/@ghost-corp/secret-core/src/index.ts",
        "webpack://app/./node_modules/@ghost-corp/internal-auth/dist/index.js",
        "webpack://app/./node_modules/react/index.js",
    ],
    "sourcesContent": [None, None, None, None], "names": [], "mappings": "",
}))

# ---------- 10b: deeply nested chunks (private dep at depth 3) ----------
w("level10b/index.html", '<!doctype html><html><body><script src="entry.js"></script></body></html>')
w("level10b/entry.js", 'var n={1:"a.chunk.js"};import("./a.chunk.js");import "@public-scope/ui";\n')
w("level10b/a.chunk.js", 'var n={2:"b.chunk.js"};import("./b.chunk.js");const _=require("lodash");\n')
w("level10b/b.chunk.js", 'var n={3:"c.chunk.js"};import("./c.chunk.js");\n')
w("level10b/c.chunk.js",
  'const secret=require("@deep-corp/level3-private");\n'
  'export const g=require("@deep-corp/nested-gateway");\n')

# ---------- 10c: exposed internal registries ----------
w("level10c/index.html", '<!doctype html><html><body><script src="config.js"></script></body></html>')
w("level10c/config.js",
  'window.__NPM_CONFIG__ = {\n'
  '  "@corp:registry": "https://artifactory.corp.local/api/npm/npm-internal/",\n'
  '  "@corp-legacy:registry": "https://npm.pkg.github.com",\n'
  '  "registry": "https://nexus.internal:8081/repository/npm-all/"\n'
  '};\n'
  '// build-time leftovers\n'
  'const mirror = "https://verdaccio.dev.corp.local:4873/";\n'
  'const azure  = "https://pkgs.dev.azure.com/acme/_packaging/feed/npm/registry/";\n'
  'const pub    = "https://registry.npmjs.org/";  // must be ignored\n'
  'const auth   = require("@corp/design-system");\n')

print("L10 fixtures under", ROOT)
