package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Resolver performs npm/scope/github lookups. It is shared across all workers so
// the cache and rate limiter are global (unlike the old per-domain scanners).
type Resolver struct {
	client  *http.Client
	enabled bool
	debug   bool
	ua      string
	ghToken string

	limiter chan struct{} // global token bucket
	stopCh  chan struct{}

	npmMu   sync.Mutex
	npm     map[string]*NPMInfo
	scopeMu sync.Mutex
	scope   map[string]*ScopeInfo
	ghMu    sync.Mutex
	gh      map[string]*GitHubInfo
}

func NewResolver(enabled bool, ratePerSec int, ghToken, ua string, debug bool) *Resolver {
	if ratePerSec < 1 {
		ratePerSec = 1
	}
	r := &Resolver{
		client:  &http.Client{Timeout: 8 * time.Second},
		enabled: enabled,
		debug:   debug,
		ua:      ua,
		ghToken: ghToken,
		limiter: make(chan struct{}, ratePerSec),
		stopCh:  make(chan struct{}),
		npm:     map[string]*NPMInfo{},
		scope:   map[string]*ScopeInfo{},
		gh:      map[string]*GitHubInfo{},
	}
	// Refill the bucket ratePerSec times per second.
	go func() {
		t := time.NewTicker(time.Second / time.Duration(ratePerSec))
		defer t.Stop()
		for {
			select {
			case <-r.stopCh:
				return
			case <-t.C:
				select {
				case r.limiter <- struct{}{}:
				default:
				}
			}
		}
	}()
	return r
}

func (r *Resolver) Close() { close(r.stopCh) }

func (r *Resolver) dbg(format string, a ...interface{}) {
	if r.debug {
		fmt.Fprintf(os.Stderr, "\033[34m[NET]\033[0m "+format+"\n", a...)
	}
}

func (r *Resolver) throttle() {
	select {
	case <-r.limiter:
	case <-time.After(10 * time.Second): // safety valve
	}
}

// CheckNPM resolves a package on the registry, distinguishing absent vs unknown.
func (r *Resolver) CheckNPM(pkg string) *NPMInfo {
	if !r.enabled {
		return &NPMInfo{Checked: false}
	}
	r.npmMu.Lock()
	if c, ok := r.npm[pkg]; ok {
		r.npmMu.Unlock()
		return c
	}
	r.npmMu.Unlock()

	info := &NPMInfo{Checked: true, CheckedAt: time.Now()}
	endpoint := "https://registry.npmjs.org/" + npmEscape(pkg)

	r.throttle()
	// Abbreviated metadata doc: ~10-100x smaller than the full doc (matters at scale)
	// and still carries dist-tags for the version.
	status, body, err := r.get(endpoint, "application/vnd.npm.install-v1+json")
	info.Status = status
	switch {
	case err != nil:
		info.Unknown = true
		info.Error = err.Error()
	case status == 200:
		info.Exists = true
		info.Public = true
		var m map[string]interface{}
		if json.Unmarshal([]byte(body), &m) == nil {
			if dt, ok := m["dist-tags"].(map[string]interface{}); ok {
				if v, ok := dt["latest"].(string); ok {
					info.Version = v
				}
			}
			if repo, ok := m["repository"].(map[string]interface{}); ok {
				if u, ok := repo["url"].(string); ok {
					info.Repository = u
				}
			}
		}
	case status == 404:
		info.Exists = false
		info.Public = false
	default:
		// 429, 5xx, anything else → we genuinely don't know. NEVER call this private.
		info.Unknown = true
		info.Error = fmt.Sprintf("http %d", status)
	}

	verdict := "unknown"
	switch {
	case info.Unknown:
		verdict = "INCONCLUSIVE"
	case info.Exists:
		verdict = "published v" + info.Version
	default:
		verdict = "NOT published"
	}
	r.dbg("npm   %-32s http=%d → %s", pkg, info.Status, verdict)

	r.npmMu.Lock()
	r.npm[pkg] = info
	r.npmMu.Unlock()
	return info
}

