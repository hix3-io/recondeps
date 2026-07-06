package main

import "time"

// Dependency is a single discovered package reference with all evidence and verdict.
type Dependency struct {
	Package     string   `json:"package"`
	Scope       string   `json:"scope,omitempty"` // @org for scoped packages
	Type        string   `json:"type"`            // scoped-private | scoped-public | company-specific | standard
	Method      string   `json:"method"`          // sourcemap | import | require | bundler-path | package-json | dynamic | obfuscated
	Sources     []string `json:"sources"`         // JS/map URLs where it was seen
	LineNumber  int      `json:"line_number,omitempty"`
	CodeExtract string   `json:"code_extract,omitempty"`
	Context     string   `json:"context,omitempty"`

	// Verdict
	Risk       string   `json:"risk"`       // low | medium | high | critical
	RiskScore  int      `json:"risk_score"` // 0-100
	Indicators []string `json:"indicators,omitempty"`

	// Resolution
	NPM    *NPMInfo    `json:"npm,omitempty"`
	Scope_ *ScopeInfo  `json:"scope_info,omitempty"`
	GitHub *GitHubInfo `json:"github,omitempty"`

	// The bottom line for supply-chain: is this a real dependency-confusion candidate?
	Exploitability string `json:"exploitability"` // confirmed-claimable | likely | unlikely | not-applicable | unknown
}

// NPMInfo captures the raw registry answer, distinguishing "absent" from "couldn't check".
type NPMInfo struct {
	Checked    bool      `json:"checked"`
	Status     int       `json:"status"` // raw HTTP status (200/404/429/0=neterr)
	Exists     bool      `json:"exists"` // only true on a definitive 200
	Public     bool      `json:"public"`
	Unknown    bool      `json:"unknown"` // true when 429/5xx/network — do NOT treat as private
	Version    string    `json:"version,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

// ScopeInfo answers the actual dependency-confusion question: is @org claimable?
type ScopeInfo struct {
	Checked     bool      `json:"checked"`
	Scope       string    `json:"scope"`
	Occupied    bool      `json:"occupied"`     // at least one public package exists under @org
	KnownPublic bool      `json:"known_public"` // @org is a well-known public org (babel, mui, ...)
	Claimable   bool      `json:"claimable"`    // best-effort: appears free to register
	Unknown     bool      `json:"unknown"`
	SamplePkg   string    `json:"sample_pkg,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

type GitHubInfo struct {
	Checked   bool      `json:"checked"`
	Exists    bool      `json:"exists"`
	Stars     int       `json:"stars"`
	Language  string    `json:"language,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// ScanResult is the full output for one target.
type ScanResult struct {
	Target        string         `json:"target"`
	StartTime     time.Time      `json:"start_time"`
	Duration      string         `json:"duration"`
	JSFiles       int            `json:"js_files_analyzed"`
	SourceMaps    int            `json:"source_maps_parsed"`
	Dependencies  []Dependency   `json:"dependencies"`
	Organizations []string       `json:"organizations"`
	Registries    []RegistryHint `json:"registries,omitempty"`
	Summary       Summary        `json:"summary"`
	Error         string         `json:"error,omitempty"`

	regSeen map[string]bool // dedup for registries (not serialized)
}

// RegistryHint is an npm-registry reference found in the target's assets. An
// internal/private registry is itself a supply-chain finding: it names where the
// private packages live and, if reachable, may be enumerable.
type RegistryHint struct {
	URL    string `json:"url"`
	Host   string `json:"host"`
	Scope  string `json:"scope,omitempty"` // @org this registry is bound to, if scoped
	Type   string `json:"type"`            // internal | artifactory | nexus | verdaccio | github-packages | azure | gitlab | public | unknown
	Source string `json:"source"`
}

type Summary struct {
	Total        int `json:"total_dependencies"`
	Private      int `json:"private_packages"`
	CompanySpec  int `json:"company_specific"`
	Confirmed    int `json:"confirmed_claimable"`
	Likely       int `json:"likely_claimable"`
	HighAndAbove int `json:"high_or_critical"`
}
