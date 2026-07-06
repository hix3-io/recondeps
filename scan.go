package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Scanner struct {
	client       *http.Client
	userAgent    string
	cookie       string
	maxBytes     int64
	maxAssets    int
	depth        int
	sameHostOnly bool
	resolver     *Resolver
	debug        bool
}

func NewScanner(res *Resolver, opts Options) *Scanner {
	return &Scanner{
		client: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: opts.Insecure},
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 8,
			},
		},
		userAgent:    opts.UserAgent,
		cookie:       opts.Cookie,
		maxBytes:     opts.MaxBytes,
		maxAssets:    opts.MaxAssets,
		depth:        opts.Depth,
		sameHostOnly: opts.SameHostOnly,
		resolver:     res,
		debug:        opts.Debug,
	}
}

func (s *Scanner) dbg(format string, a ...interface{}) {
	if s.debug {
		fmt.Fprintf(os.Stderr, "\033[35m[DEBUG]\033[0m "+format+"\n", a...)
	}
}

// Scan runs the full pipeline against a single target URL.
func (s *Scanner) Scan(target string) *ScanResult {
	start := time.Now()
	res := &ScanResult{Target: target, StartTime: start, regSeen: map[string]bool{}}

	assets, err := s.collectJS(target)
	if err != nil {
		res.Error = err.Error()
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	res.JSFiles = len(assets)
	s.dbg("%s: %d JS assets", target, len(assets))

	// Merge raw hits from all JS + their source maps, keyed by normalized package.
	merged := map[string]*Dependency{}
	addHit := func(ex extracted) {
		norm := normalizePackage(ex.pkg)
		if !looksLikePackage(norm) {
			return
		}
		d, ok := merged[norm]
		if !ok {
			d = &Dependency{
				Package: norm,
				Scope:   scopeOf(norm),
				Type:    classify(norm),
				Method:  ex.method,
			}
			merged[norm] = d
		}
		// Prefer the strongest method label (sourcemap/obfuscated over generic).
		d.Method = strongerMethod(d.Method, ex.method)
		if ex.evidence != "" && !contains(d.Sources, ex.evidence) {
			d.Sources = append(d.Sources, ex.evidence)
		}
		if d.LineNumber == 0 && ex.line > 0 {
			d.LineNumber = ex.line
			d.CodeExtract = ex.extract
			d.Context = ex.context
		}
	}

	for _, a := range assets {
		hits := extractFromJS(a.url, a.content)
		s.dbg("analyze %s → %d raw hit(s)", a.url, len(hits))
		for _, ex := range hits {
			s.dbg("   hit  %-14s %s", ex.method, ex.pkg)
			addHit(ex)
		}
		// Internal-registry hints (Artifactory/Nexus/Verdaccio/GH-Packages, @scope:registry=).
		for _, rh := range extractRegistries(a.content, a.url) {
			key := rh.Scope + "|" + rh.URL
			if !res.regSeen[key] {
				res.regSeen[key] = true
				res.Registries = append(res.Registries, rh)
				s.dbg("   registry %-10s %s (scope %s)", rh.Type, rh.URL, rh.Scope)
			}
		}

		// Source map: inline, declared-external, or fallback to <jsurl>.map.
		var mapRaw, mapSrc string
		if a.sourceMap == "data:" {
			mapRaw, mapSrc = inlineSourceMap(a.content), a.url+" (inline map)"
		} else {
			candidates := []string{}
			if a.sourceMap != "" {
				candidates = append(candidates, a.sourceMap)
			}
			candidates = append(candidates, stripQuery(a.url)+".map") // convention fallback
			for _, c := range candidates {
				body, st, err := s.fetch(c)
				if err == nil && st == 200 && looksLikeSourceMap(body) {
					mapRaw, mapSrc = body, c
					if c != a.sourceMap {
						s.dbg("   map fallback hit: %s (declared map was unusable)", c)
					}
					break
				}
			}
		}
		if mapRaw != "" {
			res.SourceMaps++
			smHits := packagesFromSourceMap(mapRaw)
			s.dbg("   sourcemap parsed → %d package(s) from sources[]", len(smHits))
			for _, ex := range smHits {
				s.dbg("   hit  %-14s %s", ex.method, ex.pkg)
				ex.evidence = mapSrc + " → " + ex.evidence
				addHit(ex)
			}
		}
	}
	s.dbg("merged → %d unique package(s), resolving…", len(merged))

	// Resolve + score.
	deps := make([]*Dependency, 0, len(merged))
	for _, d := range merged {
		deps = append(deps, d)
	}
	s.resolveAndScore(deps)

	// Filter: drop the clearly-uninteresting public scoped packages.
	final := make([]Dependency, 0, len(deps))
	orgs := map[string]bool{}
	for _, d := range deps {
		if d.Type == "scoped-public" && d.Exploitability == "not-applicable" {
			continue
		}
		final = append(final, *d)
		if d.Scope != "" {
			orgs[d.Scope] = true
		}
	}
	sort.Slice(final, func(i, j int) bool { return final[i].RiskScore > final[j].RiskScore })

	res.Dependencies = final
	for o := range orgs {
		res.Organizations = append(res.Organizations, o)
	}
	sort.Strings(res.Organizations)
	res.Summary = summarize(final)
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res
}

func (s *Scanner) resolveAndScore(deps []*Dependency) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, d := range deps {
		wg.Add(1)
		sem <- struct{}{}
		go func(d *Dependency) {
			defer wg.Done()
			defer func() { <-sem }()
			// Only spend resolution budget on plausibly-interesting packages.
			if d.Type != "scoped-public" {
				d.NPM = s.resolver.CheckNPM(d.Package)
				if d.Scope != "" {
					d.Scope_ = s.resolver.CheckScope(d.Scope)
				}
				if d.NPM != nil && d.NPM.Repository != "" {
					d.GitHub = s.resolver.CheckGitHub(d.NPM.Repository)
				}
			}
			assess(d)
			s.dbg("verdict %-32s %-20s risk=%s(%d)", d.Package, d.Exploitability, d.Risk, d.RiskScore)
		}(d)
	}
	wg.Wait()
}

