package main

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// sourceMap is the subset of the Source Map v3 spec we need.
type sourceMap struct {
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
}

var reInlineMap = regexp.MustCompile(`(?m)//[#@]\s*sourceMappingURL=data:application/json[^,]*base64,([A-Za-z0-9+/=]+)`)

// packagesFromSourceMap extracts real dependency names from a source map's
// `sources` paths. This is the reliable signal: webpack/rollup keep the true
// module paths (webpack://app/./node_modules/@org/pkg/index.js) even after the
// emitted bundle is fully minified.
func packagesFromSourceMap(raw string) []extracted {
	var sm sourceMap
	if err := json.Unmarshal([]byte(raw), &sm); err != nil {
		return nil
	}
	out := map[string]extracted{}
	for _, src := range sm.Sources {
		if pkg := packageFromPath(src); pkg != "" {
			ex := out[pkg]
			ex.pkg = pkg
			ex.method = "sourcemap"
			ex.evidence = src
			out[pkg] = ex
		}
	}
	var list []extracted
	for _, v := range out {
		list = append(list, v)
	}
	return list
}

// inlineSourceMap decodes a base64 inline map embedded in a JS file, if present.
func inlineSourceMap(jsContent string) string {
	m := reInlineMap.FindStringSubmatch(jsContent)
	if m == nil {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return ""
	}
	return string(data)
}

// looksLikeSourceMap cheaply rejects HTML/SPA-fallback bodies before we treat a
// 200 as a real map (many servers answer any path with index.html).
func looksLikeSourceMap(body string) bool {
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	return strings.Contains(head, "\"version\"") && strings.Contains(head, "\"sources\"")
}

// stripQuery drops the ?query#fragment from a URL for the .map fallback.
func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

// packageFromPath pulls the package name out of a module path that contains a
// node_modules segment. Handles scoped packages and nested node_modules.
func packageFromPath(p string) string {
	// Normalize webpack/rollup prefixes and backslashes.
	p = strings.ReplaceAll(p, "\\", "/")
	idx := strings.LastIndex(p, "node_modules/")
	if idx == -1 {
		return ""
	}
	rest := p[idx+len("node_modules/"):]
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 || parts[1] == "" {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	// Reject things that clearly aren't package roots.
	if strings.ContainsAny(parts[0], " ()[]{}<>") {
		return ""
	}
	return parts[0]
}
