package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	// <script src="...">, module preloads, and generic "....js" string refs.
	reScriptSrc  = regexp.MustCompile(`(?i)<script[^>]+src\s*=\s*["']([^"']+?\.[cm]?js[^"']*)["']`)
	reModulePre  = regexp.MustCompile(`(?i)<link[^>]+href\s*=\s*["']([^"']+?\.[cm]?js[^"']*)["'][^>]*rel\s*=\s*["'](?:modulepreload|preload)["']`)
	reModulePre2 = regexp.MustCompile(`(?i)<link[^>]+rel\s*=\s*["'](?:modulepreload|preload)["'][^>]*href\s*=\s*["']([^"']+?\.[cm]?js[^"']*)["']`)
	// String literals inside JS that look like chunk file names (hashed or plain).
	reJSRef = regexp.MustCompile(`["'` + "`" + `]([a-zA-Z0-9_./~-]+?\.[cm]?js)["'` + "`" + `]`)
	// Webpack chunk map: {12:"a1b2c3",...} + ".js" — captures the whole map object to mine hashes.
	reSourceMapURL = regexp.MustCompile(`(?m)//[#@]\s*sourceMappingURL=(\S+)`)
)

// asset holds a fetched JS file and, if present, the URL of its source map.
type asset struct {
	url       string
	content   string
	sourceMap string // resolved .map URL if declared
}

// collectJS does a bounded BFS starting from the target HTML, following JS chunk
// references. Same-host only by default. Returns fetched JS assets (deduped).
func (s *Scanner) collectJS(target string) ([]asset, error) {
	base, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	html, _, err := s.fetch(target)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var frontier []string
	push := func(raw, from string) {
		abs := s.resolveURL(base, from, raw)
		if abs == "" || seen[abs] {
			return
		}
		if s.sameHostOnly && !sameHost(base, abs) {
			return
		}
		seen[abs] = true
		frontier = append(frontier, abs)
	}

	// Seed from HTML.
	for _, m := range reScriptSrc.FindAllStringSubmatch(html, -1) {
		push(m[1], target)
	}
	for _, m := range reModulePre.FindAllStringSubmatch(html, -1) {
		push(m[1], target)
	}
	for _, m := range reModulePre2.FindAllStringSubmatch(html, -1) {
		push(m[1], target)
	}

	var assets []asset
	depth := 0
	for len(frontier) > 0 && len(assets) < s.maxAssets && depth <= s.depth {
		next := frontier
		frontier = nil
		for _, jsURL := range next {
			if len(assets) >= s.maxAssets {
				break
			}
			if isNoiseAsset(jsURL) {
				continue
			}
			content, _, err := s.fetch(jsURL)
			if err != nil || content == "" {
				continue
			}
			a := asset{url: jsURL, content: content}
			// A concatenated/vendored bundle can carry several sourceMappingURL
			// comments (each bundled lib keeps its own). The map for THIS emitted
			// file is, by convention, the last one — take it, not the first.
			if all := reSourceMapURL.FindAllStringSubmatch(content, -1); all != nil {
				sm := all[len(all)-1]
				smRaw := strings.TrimSpace(sm[1])
				if !strings.HasPrefix(smRaw, "data:") {
					a.sourceMap = s.resolveURL(base, jsURL, smRaw)
					s.dbg("     map declared: %s", a.sourceMap)
				} else {
					a.sourceMap = "data:" // inline map, handled by caller
					s.dbg("     map declared: inline (data:)")
				}
			}
			assets = append(assets, a)

			// Follow nested chunk references (next depth level).
			if depth < s.depth {
				before := len(frontier)
				for _, m := range reJSRef.FindAllStringSubmatch(content, -1) {
					push(m[1], jsURL)
				}
				if n := len(frontier) - before; n > 0 {
					s.dbg("     +%d chunk ref(s) queued (depth %d)", n, depth+1)
				}
			}
		}
		depth++
	}
	return assets, nil
}

// fetch GETs a URL and returns body + status. Honors delay/UA/cookies.
func (s *Scanner) fetch(u string) (string, int, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", s.userAgent)
	if s.cookie != "" {
		req.Header.Set("Cookie", s.cookie)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.dbg("GET  err  %s (%v)", u, err)
		return "", 0, err
	}
	defer resp.Body.Close()
	// Read one extra byte to detect truncation. If the file exceeds max-bytes we
	// drop the tail — where sourceMappingURL and late deps live — so we must warn
	// loudly rather than silently miss them.
	body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxBytes+1))
	if err != nil {
		return "", resp.StatusCode, err
	}
	if int64(len(body)) > s.maxBytes {
		body = body[:s.maxBytes]
		fmt.Fprintf(os.Stderr, "\033[33m[WARN]\033[0m truncated %s at %d bytes — raise -max-bytes to capture the tail (source map / late deps)\n", u, s.maxBytes)
	}
	s.dbg("GET  %d  %s (%d bytes)", resp.StatusCode, u, len(body))
	return string(body), resp.StatusCode, nil
}

func (s *Scanner) resolveURL(base *url.URL, from, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "data:") {
		return ""
	}
	fromURL := base
	if from != "" {
		if fu, err := url.Parse(from); err == nil {
			fromURL = fu
		}
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	abs := fromURL.ResolveReference(ref)
	abs.Fragment = ""
	return abs.String()
}

func sameHost(base *url.URL, abs string) bool {
	u, err := url.Parse(abs)
	if err != nil {
		return false
	}
	return u.Hostname() == base.Hostname()
}

// isNoiseAsset skips well-known third-party JS that never carries private deps.
func isNoiseAsset(u string) bool {
	noise := []string{
		"google-analytics", "googletagmanager", "gtag/js", "gtag.js",
		"facebook.net", "connect.facebook", "hotjar", "cdn.segment",
		"jquery.min.js", "bootstrap.min.js", "recaptcha", "cloudflareinsights",
		"tarteaucitron", "cookieconsent", "polyfill.io",
	}
	lu := strings.ToLower(u)
	for _, n := range noise {
		if strings.Contains(lu, n) {
			return true
		}
	}
	return false
}