// CheckScope answers the real dependency-confusion question: is @org claimable?
// It searches the registry for any published package under the scope.
func (r *Resolver) CheckScope(scope string) *ScopeInfo {
	if scope == "" {
		return &ScopeInfo{Checked: false}
	}
	if !r.enabled {
		if publicScopedOrgs[strings.ToLower(scope)] {
			return &ScopeInfo{Checked: true, Scope: scope, Occupied: true, KnownPublic: true}
		}
		return &ScopeInfo{Checked: false, Scope: scope}
	}
	r.scopeMu.Lock()
	if c, ok := r.scope[scope]; ok {
		r.scopeMu.Unlock()
		return c
	}
	r.scopeMu.Unlock()

	info := &ScopeInfo{Checked: true, Scope: scope, CheckedAt: time.Now()}
	if publicScopedOrgs[strings.ToLower(scope)] {
		info.Occupied = true
		info.KnownPublic = true
		r.storeScope(scope, info)
		return info
	}

	// npm search restricted to the scope. If anything comes back under @org/,
	// the scope is owned by someone → not claimable.
	q := "https://registry.npmjs.org/-/v1/search?size=20&text=" + url.QueryEscape("scope:"+strings.TrimPrefix(scope, "@"))
	r.throttle()
	status, body, err := r.get(q, "application/json")
	if err != nil || status != 200 {
		info.Unknown = true
		r.storeScope(scope, info)
		return info
	}
	var sr struct {
		Objects []struct {
			Package struct {
				Name string `json:"name"`
			} `json:"package"`
		} `json:"objects"`
	}
	if json.Unmarshal([]byte(body), &sr) != nil {
		info.Unknown = true
		r.storeScope(scope, info)
		return info
	}
	prefix := scope + "/"
	for _, o := range sr.Objects {
		if strings.HasPrefix(o.Package.Name, prefix) {
			info.Occupied = true
			info.SamplePkg = o.Package.Name
			break
		}
	}
	info.Claimable = !info.Occupied
	if info.Occupied {
		r.dbg("scope %-32s OCCUPIED (%s) → not claimable", scope, info.SamplePkg)
	} else {
		r.dbg("scope %-32s free → APPEARS CLAIMABLE", scope)
	}
	r.storeScope(scope, info)
	return info
}

func (r *Resolver) storeScope(scope string, info *ScopeInfo) {
	r.scopeMu.Lock()
	r.scope[scope] = info
	r.scopeMu.Unlock()
}

// CheckGitHub optionally enriches with repo existence/stars (authenticated if token set).
func (r *Resolver) CheckGitHub(repoURL string) *GitHubInfo {
	if !r.enabled || repoURL == "" {
		return nil
	}
	path := repoURL
	for _, p := range []string{"git+", "https://", "http://", "ssh://", "git@"} {
		path = strings.TrimPrefix(path, p)
	}
	path = strings.TrimPrefix(path, "github.com/")
	path = strings.TrimPrefix(path, "github.com:")
	path = strings.TrimSuffix(path, ".git")
	if strings.Count(path, "/") != 1 {
		return nil
	}
	r.ghMu.Lock()
	if c, ok := r.gh[path]; ok {
		r.ghMu.Unlock()
		return c
	}
	r.ghMu.Unlock()

	info := &GitHubInfo{Checked: true, CheckedAt: time.Now()}
	r.throttle()
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+path, nil)
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", "application/vnd.github+json")
	if r.ghToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.ghToken)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := r.client.Do(req.WithContext(ctx))
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			info.Exists = true
			var m map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&m)
			if s, ok := m["stargazers_count"].(float64); ok {
				info.Stars = int(s)
			}
			if l, ok := m["language"].(string); ok {
				info.Language = l
			}
		}
	}
	r.ghMu.Lock()
	r.gh[path] = info
	r.ghMu.Unlock()
	return info
}

func (r *Resolver) get(u, accept string) (int, string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", accept)
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 32*1024)
	total := 0
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			total += n
			if total <= 8*1024*1024 {
				b.Write(buf[:n])
			}
		}
		if e != nil {
			break
		}
	}
	return resp.StatusCode, b.String(), nil
}

func npmEscape(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		// @scope/name → @scope%2Fname
		return strings.Replace(pkg, "/", "%2F", 1)
	}
	return pkg
}
