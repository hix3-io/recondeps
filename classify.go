package main

import (
	"regexp"
	"strings"
)

var reScoped = regexp.MustCompile(`^@[a-z0-9][a-z0-9\-_.]*/[a-z0-9][a-z0-9\-_.]*$`)
var rePlain = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_.]*$`)

// nodeBuiltins are never packages of interest.
var nodeBuiltins = map[string]bool{
	"assert": true, "buffer": true, "child_process": true, "cluster": true,
	"console": true, "constants": true, "crypto": true, "dgram": true, "dns": true,
	"domain": true, "events": true, "fs": true, "http": true, "http2": true,
	"https": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true,
	"readline": true, "repl": true, "stream": true, "string_decoder": true,
	"timers": true, "tls": true, "tty": true, "url": true, "util": true, "v8": true,
	"vm": true, "worker_threads": true, "zlib": true, "async_hooks": true,
}

// publicScopedOrgs are well-known public @orgs → not dependency-confusion targets.
var publicScopedOrgs = map[string]bool{
	"@babel": true, "@rollup": true, "@vitejs": true, "@vite": true, "@parcel": true,
	"@esbuild": true, "@swc": true, "@angular": true, "@vue": true, "@remix-run": true,
	"@next": true, "@nuxt": true, "@sveltejs": true, "@gatsbyjs": true, "@astrojs": true,
	"@mui": true, "@material-ui": true, "@ant-design": true, "@chakra-ui": true,
	"@mantine": true, "@headlessui": true, "@heroicons": true, "@radix-ui": true,
	"@emotion": true, "@floating-ui": true, "@tailwindcss": true, "@fortawesome": true,
	"@popperjs": true, "@tabler": true, "@types": true, "@typescript-eslint": true,
	"@testing-library": true, "@storybook": true, "@jest": true, "@cypress": true,
	"@playwright": true, "@eslint": true, "@aws-sdk": true, "@azure": true,
	"@google-cloud": true, "@firebase": true, "@stripe": true, "@shopify": true,
	"@wordpress": true, "@supabase": true, "@reduxjs": true, "@ngrx": true,
	"@apollo": true, "@graphql-tools": true, "@prisma": true, "@trpc": true,
	"@tanstack": true, "@nestjs": true, "@fastify": true, "@sentry": true,
	"@datadog": true, "@segment": true, "@amplitude": true, "@ag-grid-community": true,
	"@ag-grid-enterprise": true, "@mapbox": true, "@googlemaps": true,
	"@monaco-editor": true, "@codemirror": true, "@vercel": true, "@netlify": true,
	"@nx": true, "@lerna": true, "@turbo": true, "@microsoft": true, "@adobe": true,
	"@salesforce": true, "@github": true, "@gitlab": true, "@atlassian": true,
	"@slack": true, "@twilio": true, "@auth0": true, "@okta": true, "@hotwired": true,
	"@formatjs": true, "@fullcalendar": true, "@splidejs": true, "@date-io": true,
	"@dnd-kit": true, "@react-aria": true, "@react-hook": true, "@react-spring": true,
	"@zag-js": true, "@internationalized": true, "@unhead": true, "@iconify": true,
	"@odoo": true, // Odoo framework packages (owl, o-spreadsheet) are documented/public
}

// company-signalling substrings in the package name → likely internal even if public.
var companySignals = []string{
	"internal", "private", "-auth", "-api", "-admin", "dashboard", "backoffice",
	"design-system", "ui-kit", "components", "core", "commons", "shared", "sdk",
	"client", "service", "gateway", "portal", "billing", "payment", "checkout",
}

// looksLikePackage rejects paths, urls, code fragments and node builtins.
func looksLikePackage(p string) bool {
	if p == "" || len(p) > 120 {
		return false
	}
	if strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "http") {
		return false
	}
	if strings.ContainsAny(p, " \t\n\r=(){}[]<>;?:!&|*+\\\"'`") {
		return false
	}
	// Strip subpath: keep @scope/name or plain top-level name.
	if strings.HasPrefix(p, "@") {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) < 2 {
			return false
		}
		cand := parts[0] + "/" + parts[1]
		return reScoped.MatchString(strings.ToLower(cand))
	}
	// Plain (non-scoped) package: take first segment, must be a real-ish name.
	seg := strings.SplitN(p, "/", 2)[0]
	if nodeBuiltins[seg] {
		return false
	}
	if len(seg) < 3 {
		return false
	}
	return rePlain.MatchString(strings.ToLower(seg))
}

// normalizePackage reduces a specifier to its installable root (@scope/name or name).
func normalizePackage(p string) string {
	if strings.HasPrefix(p, "@") {
		parts := strings.SplitN(p, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return p
	}
	return strings.SplitN(p, "/", 2)[0]
}

func scopeOf(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		if i := strings.Index(pkg, "/"); i > 0 {
			return pkg[:i]
		}
	}
	return ""
}

// classify assigns a static type before resolution. Resolution may refine it.
func classify(pkg string) string {
	scope := scopeOf(pkg)
	if scope == "" {
		return "standard"
	}
	if publicScopedOrgs[strings.ToLower(scope)] {
		return "scoped-public"
	}
	low := strings.ToLower(pkg)
	for _, sig := range companySignals {
		if strings.Contains(low, sig) {
			return "company-specific"
		}
	}
	// Unknown scoped org → treat as potentially private (highest bug-bounty value).
	return "scoped-private"
}
