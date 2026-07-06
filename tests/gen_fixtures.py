#!/usr/bin/env python3
"""Generate progressively adversarial test apps that try to HIDE private deps.
Each level buries an @org/pkg deeper so the scanner has to work harder.
Also plants decoys (public pkgs, node builtins, relative imports) to catch
false positives.
"""
import base64, json, os, textwrap

ROOT = os.path.join(os.path.dirname(__file__), "www")

def w(path, content):
    full = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(content)
    print("wrote", path)

# ---------- Level 1: plain, readable ----------
w("level1/index.html", """<!doctype html><html><head><title>L1</title></head>
<body><script src="app.js"></script></body></html>""")
w("level1/app.js", textwrap.dedent("""
    // decoys the tool must ignore:
    const fs = require('fs');                 // node builtin
    import React from 'react';                // public
    import babel from '@babel/runtime/helpers/typeof'; // public scoped
    const local = require('./utils/helper');  // relative

    // the real target, in the open:
    const auth = require('@acme-corp/auth-internal');
    import { pay } from '@acme-corp/payment-sdk';

    // exposed manifest fragment:
    const meta = {"dependencies":{"@acme-corp/legacy-core":"1.2.3","lodash":"4.17.21"}};
"""))

# ---------- Level 2: minified, only node_modules paths survive ----------
mini = ('!function(e){var t={};function n(r){}' +
        'n.p="/static/",' +
        'var mods={' +
        '"./node_modules/@acme-corp/api-client/dist/index.js":function(e,t){},' +
        '"./node_modules/react/index.js":function(e,t){},' +
        '"./node_modules/@babel/runtime/helpers/esm/typeof.js":function(e,t){}' +
        '};}({});')
w("level2/index.html", """<!doctype html><html><body><script src="bundle.min.js"></script></body></html>""")
w("level2/bundle.min.js", mini)

# ---------- Level 3: code-split, target lives in a lazy chunk ----------
w("level3/index.html", """<!doctype html><html><body><script src="main.js"></script></body></html>""")
w("level3/main.js", textwrap.dedent("""
    // webpack-style chunk map; the tool must follow the referenced .js file
    var chunks = {12:"billing.4f2a9c.js", 13:"vendor.aa11.js"};
    function load(id){ return import("./"+chunks[id]); }
    // nothing juicy here directly, just a public import as a decoy
    import cx from '@emotion/css';
"""))
w("level3/billing.4f2a9c.js", textwrap.dedent("""
    // the private dep only appears inside the lazy chunk
    const billing = require('@acme-corp/billing-core');
    export const svc = require('@acme-corp/internal-gateway');
"""))
w("level3/vendor.aa11.js", "import _ from 'lodash'; import ax from 'axios';")

# ---------- Level 4: fully minified, name ONLY in the source map ----------
# The emitted bundle has zero readable package names. The .map reveals them.
w("level4/index.html", """<!doctype html><html><body><script src="app.min.js"></script></body></html>""")
w("level4/app.min.js",
  "(function(a,b,c){return a(b)(c)})(function(){},function(){},0);\n"
  "//# sourceMappingURL=app.min.js.map\n")
smap = {
    "version": 3,
    "file": "app.min.js",
    "sources": [
        "webpack://app/./src/index.ts",
        "webpack://app/./node_modules/@secret-org/crypto-core/src/index.ts",
        "webpack://app/./node_modules/@secret-org/telemetry/dist/index.js",
        "webpack://app/./node_modules/react-dom/index.js",
        "webpack://app/./node_modules/@babel/runtime/helpers/typeof.js",
    ],
    "sourcesContent": ["//app", "//crypto", "//telemetry", "//rd", "//babel"],
    "names": [],
    "mappings": "",
}
w("level4/app.min.js.map", json.dumps(smap))

# ---------- Level 5: obfuscated specifiers ----------
b64 = base64.b64encode(b"@shadow-org/payload").decode()
charcodes = ",".join(str(ord(c)) for c in "@shadow-org/loader")
w("level5/index.html", """<!doctype html><html><body><script src="obf.js"></script></body></html>""")
w("level5/obf.js", textwrap.dedent(f"""
    // base64-hidden module specifier
    const p = require(atob("{b64}"));
    // String.fromCharCode-hidden specifier
    const l = require(String.fromCharCode({charcodes}));
    // decoy noise the tool must not treat as packages
    var x = "a"+"b"+"c"; const y = window.location.href;
"""))

# ---------- Level 6: mixed real-world (inline source map + occupied public scope) ----------
inner = {
    "version": 3, "file": "b.js",
    "sources": ["webpack://x/./node_modules/@mui/material/styles.js",
                "webpack://x/./node_modules/@acme-corp/design-system/index.js"],
    "sourcesContent": ["//mui", "//ds"], "names": [], "mappings": "",
}
inline = base64.b64encode(json.dumps(inner).encode()).decode()
w("level6/index.html", """<!doctype html><html><body><script src="b.js"></script></body></html>""")
w("level6/b.js", "console.log(1);\n//# sourceMappingURL=data:application/json;charset=utf-8;base64," + inline + "\n")

print("\nAll fixtures generated under", ROOT)