func summarize(deps []Dependency) Summary {
	var sm Summary
	sm.Total = len(deps)
	for _, d := range deps {
		if d.Type == "scoped-private" {
			sm.Private++
		}
		if d.Type == "company-specific" {
			sm.CompanySpec++
		}
		if d.Exploitability == "confirmed-claimable" {
			sm.Confirmed++
		}
		if d.Exploitability == "likely" {
			sm.Likely++
		}
		if d.Risk == "high" || d.Risk == "critical" {
			sm.HighAndAbove++
		}
	}
	return sm
}

// ---------- Mass scan ----------

type MassScanner struct {
	opts     Options
	resolver *Resolver
	stats    MassStats
	outDir   string
	ndjson   *os.File
	ndMu     sync.Mutex
	seen     map[string]bool // resumed domains
}

type MassStats struct {
	Total     int64
	Processed int64
	Success   int64
	WithDeps  int64
	Claimable int64
	Start     time.Time
}

func NewMassScanner(res *Resolver, opts Options, outDir string) *MassScanner {
	return &MassScanner{opts: opts, resolver: res, outDir: outDir, seen: map[string]bool{}}
}

func (m *MassScanner) Run(domainsFile string) error {
	if err := os.MkdirAll(m.outDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(m.outDir, "high_value"), 0755); err != nil {
		return err
	}

	// Resume: load already-processed domains from existing NDJSON.
	ndPath := filepath.Join(m.outDir, "results.ndjson")
	if m.opts.Resume {
		m.loadCheckpoint(ndPath)
	}
	f, err := os.OpenFile(ndPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	m.ndjson = f
	defer f.Close()

	domains, err := readLines(domainsFile)
	if err != nil {
		return err
	}
	m.stats.Start = time.Now()
	atomic.StoreInt64(&m.stats.Total, int64(len(domains)))

	fmt.Printf("recondeps-ng v%s — mass scan\n", VERSION)
	fmt.Printf("domains=%d workers=%d resume=%v resolve=%v out=%s\n\n",
		len(domains), m.opts.Workers, m.opts.Resume, m.opts.Resolve, m.outDir)

	domainCh := make(chan string, m.opts.Workers*2)
	var wg sync.WaitGroup
	for i := 0; i < m.opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := NewScanner(m.resolver, m.opts)
			for d := range domainCh {
				m.scanOne(sc, d)
			}
		}()
	}
	stop := make(chan struct{})
	go m.progress(stop)

	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" || m.seen[d] {
			continue
		}
		domainCh <- d
	}
	close(domainCh)
	wg.Wait()
	close(stop)
	m.finalStats()
	return nil
}

