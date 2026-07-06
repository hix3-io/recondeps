package main

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// extracted is a raw hit before classification/resolution.
type extracted struct {
	pkg      string
	method   string
	evidence string // source path or code snippet
	line     int
	extract  string
	context  string
}

var (
	reES6 = []*regexp.Regexp{
		regexp.MustCompile(`import\s+(?:[\w*{}\s,]+\s+from\s+)?['"]([^'"]+)['"]`),
		regexp.MustCompile(`import\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`export\s+(?:\*|\{[^}]*\})\s+from\s+['"]([^'"]+)['"]`),
	}
	reRequire = []*regexp.Regexp{
		regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	}
	reBundlerPath = regexp.MustCompile(`["'\x60]([^"'\x60]*?/node_modules/(@[\w.-]+/[\w.-]+|[\w.-]+)/[^"'\x60]*)["'\x60]`)
	rePkgJSON     = []*regexp.Regexp{
		regexp.MustCompile(`"(?:dependencies|devDependencies|peerDependencies)"\s*:\s*\{([^}]*)\}`),
	}
	rePkgJSONEntry = regexp.MustCompile(`"(@?[\w.-]+(?:/[\w.-]+)?)"\s*:\s*"[^"]*"`)
	// require(atob("....")) / import(atob("....")) obfuscation.
	reObfB64 = regexp.MustCompile(`(?:require|import)\s*\(\s*atob\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]\s*\)`)
	// String.fromCharCode(...) obfuscation.
	reObfCharCode = regexp.MustCompile(`String\.fromCharCode\s*\(([\d\s,]+)\)`)
)

// extractFromJS mines a raw JS file for package references via multiple methods.
func extractFromJS(jsURL, content string) []extracted {
	lines := strings.Split(content, "\n")
	out := map[string]extracted{}
	add := func(pkg, method string, pos int) {
		pkg = strings.TrimSpace(pkg)
		if !looksLikePackage(pkg) {
			return
		}
		if _, ok := out[pkg]; ok {
			return
		}
		ln, ex, ctx := codeContext(content, pos, lines)
		out[pkg] = extracted{pkg: pkg, method: method, evidence: jsURL, line: ln, extract: ex, context: ctx}
	}

	for _, re := range reES6 {
		for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
			add(content[m[2]:m[3]], "import", m[0])
		}
	}
	for _, re := range reRequire {
		for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
			add(content[m[2]:m[3]], "require", m[0])
		}
	}
	// Bundler node_modules paths (best signal in non-source-mapped bundles).
	for _, m := range reBundlerPath.FindAllStringSubmatchIndex(content, -1) {
		full := content[m[2]:m[3]]
		if pkg := packageFromPath(full); pkg != "" {
			// locate for context
			pos := strings.Index(content, full)
			add(pkg, "bundler-path", pos)
		}
	}
	// package.json blobs embedded in JS.
	for _, re := range rePkgJSON {
		for _, block := range re.FindAllStringSubmatch(content, -1) {
			for _, e := range rePkgJSONEntry.FindAllStringSubmatch(block[1], -1) {
				add(e[1], "package-json", strings.Index(content, e[0]))
			}
		}
	}
	// Base64-obfuscated module specifiers — decoded for real this time.
	for _, m := range reObfB64.FindAllStringSubmatch(content, -1) {
		if dec, err := base64.StdEncoding.DecodeString(m[1]); err == nil {
			add(string(dec), "obfuscated", strings.Index(content, m[0]))
		}
	}
	// String.fromCharCode obfuscation.
	for _, m := range reObfCharCode.FindAllStringSubmatch(content, -1) {
		if dec := decodeCharCodes(m[1]); dec != "" {
			add(dec, "obfuscated", strings.Index(content, m[0]))
		}
	}

	var list []extracted
	for _, v := range out {
		list = append(list, v)
	}
	return list
}

func decodeCharCodes(csv string) string {
	var sb strings.Builder
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n := 0
		for _, c := range part {
			if c < '0' || c > '9' {
				return ""
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 && n < 0x110000 {
			sb.WriteRune(rune(n))
		}
	}
	return sb.String()
}

// codeContext returns (lineNumber, snippet, enclosing-context) around a byte offset.
func codeContext(content string, pos int, lines []string) (int, string, string) {
	lineNum, cur := 1, 0
	for i, l := range lines {
		cur += len(l) + 1
		if cur > pos {
			lineNum = i + 1
			break
		}
	}
	start, end := lineNum-3, lineNum+1
	if start < 0 {
		start = 0
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		prefix := "  "
		if i == lineNum-1 {
			prefix = "> "
		}
		snippet := lines[i]
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		b.WriteString(prefix)
		b.WriteString(snippet)
		b.WriteString("\n")
	}
	return lineNum, strings.TrimRight(b.String(), "\n"), detectContext(lines, lineNum-1)
}

var (
	reFunc  = regexp.MustCompile(`function\s+(\w+)`)
	reClass = regexp.MustCompile(`class\s+(\w+)`)
	reVar   = regexp.MustCompile(`(?:const|let|var)\s+(\w+)`)
)

func detectContext(lines []string, target int) string {
	for i := target; i >= 0 && i >= target-8; i-- {
		l := strings.TrimSpace(lines[i])
		if m := reFunc.FindStringSubmatch(l); m != nil {
			return "function " + m[1]
		}
		if m := reClass.FindStringSubmatch(l); m != nil {
			return "class " + m[1]
		}
		if m := reVar.FindStringSubmatch(l); m != nil {
			return "variable " + m[1]
		}
	}
	return "global"
}
