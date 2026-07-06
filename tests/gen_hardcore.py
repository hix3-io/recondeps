#!/usr/bin/env python3
"""Hardcore, real-world-scale fixtures.

L7  real fat app bundle: several REAL production libraries concatenated into one
    multi-MB / 100k+ line main.js. No injected private dep — measures robustness,
    false positives and performance on genuine minified+beautified code.
L8  needle in a haystack: the fat bundle + a few realistic private node_modules
    path strings + a LARGE (8k sources) real-style source map. Tests whether the
    private needles are still found and how fast a big .map parses.
L9  synthetic monster: a ~500k line JS file with thousands of require() calls
    mixing public and private scopes — pure scale/memory/time stress.
"""
import base64, json, os, sys, urllib.request

HERE = os.path.dirname(__file__)
ROOT = os.path.join(HERE, "www-hard")
CACHE = os.path.join(HERE, ".cache")
os.makedirs(CACHE, exist_ok=True)

REAL = [
    ("three.js",   "https://unpkg.com/three@0.160.0/build/three.js"),
    ("d3.js",      "https://unpkg.com/d3@7.8.5/dist/d3.js"),
    ("tf-core.js", "https://unpkg.com/@tensorflow/tfjs-core@4.17.0/dist/tf-core.js"),
    ("rxjs.js",    "https://unpkg.com/rxjs@7.8.1/dist/bundles/rxjs.umd.js"),
    ("tf.min.js",  "https://unpkg.com/@tensorflow/tfjs@4.17.0/dist/tf.min.js"),
]

def fetch(name, url):
    dst = os.path.join(CACHE, name)
    if os.path.exists(dst) and os.path.getsize(dst) > 0:
        return open(dst, "r", errors="replace").read()
    print("  downloading", url)
    req = urllib.request.Request(url, headers={"User-Agent": "fixture-builder"})
    data = urllib.request.urlopen(req, timeout=60).read().decode("utf-8", "replace")
    open(dst, "w").write(data)
    return data

def w(path, content):
    full = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "w") as f:
        f.write(content)

def main():
    print("fetching real libraries…")
    libs = {name: fetch(name, url) for name, url in REAL}
    fat = "\n".join(f"/* ===== {n} ===== */\n{c}" for n, c in libs.items())
    lines = fat.count("\n")
    print(f"fat bundle: {len(fat)//1024} KB, {lines} lines")

    # ---- L7: real fat bundle, no injection ----
    w("level7/index.html", '<!doctype html><html><body><script src="main.js"></script></body></html>')
    w("level7/main.js", fat)

    # ---- L8: needle in haystack ----
    needles = (
        '\n/* app runtime */\n'
        'var m={"./node_modules/@secret-corp/auth-core/index.js":1,'
        '"./node_modules/@secret-corp/telemetry-internal/dist/i.js":2,'
        '"./node_modules/@acme-labs/private-sdk/index.js":3};\n'
        'const a=require("@secret-corp/billing-engine");\n'
        'import x from "@acme-labs/design-tokens";\n'
    )
    w("level8/index.html", '<!doctype html><html><body><script src="app.min.js"></script></body></html>')
    w("level8/app.min.js", fat + needles + "\n//# sourceMappingURL=app.min.js.map\n")

    # big real-style source map: thousands of public node_modules + a few private
    pub_orgs = ["@babel/runtime","@mui/material","@emotion/react","@angular/core",
                "@tanstack/query-core","@sentry/browser","react-dom","lodash-es","rxjs","three"]
    sources, contents = [], []
    for i in range(8000):
        org = pub_orgs[i % len(pub_orgs)]
        sources.append(f"webpack://app/./node_modules/{org}/dist/chunk-{i}.js")
        contents.append(None)
    # hide 6 private modules among 8000
    private = ["@secret-corp/auth-core","@secret-corp/telemetry-internal",
               "@secret-corp/billing-engine","@acme-labs/private-sdk",
               "@acme-labs/design-tokens","@internal-x/crypto"]
    for j, p in enumerate(private):
        sources.insert(1234 + j*900, f"webpack://app/./node_modules/{p}/src/index.ts")
        contents.insert(1234 + j*900, None)
    smap = {"version":3,"file":"app.min.js","sources":sources,
            "sourcesContent":contents,"names":[],"mappings":""}
    w("level8/app.min.js.map", json.dumps(smap))
    print(f"L8 source map: {len(sources)} sources ({len(private)} private hidden)")

    # ---- L9: synthetic monster ----
    pub = ["react","react-dom","lodash","axios","@babel/runtime","@mui/material",
           "@emotion/react","rxjs","moment","three","d3","@angular/core"]
    priv = ["@monster-corp/core","@monster-corp/auth","@monster-corp/internal-api",
            "@shadow-labs/secret-sdk","@internal-z/payments"]
    monster = []
    monster.append('(function(){"use strict";\n')
    n = 0
    while n < 500000:
        block = n // 100
        monster.append(f'function f{block}_{n%100}(a,b){{var t=a+b;')
        if n % 37 == 0:
            monster.append(f'var p=require("{pub[n%len(pub)]}");')
        if n % 991 == 0:  # sparse private needles
            monster.append(f'var s=require("{priv[(n//991)%len(priv)]}");')
        monster.append('return t;}\n')
        n += 1
    monster.append('})();\n')
    body = "".join(monster)
    w("level9/index.html", '<!doctype html><html><body><script src="monster.js"></script></body></html>')
    w("level9/monster.js", body)
    print(f"L9 monster: {len(body)//1024//1024} MB, {body.count(chr(10))} lines, "
          f"~{500000//991} private needles")

    print("\nhardcore fixtures under", ROOT)

if __name__ == "__main__":
    main()