func (m *MassScanner) scanOne(sc *Scanner, domain string) {
	defer atomic.AddInt64(&m.stats.Processed, 1)
	var result *ScanResult
	for _, scheme := range []string{"https://", "http://"} {
		r := sc.Scan(scheme + domain)
		if r.Error == "" && (r.JSFiles > 0 || len(r.Dependencies) > 0) {
			result = r
			break
		}
		if result == nil {
			result = r
		}
	}
	if result.Error == "" {
		atomic.AddInt64(&m.stats.Success, 1)
	}
	if len(result.Dependencies) > 0 {
		atomic.AddInt64(&m.stats.WithDeps, 1)
	}
	entry := map[string]interface{}{"domain": domain, "result": result}
	line, _ := json.Marshal(entry)
	m.ndMu.Lock()
	m.ndjson.Write(line)
	m.ndjson.Write([]byte("\n"))
	m.ndMu.Unlock()

	if result.Summary.Confirmed > 0 || result.Summary.Likely > 0 || len(result.Registries) > 0 {
		atomic.AddInt64(&m.stats.Claimable, 1)
		data, _ := json.MarshalIndent(result, "", "  ")
		os.WriteFile(filepath.Join(m.outDir, "high_value", sanitizeFilename(domain)+".json"), data, 0644)
		fmt.Printf("\033[32m🎯 %s\033[0m — %d claimable / %d likely\n", domain, result.Summary.Confirmed, result.Summary.Likely)
		for _, d := range result.Dependencies {
			if d.Exploitability == "confirmed-claimable" || d.Exploitability == "likely" {
				fmt.Printf("   └─ \033[33m%s\033[0m [%s] %s\n", d.Package, d.Exploitability, d.Risk)
			}
		}
	} else if !m.opts.Quiet && len(result.Dependencies) > 0 {
		fmt.Printf("📦 %s — %d deps\n", domain, len(result.Dependencies))
	}
}

func (m *MassScanner) loadCheckpoint(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		var e struct {
			Domain string `json:"domain"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Domain != "" {
			m.seen[e.Domain] = true
			n++
		}
	}
	fmt.Printf("resume: skipping %d already-scanned domains\n", n)
}

func (m *MassScanner) progress(stop <-chan struct{}) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			p := atomic.LoadInt64(&m.stats.Processed)
			tot := atomic.LoadInt64(&m.stats.Total)
			cl := atomic.LoadInt64(&m.stats.Claimable)
			el := time.Since(m.stats.Start).Seconds()
			rate := float64(p) / el
			fmt.Printf("\033[36m[%d/%d] %.1f%% | %.1f/s | 🎯 %d claimable\033[0m\n",
				p, tot, float64(p)/float64(max64(tot, 1))*100, rate, cl)
		}
	}
}

func (m *MassScanner) finalStats() {
	fmt.Printf("\n=== done ===\n")
	fmt.Printf("processed=%d success=%d with_deps=%d claimable_domains=%d elapsed=%s\n",
		m.stats.Processed, m.stats.Success, m.stats.WithDeps, m.stats.Claimable,
		time.Since(m.stats.Start).Round(time.Second))
	fmt.Printf("results: %s/results.ndjson | high value: %s/high_value/\n", m.outDir, m.outDir)
}

// ---------- helpers ----------

func strongerMethod(a, b string) string {
	rank := map[string]int{"import": 1, "require": 1, "package-json": 2, "bundler-path": 3, "sourcemap": 4, "obfuscated": 5}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(a, b string) string {
	if a != "" && a != "data:" {
		return a
	}
	return b
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

func sanitizeFilename(s string) string {
	repl := func(r rune) rune {
		switch r {
		case '/', '\\', ':', '?', '*', '"', '<', '>', '|', ' ':
			return '_'
		}
		return r
	}
	return strings.TrimRight(strings.Map(repl, s), "_")
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
