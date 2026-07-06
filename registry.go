package main

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	// @scope:registry=https://host/...   (npmrc / embedded config, unquoted)
	reScopeRegistry = regexp.MustCompile(`(@[a-z0-9][a-z0-9\-_.]*)\s*:\s*registry\s*[=:]\s*["']?(https?://[^\s"'\\]+)`)
	// "@scope:registry": "https://host/..."   (JSON-key form)
	reScopeRegistryJSON = regexp.MustCompile(`["'](@[a-z0-9][a-z0-9\-_.]*):registry["']\s*:\s*["'](https?://[^\s"'\\]+)`)
	// registry=https://host/...   or   "registry": "https://host/..."
	reGlobalRegistry = regexp.MustCompile(`(?:\bregistry\s*[=:]\s*|["']registry["']\s*:\s*)["']?(https?://[^\s"'\\]+)`)
	// any bare URL — kept only if its HOST strongly signals a registry.
	reBareURL = regexp.MustCompile(`https?://[a-z0-9.-]+(?::\d+)?[a-z0-9._/~-]*`)
)

// extractRegistries finds npm-registry references and classifies them. Public
// registries (npmjs, yarnpkg) are dropped; everything else is a real finding.
func extractRegistries(content, source string) []RegistryHint {
	out := map[string]RegistryHint{}
	// explicit=true means the URL appeared in a registry= / :registry context, so
	// weak internal hosts (.corp/.local/.internal) are trusted; bare URLs need a
	// strong host signal to avoid noise on real bundles.
	add := func(scope, raw string, explicit bool) {
		raw = strings.TrimRight(raw, "/,;)\"'")
		host := hostOf(raw)
		if host == "" {
			return
		}
		typ := classifyRegistry(host)
		if typ == "public" {
			return
		}
		if typ == "internal-weak" {
			if !explicit {
				return // too weak to trust from a bare URL
			}
			typ = "internal"
		}
		if typ == "unknown" && !explicit {
			return
		}
		key := scope + "|" + raw
		if _, ok := out[key]; ok {
			return
		}
		out[key] = RegistryHint{URL: raw, Host: host, Scope: scope, Type: typ, Source: source}
	}

	for _, m := range reScopeRegistry.FindAllStringSubmatch(content, -1) {
		add(m[1], m[2], true)
	}
	for _, m := range reScopeRegistryJSON.FindAllStringSubmatch(content, -1) {
		add(m[1], m[2], true)
	}
	for _, m := range reGlobalRegistry.FindAllStringSubmatch(content, -1) {
		add("", m[1], true)
	}
	for _, m := range reBareURL.FindAllString(content, -1) {
		add("", m, false) // strong-host only
	}

	// If a URL was captured with a scope binding, drop its scope-less duplicate.
	scopedURLs := map[string]bool{}
	for _, v := range out {
		if v.Scope != "" {
			scopedURLs[v.URL] = true
		}
	}
	var list []RegistryHint
	for _, v := range out {
		if v.Scope == "" && scopedURLs[v.URL] {
			continue
		}
		list = append(list, v)
	}
	return list
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func classifyRegistry(host string) string {
	h := strings.ToLower(host)
	switch {
	case h == "registry.npmjs.org" || h == "registry.yarnpkg.com" || strings.HasSuffix(h, ".npmjs.org"):
		return "public"
	case h == "npm.pkg.github.com":
		return "github-packages"
	case strings.HasSuffix(h, "pkgs.dev.azure.com") || strings.Contains(h, "pkgs.dev.azure.com"):
		return "azure"
	case strings.Contains(h, "artifactory") || strings.HasSuffix(h, ".jfrog.io") || strings.Contains(h, "jfrog"):
		return "artifactory"
	case strings.Contains(h, "nexus"):
		return "nexus"
	case strings.Contains(h, "verdaccio"):
		return "verdaccio"
	case strings.Contains(h, "gitlab"):
		return "gitlab"
	// Strong: host clearly serves an npm registry.
	case strings.Contains(h, "npm") || strings.Contains(h, "registry") || strings.Contains(h, "packages"):
		return "internal"
	// Weak: internal-looking hostname, trusted only in an explicit registry context.
	case strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".corp") || strings.Contains(h, ".internal.") || strings.Contains(h, ".corp."):
		return "internal-weak"
	default:
		return "unknown"
	}
}
