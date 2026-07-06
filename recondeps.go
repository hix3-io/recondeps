package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Version de l'outil (lue depuis le fichier VERSION)
var VERSION string

func init() {
	versionBytes, err := os.ReadFile("VERSION")
	if err != nil {
		VERSION = "0.1.0" // Version par défaut si fichier absent
	} else {
		VERSION = strings.TrimSpace(string(versionBytes))
	}
}

// Structures de données (inspirées de reconjsx)
type Target struct {
	URL         string            `json:"url"`
	Domain      string            `json:"domain"`
	JSFiles     []string          `json:"js_files"`
	Dependencies []Dependency     `json:"dependencies"`
	Organizations []string        `json:"organizations"`
}

type Dependency struct {
	Package       string          `json:"package"`
	Type          string          `json:"type"` // "scoped-private", "scoped-public", "company-specific", "standard", "obfuscated", "conditional"
	Source        string          `json:"source"` // URL du fichier JS
	Method        string          `json:"method"` // "import", "require", "dynamic", "config"
	Risk          string          `json:"risk"`   // "low", "medium", "high", "critical"
	RiskScore     int             `json:"risk_score"` // 0-100 score for sorting
	CodeExtract   string          `json:"code_extract"` // Extrait de code autour de la dépendance
	LineNumber    int             `json:"line_number"`  // Numéro de ligne dans le fichier
	Context       string          `json:"context"`      // Contexte additionnel (fonction, classe, etc.)
	NPMInfo       *NPMPackageInfo `json:"npm_info,omitempty"` // NPM registry information
	GitHubInfo    *GitHubRepoInfo `json:"github_info,omitempty"` // GitHub repository information
	Indicators    []string        `json:"indicators,omitempty"` // Risk indicators found
}

type ScanResult struct {
	Summary struct {
		Target              string        `json:"target"`
		JSFilesAnalyzed     int           `json:"js_files_analyzed"`
		DependenciesFound   int           `json:"dependencies_found"`
		PrivatePackages     int           `json:"private_packages"`
		ScopedPackages      int           `json:"scoped_packages"` // Keep for backward compatibility
		OrganizationsFound  int           `json:"organizations_found"`
		ObfuscatedDeps      int           `json:"obfuscated_dependencies"`
		ScanDuration        time.Duration `json:"scan_duration"`
		StartTime           time.Time     `json:"start_time"`
	} `json:"summary"`
	Targets      []Target     `json:"targets"`
	Dependencies []Dependency `json:"dependencies"`
	Organizations []string    `json:"organizations"`
	Risks        []RiskItem   `json:"risks"`
}

type RiskItem struct {
	Level       string   `json:"level"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Packages    []string `json:"packages"`
}

type Scanner struct {
	client        *http.Client
	visited       map[string]bool
	mutex         sync.RWMutex
	userAgent     string
	debug         bool
	dependencies  map[string]*Dependency
	organizations map[string]bool
	maxDepth      int
	delay         time.Duration
	npmCache      map[string]*NPMPackageInfo  // Cache for npm lookups
	npmMutex      sync.RWMutex                // Mutex for npm cache
	githubCache   map[string]*GitHubRepoInfo  // Cache for GitHub lookups
	githubMutex   sync.RWMutex                // Mutex for GitHub cache
}

// NPM package information from registry
type NPMPackageInfo struct {
	Exists       bool      `json:"exists"`
	Public       bool      `json:"public"`
	Version      string    `json:"version"`
	Description  string    `json:"description"`
	Author       string    `json:"author"`
	Downloads    int       `json:"downloads"`
	LastModified time.Time `json:"last_modified"`
	Repository   string    `json:"repository"`
	CheckedAt    time.Time `json:"checked_at"`
}

// GitHub repository information
type GitHubRepoInfo struct {
	Exists      bool      `json:"exists"`
	Public      bool      `json:"public"`
	Stars       int       `json:"stars"`
	Forks       int       `json:"forks"`
	LastUpdated time.Time `json:"last_updated"`
	Language    string    `json:"language"`
	Topics      []string  `json:"topics"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Mass scanning structures
type MassScanner struct {
	scanner       *Scanner
	workers       int
	timeout       time.Duration
	httpsFirst    bool
	onlyScoped    bool
	outputDir     string
	quiet         bool
	stats         *ScanStats
}

type ScanStats struct {
	TotalDomains    int64
	ProcessedDomains int64
	SuccessfulScans int64
	WithJS          int64
	WithScoped      int64
	StartTime       time.Time
	mutex           sync.RWMutex
}

type DomainResult struct {
	Domain    string      `json:"domain"`
	Success   bool        `json:"success"`
	Result    *ScanResult `json:"result,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// Patterns de reconnaissance (inspirés de reconjsx mais pour les dépendances)
var (
	// Patterns pour extraire les imports ES6
	es6ImportPatterns = []*regexp.Regexp{
		regexp.MustCompile(`import\s+(?:\{[^}]*\}|\*\s+as\s+\w+|\w+)\s+from\s+['"]([^'"]+)['"]`),
		regexp.MustCompile(`import\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`from\s+['"]([^'"]+)['"]`), // Patterns plus larges pour bundles
	}

	// Patterns pour extraire les require CommonJS
	requirePatterns = []*regexp.Regexp{
		regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`const\s+\w+\s*=\s*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`var\s+\w+\s*=\s*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`let\s+\w+\s*=\s*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	}

	// Patterns pour webpack/bundler (crucial pour apps modernes)
	bundlerPatterns = []*regexp.Regexp{
		regexp.MustCompile(`__webpack_require__\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`webpackJsonp.*?['"]([^'"]+)['"]`),
		regexp.MustCompile(`__WEBPACK_IMPORTED_MODULE_\d+__([^'"]+)`),
		regexp.MustCompile(`"([^"]*\/node_modules\/([^"\/]+)\/[^"]*)"`), // node_modules dans bundles
	}

	// Patterns pour package.json expose dans le web (très utile!)
	packageJsonPatterns = []*regexp.Regexp{
		regexp.MustCompile(`"dependencies"\s*:\s*\{[^}]*"([^"]+)"\s*:`), 
		regexp.MustCompile(`"devDependencies"\s*:\s*\{[^}]*"([^"]+)"\s*:`),
		regexp.MustCompile(`"peerDependencies"\s*:\s*\{[^}]*"([^"]+)"\s*:`),
		regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`), // Nom du package lui-même
	}

	// Patterns pour détecter l'obfuscation (inspiré de reconjsx secrets)
	obfuscatedPatterns = []*regexp.Regexp{
		regexp.MustCompile(`atob\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]\s*\)`),
		regexp.MustCompile(`Buffer\.from\s*\(\s*['"]([A-Za-z0-9+/=]+)['"]\s*,\s*['"]base64['"]\s*\)`),
	}

	// Patterns pour les imports conditionnels
	conditionalPatterns = []*regexp.Regexp{
		regexp.MustCompile(`try\s*\{[^}]*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
		regexp.MustCompile(`if\s*\([^)]*\)\s*\{[^}]*require\s*\(\s*['"]([^'"]+)['"]\s*\)`),
	}

	// Patterns pour les configurations (webpack, etc.)
	configPatterns = []*regexp.Regexp{
		regexp.MustCompile(`externals\s*:\s*\{[^}]*['"]([^'"]+)['"]\s*:`),
		regexp.MustCompile(`alias\s*:\s*\{[^}]*['"]([^'"]+)['"]\s*:`),
	}

	// Pattern pour identifier les packages scoped
	scopedPackagePattern = regexp.MustCompile(`^@[a-zA-Z0-9\-_]+\/[a-zA-Z0-9\-_.]+`)
)

func NewScanner(debug bool, maxDepth int, delay time.Duration) *Scanner {
	return &Scanner{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConns:        100,  // Limit idle connections
				MaxIdleConnsPerHost: 10,   // Limit per host
				DisableKeepAlives:   false, // Reuse connections but limit them
			},
		},
		visited:       make(map[string]bool),
		userAgent:     "ReconDeps/1.0.0 (Dependency Reconnaissance Tool)",
		debug:         debug,
		dependencies:  make(map[string]*Dependency),
		organizations: make(map[string]bool),
		maxDepth:      maxDepth,
		delay:         delay,
		npmCache:      make(map[string]*NPMPackageInfo),
		githubCache:   make(map[string]*GitHubRepoInfo),
	}
}

func (s *Scanner) log(message string, level string) {
	colors := map[string]string{
		"info":    "\033[36m[*]\033[0m",
		"success": "\033[32m[+]\033[0m",
		"warning": "\033[33m[!]\033[0m",
		"error":   "\033[31m[-]\033[0m",
		"debug":   "\033[35m[DEBUG]\033[0m",
	}
	
	// Only show debug and info messages in debug mode
	if (level == "debug" || level == "info") && !s.debug {
		return
	}
	
	fmt.Printf("%s %s\n", colors[level], message)
}

// Découverte des fichiers JS (inspirée de reconjsx)
func (s *Scanner) discoverJSFiles(targetURL string) ([]string, error) {
	s.log(fmt.Sprintf("Discovering JavaScript files from: %s", targetURL), "info")
	
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	
	var jsFiles []string
	
	// Récupérer la page principale
	resp, err := s.client.Get(targetURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	content := string(body)
	
	// Patterns pour extraire les références de fichiers JS (comme reconjsx)
	jsPatterns := []*regexp.Regexp{
		regexp.MustCompile(`<script[^>]*src\s*=\s*['"]([^'"]*\.js[^'"]*?)['"]`),
		regexp.MustCompile(`<script[^>]*src\s*=\s*['"]([^'"]*)['"]\s*type\s*=\s*['"]module['"]`),
		regexp.MustCompile(`import\s*\(\s*['"]([^'"]*\.js[^'"]*?)['"]`),
		regexp.MustCompile(`['"]([^'"]*\.js)['"]`),
	}
	
	for _, pattern := range jsPatterns {
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				jsURL := match[1]
				
				// Convertir en URL absolue
				if strings.HasPrefix(jsURL, "//") {
					jsURL = parsedURL.Scheme + ":" + jsURL
				} else if strings.HasPrefix(jsURL, "/") {
					jsURL = fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, jsURL)
				} else if !strings.HasPrefix(jsURL, "http") {
					jsURL = fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, "/"+jsURL)
				}
				
				// Filtrer les fichiers JS valides
				if s.isValidJSFile(jsURL) {
					jsFiles = append(jsFiles, jsURL)
				}
			}
		}
	}
	
	// Dédupliquer
	jsFiles = s.deduplicateSlice(jsFiles)
	
	s.log(fmt.Sprintf("Found %d JavaScript files", len(jsFiles)), "info")
	return jsFiles, nil
}

func (s *Scanner) isValidJSFile(jsURL string) bool {
	// Filtrer les fichiers non-JS ou CDN communs (comme reconjsx filtre les domaines)
	excludePatterns := []string{
		"google-analytics",
		"googletagmanager",
		"facebook.net",
		"analytics.js",
		"gtag.js",
		"jquery.min.js",
		"bootstrap.min.js",
	}
	
	for _, pattern := range excludePatterns {
		if strings.Contains(jsURL, pattern) {
			return false
		}
	}
	
	return strings.Contains(jsURL, ".js")
}

// Extrait le contexte de code autour d'une dépendance (crucial pour bug bounty)
func (s *Scanner) extractCodeContext(content string, matchPos int, lines []string) (int, string, string) {
	// Trouver la ligne où se trouve la correspondance
	lineNum := 1
	currentPos := 0
	
	for i, line := range lines {
		currentPos += len(line) + 1 // +1 pour le \n
		if currentPos > matchPos {
			lineNum = i + 1
			break
		}
	}
	
	// Extraire 2 lignes avant et après pour le contexte
	contextLines := 2
	startLine := lineNum - contextLines - 1
	endLine := lineNum + contextLines - 1
	
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	
	// Construire l'extrait de code
	var extractLines []string
	for i := startLine; i <= endLine; i++ {
		prefix := "  "
		if i == lineNum-1 { // Ligne courante
			prefix = "► "
		}
		extractLines = append(extractLines, fmt.Sprintf("%s%d: %s", prefix, i+1, lines[i]))
	}
	extract := strings.Join(extractLines, "\n")
	
	// Détecter le contexte (fonction, classe, objet)
	context := s.detectContext(lines, lineNum-1)
	
	return lineNum, extract, context
}

// Détecte le contexte JavaScript (fonction, classe, objet) autour de la ligne
func (s *Scanner) detectContext(lines []string, targetLine int) string {
	// Rechercher en remontant pour trouver le contexte
	for i := targetLine; i >= 0 && i >= targetLine-10; i-- {
		line := strings.TrimSpace(lines[i])
		
		// Détecter les patterns de fonction
		if strings.Contains(line, "function ") {
			funcName := s.extractFunctionName(line)
			if funcName != "" {
				return fmt.Sprintf("function %s", funcName)
			}
		}
		
		// Détecter les classes
		if strings.Contains(line, "class ") {
			className := s.extractClassName(line)
			if className != "" {
				return fmt.Sprintf("class %s", className)
			}
		}
		
		// Détecter les méthodes
		if regexp.MustCompile(`\w+\s*\(`).MatchString(line) && strings.Contains(line, ":") {
			methodName := s.extractMethodName(line)
			if methodName != "" {
				return fmt.Sprintf("method %s", methodName)
			}
		}
		
		// Détecter les objets/configurations
		if strings.Contains(line, "const ") || strings.Contains(line, "let ") || strings.Contains(line, "var ") {
			varName := s.extractVariableName(line)
			if varName != "" {
				return fmt.Sprintf("variable %s", varName)
			}
		}
	}
	
	return "global scope"
}

func (s *Scanner) extractFunctionName(line string) string {
	re := regexp.MustCompile(`function\s+(\w+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (s *Scanner) extractClassName(line string) string {
	re := regexp.MustCompile(`class\s+(\w+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (s *Scanner) extractMethodName(line string) string {
	re := regexp.MustCompile(`(\w+)\s*\(`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (s *Scanner) extractVariableName(line string) string {
	re := regexp.MustCompile(`(?:const|let|var)\s+(\w+)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// Analyse des dépendances depuis le contenu JS (coeur de l'outil)
func (s *Scanner) analyzeDependencies(jsURL string, content string) []Dependency {
	var deps []Dependency
	lines := strings.Split(content, "\n")
	
	// Extraire les imports ES6 avec contexte
	for _, pattern := range es6ImportPatterns {
		matches := pattern.FindAllStringSubmatchIndex(content, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				pkg := content[match[2]:match[3]]
				
				if s.isValidPackage(pkg) {
					lineNum, extract, context := s.extractCodeContext(content, match[0], lines)
					dep := s.createEnhancedDependency(pkg, jsURL, "import", lineNum, extract, context)
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("ES6 import: %s at line %d", pkg, lineNum), "debug")
					}
				}
			}
		}
	}
	
	// Extraire les require CommonJS avec contexte
	for _, pattern := range requirePatterns {
		matches := pattern.FindAllStringSubmatchIndex(content, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				pkg := content[match[2]:match[3]]
				
				if s.isValidPackage(pkg) {
					lineNum, extract, context := s.extractCodeContext(content, match[0], lines)
					dep := s.createEnhancedDependency(pkg, jsURL, "require", lineNum, extract, context)
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("CommonJS require: %s at line %d", pkg, lineNum), "debug")
					}
				}
			}
		}
	}
	
	// Analyser les patterns webpack/bundler (crucial pour apps modernes)
	for _, pattern := range bundlerPatterns {
		matches := pattern.FindAllStringSubmatchIndex(content, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				pkg := content[match[2]:match[3]]
				
				// Nettoyer les paths de node_modules
				if strings.Contains(pkg, "node_modules") {
					parts := strings.Split(pkg, "/")
					for i, part := range parts {
						if part == "node_modules" && i+1 < len(parts) {
							pkg = parts[i+1]
							// Si c'est un package scoped, inclure aussi le nom
							if strings.HasPrefix(pkg, "@") && i+2 < len(parts) {
								pkg = pkg + "/" + parts[i+2]
							}
							break
						}
					}
				}
				
				if s.isValidPackage(pkg) {
					lineNum, extract, context := s.extractCodeContext(content, match[0], lines)
					dep := s.createEnhancedDependency(pkg, jsURL, "bundler", lineNum, extract, context)
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("Bundler dependency: %s at line %d", pkg, lineNum), "debug")
					}
				}
			}
		}
	}

	// Analyser les package.json exposés (très précieux pour bug bounty!)
	for _, pattern := range packageJsonPatterns {
		matches := pattern.FindAllStringSubmatchIndex(content, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				pkg := content[match[2]:match[3]]
				
				if s.isValidPackage(pkg) {
					lineNum, extract, context := s.extractCodeContext(content, match[0], lines)
					dep := s.createEnhancedDependency(pkg, jsURL, "package.json", lineNum, extract, context)
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("Package.json dependency: %s at line %d", pkg, lineNum), "debug")
					}
				}
			}
		}
	}

	// Détecter l'obfuscation (comme reconjsx détecte les secrets)
	for _, pattern := range obfuscatedPatterns {
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				decoded := s.decodeBase64(match[1])
				if decoded != "" && s.isValidPackage(decoded) {
					lineNum, extract, context := s.extractCodeContext(content, strings.Index(content, match[0]), lines)
					
					dep := s.createEnhancedDependency(decoded, jsURL, "obfuscated", lineNum, extract, context)
					dep.Type = "obfuscated" // Override type
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("Obfuscated dependency: %s at line %d", decoded, lineNum), "debug")
					}
				}
			}
		}
	}
	
	// Détecter les imports conditionnels
	for _, pattern := range conditionalPatterns {
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				pkg := match[1]
				if s.isValidPackage(pkg) {
					dep := s.createEnhancedDependency(pkg, jsURL, "conditional", 0, "", "")
					deps = append(deps, dep)
					
					if s.debug {
						s.log(fmt.Sprintf("Conditional dependency: %s", pkg), "debug")
					}
				}
			}
		}
	}
	
	return deps
}

func (s *Scanner) isValidPackage(pkg string) bool {
	// Exclure les chemins relatifs
	if strings.HasPrefix(pkg, "./") || strings.HasPrefix(pkg, "../") {
		return false
	}
	
	// Exclure les URLs complètes
	if strings.HasPrefix(pkg, "http") {
		return false
	}
	
	// NOUVEAU: Filtrer les détections webpack bundler non pertinentes
	if s.isWebpackNoise(pkg) {
		return false
	}
	
	// Focus sur les packages scoped pour supply chain analysis
	return scopedPackagePattern.MatchString(pkg) || (!strings.Contains(pkg, "/") && len(pkg) > 2)
}

// Filtrer le bruit webpack qui pollue les résultats
func (s *Scanner) isWebpackNoise(pkg string) bool {
	// 1. Modules Node.js standards (ne sont PAS privés)
	nodeStandardModules := []string{
		"worker_threads", "child_process", "cluster", "crypto", "dns", "events", 
		"fs", "http", "https", "net", "os", "path", "querystring", "readline",
		"stream", "string_decoder", "timers", "tls", "url", "util", "v8", "vm",
		"zlib", "assert", "buffer", "constants", "domain", "punycode", "tty",
		"dgram", "repl", "process", "module", "console",
	}
	
	for _, nodeModule := range nodeStandardModules {
		if pkg == nodeModule {
			return true // Filtrer les modules Node.js standards
		}
	}
	
	// 2. Patterns Bower/Component obsolètes (format: name~package@version)
	if strings.Contains(pkg, "~") && strings.Contains(pkg, "@") {
		// Ex: component~indexof@0.0.3, abpetkov~transitionize@0.0.3
		return true
	}
	
	// 3. Fragments de code JavaScript et variables
	codeFragmentPatterns := []string{
		" + ", " - ", " * ", " / ", " % ", // Opérateurs mathématiques
		" prev ", " next ", " curr ", " temp ", // Variables communes
		"\\u", "\\x", "\\n", "\\r", "\\t", // Échappements Unicode/hexa
		"alpha", "beta", "gamma", "delta", // Variables grecques
		".EN.", ".FR.", ".DE.", ".ASC", ".DESC", // Constantes d'énumération
		"null", "undefined", "true", "false", // Littéraux JavaScript
		" + prev + ", " + next + ", // Concaténations courantes
		"use strict", // Directive JavaScript
		"use server", // React Server Component directive
		"use client", // React Client Component directive
		".pnpm", // Package manager reference
		"+", // Concaténation de string dans le code
		"constructor.name", // Propriété JavaScript
	}
	
	for _, pattern := range codeFragmentPatterns {
		if strings.Contains(pkg, pattern) {
			return true
		}
	}
	
	// 4. Titres HTML et métadonnées (pas des packages)
	htmlMetadataPatterns := []string{
		"Page Not Found", "Not Found", "404", "Error",
		"Accueil", "Home", "Index", "Welcome",
		"Login", "Sign", "Auth", "Connect",
		"Contact", "About", "Help", "Support",
		"Solution", "Service", "Product", "Company",
	}
	
	for _, pattern := range htmlMetadataPatterns {
		if strings.Contains(pkg, pattern) {
			return true
		}
	}
	
	// 5. Patterns webpack bundler non pertinents
	webpackNoisePatterns := []string{
		"__webpack_require__",
		"_WEBPACK_IMPORTED_MODULE_",
		"react__WEBPACK_IMPORTED_MODULE_",
		"_input_", "_footer_", "_sidebar_", "_navigation_",
		"_themeContainer_", "_reactIcon_", "_reactButton_", 
		"_selectAutocomplete_", "_listparam_", "_overflowTip_",
		"_mui_material_", "_mui_styles_",
		"react_dom_", "react__", 
		".createElement(", ".Component{", ".Fragment,",
		"productionPrefix:", "generateClassName:",
		".render(", ".mount(", ".createRef(",
	}
	
	for _, pattern := range webpackNoisePatterns {
		if strings.Contains(pkg, pattern) {
			return true
		}
	}
	
	// 6. Filtrer les extraits de code webpack trop longs (pas des noms de packages)
	if len(pkg) > 100 {
		return true
	}
	
	// 7. Filtrer les IDs webpack courts non significatifs (comme "3c35", "e4da")
	if len(pkg) <= 4 && !strings.Contains(pkg, "/") && !strings.Contains(pkg, "@") {
		// Sauf s'il s'agit de packages connus courts
		knownShortPackages := []string{"vue", "react", "lodash", "axios", "uuid", "cors", "path", "util", "fs", "os"}
		for _, known := range knownShortPackages {
			if pkg == known {
				return false
			}
		}
		return true
	}
	
	// 8. Filtrer les noms génériques trop courts ou non significatifs
	genericPatterns := []string{
		"pcexp", "data-report-event", // Noms trop génériques
	}
	
	for _, pattern := range genericPatterns {
		if pkg == pattern || strings.Contains(pkg, pattern) {
			return true
		}
	}
	
	// 9. Filtrer les patterns de variables JavaScript
	if strings.Contains(pkg, "=") || strings.Contains(pkg, "{") || 
	   strings.Contains(pkg, "}") || strings.Contains(pkg, "(") ||
	   strings.Contains(pkg, ")") || strings.Contains(pkg, ";") ||
	   strings.Contains(pkg, "?") || strings.Contains(pkg, ":") {
		return true
	}
	
	// 10. Filtrer les noms trop courts avec caractères spéciaux
	if len(pkg) <= 6 && (strings.Contains(pkg, "+") || strings.Contains(pkg, "-") || 
	   strings.Contains(pkg, "*") || strings.Contains(pkg, "/") ||
	   strings.Contains(pkg, " ")) {
		return true
	}
	
	return false
}

func (s *Scanner) classifyPackage(pkg string) string {
	if scopedPackagePattern.MatchString(pkg) {
		classification := s.classifyScopedPackage(pkg)
		return classification
	}
	return "standard"
}

func (s *Scanner) classifyScopedPackage(pkg string) string {
	// Check if it's a known generic public package (not interesting for bug bounty)
	if s.isGenericPublicPackage(pkg) {
		return "scoped-public"
	}
	
	// Check if it's a company-specific package that might be public but still interesting
	if s.isCompanySpecificPackage(pkg) {
		return "company-specific"
	}
	
	// Everything else is considered potentially private (highest value)
	return "scoped-private"
}

// Known public scoped packages (common ones that are not interesting for bug bounty)
var publicScopedPackages = map[string]bool{
	// Build tools & bundlers
	"@babel":                true,
	"@webpack":             true,
	"@rollup":              true,
	"@vite":                true,
	"@parcel":              true,
	"@esbuild":             true,
	
	// Frameworks & Libraries
	"@angular":              true,
	"@vue":                  true,
	"@react":                true,
	"@next":                true,
	"@nuxt":                true,
	"@svelte":              true,
	"@remix-run":           true,
	"@gatsbyjs":            true,
	
	// UI Libraries
	"@mui":                 true,
	"@material-ui":         true,
	"@ant-design":          true,
	"@chakra-ui":           true,
	"@mantine":             true,
	"@headlessui":          true,
	"@heroicons":           true,
	"@radix-ui":            true,
	"@emotion":             true,
	"@styled-components":   true,
	"@tailwindcss":         true,
	"@fortawesome":         true,
	"@popperjs":            true,
	
	// Testing & Dev Tools
	"@types":                true,
	"@typescript-eslint":    true,
	"@testing-library":      true,
	"@storybook":           true,
	"@jest":                true,
	"@cypress":             true,
	"@playwright":          true,
	"@eslint":              true,
	"@prettier":            true,
	"@commitlint":          true,
	"@semantic-release":    true,
	"@changesets":          true,
	"@rushstack":           true,
	
	// Cloud & Services
	"@aws-sdk":             true,
	"@azure":               true,
	"@google-cloud":        true,
	"@firebase":            true,
	"@stripe":              true,
	"@shopify":             true,
	"@wordpress":           true,
	"@supabase":            true,
	"@planetscale":         true,
	
	// State Management & Data
	"@reduxjs":             true,
	"@ngrx":                true,
	"@apollo":              true,
	"@graphql-tools":       true,
	"@prisma":              true,
	"@trpc":                true,
	"@tanstack":            true,
	
	// Backend & API
	"@nestjs":              true,
	"@fastify":             true,
	"@hapi":                true,
	"@koa":                 true,
	"@express":             true,
	
	// Monitoring & Analytics
	"@sentry":              true,
	"@datadog":             true,
	"@newrelic":            true,
	"@segment":             true,
	"@amplitude":           true,
	"@mixpanel":            true,
	"@hotjar":              true,
	"@bugsnag":             true,
	
	// Common Utilities & Grids
	"@ag-grid-community":   true,
	"@ag-grid-enterprise":  true,
	"@mapbox":              true,
	"@googlemaps":          true,
	"@monaco-editor":       true,
	"@codemirror":          true,
	
	// Node.js & Runtime
	"@node":                true,
	"@nodejs":              true,
	"@vercel":              true,
	"@netlify":             true,
	
	// Package Managers & Tools
	"@npm":                 true,
	"@pnpm":                true,
	"@yarn":                true,
	"@lerna":               true,
	"@nx":                  true,
	"@turbo":               true,
	
	// Known Public Companies/Orgs (but need verification)
	"@microsoft":           true,
	"@adobe":               true,
	"@salesforce":          true,
	"@github":              true,
	"@gitlab":              true,
	"@atlassian":           true,
	"@slack":               true,
	"@discord":             true,
	"@twilio":              true,
	"@auth0":               true,
	"@okta":                true,
}

// Known company-specific packages that are public on npm but still interesting for bug bounty
var companySpecificPackages = map[string]bool{
	// Odoo official packages (few packages that are actually on npm)
	"@odoo": true,
}

// Known Odoo standard modules (not interesting for bug bounty - part of standard Odoo architecture)
var odooStandardModules = map[string]bool{
	// Core Odoo modules
	"@mail": true, "@web": true, "@website": true, "@sale": true, 
	"@html_editor": true, "@web_editor": true, "@portal": true, 
	"@payment": true, "@delivery": true, "@sign": true, 
	"@knowledge": true, "@planning": true, "@loyalty": true, 
	"@appointment": true, "@purchase": true, "@inventory": true, 
	"@accounting": true, "@hr": true, "@crm": true, "@project": true, 
	"@manufacturing": true, "@quality": true, "@maintenance": true,
	"@point_of_sale": true, "@pos": true, "@stock": true, "@mrp": true, 
	"@timesheet": true, "@calendar": true, "@contacts": true, "@board": true, 
	"@mass_mailing": true, "@survey": true, "@fleet": true, "@lunch": true,
	"@marketing": true, "@event": true, "@helpdesk": true, "@documents": true, 
	"@website_sale": true, "@l10n": true, "@base": true, "@addons": true, 
	"@enterprise": true, "@studio": true, "@voip": true,
	
	// Additional modules found in scans
	"@im_livechat": true, "@bus": true, "@social_push_notifications": true,
	"@hr_contract_salary": true, "@industry_fsm": true, "@google_recaptcha": true,
	"@openfonts": true,
	
	// Website/web modules
	"@web_tour": true, "@web_unsplash": true, "@website_slides": true,
	"@portal_rating": true, "@website_blog": true,
	
	// Additional website modules (common Odoo website addons)
	"@website_forum": true, "@website_helpdesk": true, "@website_event": true,
	"@website_crm": true, "@website_hr_recruitment": true, "@website_mass_mailing": true,
	"@website_rating": true, "@website_payment": true, "@website_delivery": true,
	"@website_loyalty": true, "@website_appointment": true, "@website_knowledge": true,
}

// Known official public packages for specific companies (not interesting as they're documented)
var knownOfficialPackages = map[string]bool{
	"@odoo/owl":              true, // Odoo Web Library (official framework)
	"@odoo/o-spreadsheet":    true, // Official spreadsheet component
	"@odoo/eslint-plugin-owl": true, // Official ESLint plugin
}

func (s *Scanner) isGenericPublicPackage(pkg string) bool {
	if !strings.HasPrefix(pkg, "@") {
		return false
	}
	
	parts := strings.Split(pkg, "/")
	if len(parts) < 2 {
		return false
	}
	
	org := parts[0] // @org
	pkgName := parts[1]
	
	// CRITICAL: Handle subpath imports FIRST
	// For @babel/runtime/helpers/typeof, @mui/material/styles, etc.
	if len(parts) > 2 {
		// This is a subpath import - check if the org is public
		if publicScopedPackages[org] {
			s.log(fmt.Sprintf("DEBUG: Subpath %s filtered out - org %s is public", pkg, org), "debug")
			return true
		}
		
		// Also check the base package
		basePackage := org + "/" + pkgName
		if knownOfficialPackages[basePackage] {
			s.log(fmt.Sprintf("DEBUG: Subpath %s filtered out - base package %s is known public", pkg, basePackage), "debug")
			return true
		}
	}
	
	// Check if it's in our known generic public packages list
	if publicScopedPackages[org] {
		return true
	}
	
	// Check if it's a known official package (documented, not interesting for bug bounty)
	if knownOfficialPackages[pkg] {
		return true
	}
	
	// Check if it's a standard Odoo module (part of normal Odoo architecture)
	if odooStandardModules[org] {
		return true
	}
	
	// Patterns indicating generic public development tools (not company-specific)
	publicToolPatterns := []string{
		"eslint-config", "prettier-config", "babel-preset", "webpack-",
		"rollup-", "vite-", "jest-", "types", "testing-",
		"cli", "config", "utils", "helpers", "common", "shared",
		"core", "base", "runtime", "polyfill", "shim", "plugin",
		"loader", "transformer", "parser", "compiler",
	}
	
	for _, pattern := range publicToolPatterns {
		if strings.Contains(pkgName, pattern) {
			return true
		}
	}
	
	// Add specific known generic public packages
	knownGenericPackages := map[string]bool{
		"@date-io/dayjs": true,
		"@glidejs/glide": true,
		"@fullcalendar/common": true,
		"@hotwired/stimulus": true,
		"@splidejs/splide": true,
		"@stardazed/zlib": true,
		"@symfony/stimulus-bundle": true,
	}
	
	if knownGenericPackages[pkg] {
		return true
	}
	
	// Check for known generic public organizations
	publicOrgPatterns := []string{
		"formatjs", "tannin", "symfony", "stimulus-components", "hotwired",
	}
	
	for _, pattern := range publicOrgPatterns {
		if strings.Contains(org, pattern) {
			return true
		}
	}
	
	// If we reach here, it's likely a company-specific package
	// which is exactly what we want for bug bounty
	return false
}

func (s *Scanner) isCompanySpecificPackage(pkg string) bool {
	if !strings.HasPrefix(pkg, "@") {
		return false
	}
	
	parts := strings.Split(pkg, "/")
	if len(parts) < 2 {
		return false
	}
	
	org := parts[0] // @org
	pkgName := parts[1]
	
	// Skip Odoo standard modules (they're handled as generic public)
	if odooStandardModules[org] {
		return false
	}
	
	// Check if it's in known company-specific organizations (excluding Odoo standard modules)
	if companySpecificPackages[org] {
		return true
	}
	
	// Patterns indicating company-specific packages that might be public on npm
	// but are still interesting for bug bounty (excluding standard Odoo modules)
	companySpecificPatterns := []string{
		// Generic company patterns (could be internal)
		"api", "auth", "admin", "dashboard", "internal", "private",
		"business", "corporate", "company", "organization",
		"app", "service", "microservice", "backend", "frontend",
		"design-system", "ui-kit", "components", "library",
		"security", "monitoring", "analytics", "reporting",
	}
	
	// Check if this looks like a company-specific package
	for _, pattern := range companySpecificPatterns {
		if strings.Contains(strings.ToLower(pkgName), pattern) {
			return true
		}
	}
	
	// Check if organization name suggests company-specific packages (excluding Odoo standard modules)
	companyOrgPatterns := []string{
		"enterprise", "business", "corp", "internal", "private", 
		"company", "org", "team",
	}
	
	for _, pattern := range companyOrgPatterns {
		if strings.Contains(strings.ToLower(org), pattern) {
			return true
		}
	}
	
	return false
}

func (s *Scanner) filterInterestingPackages(deps []Dependency) []Dependency {
	var filtered []Dependency
	
	for _, dep := range deps {
		// Only keep packages that are interesting for bug bounty
		// Filter out generic public packages (scoped-public with low risk)
		if dep.Type == "scoped-public" && dep.Risk == "low" {
			continue // Skip generic public packages
		}
		
		// Keep all other packages:
		// - scoped-private (high risk)
		// - company-specific (medium risk) 
		// - obfuscated (high risk)
		// - conditional (medium risk)
		// - standard packages (might be interesting)
		filtered = append(filtered, dep)
	}
	
	return filtered
}

func (s *Scanner) assessRisk(pkg string, method string, npmInfo *NPMPackageInfo, indicators []string) (string, int) {
	// Enhanced risk scoring system (0-100)
	score := 0
	
	// Factor 1: Detection method (0-30 points)
	switch method {
	case "obfuscated":
		score += 30
		indicators = append(indicators, "obfuscated_import")
	case "conditional":
		score += 15
		indicators = append(indicators, "conditional_import")
	case "dynamic":
		score += 20
		indicators = append(indicators, "dynamic_import")
	case "bundler":
		score += 5
		indicators = append(indicators, "bundled_dependency")
	}
	
	// Factor 2: Package classification (0-40 points)
	pkgType := s.classifyPackage(pkg)
	switch pkgType {
	case "scoped-private":
		score += 40
		indicators = append(indicators, "private_scoped_package")
	case "company-specific":
		score += 20
		indicators = append(indicators, "company_specific_package")
	case "standard":
		score += 10
		indicators = append(indicators, "standard_package")
	case "scoped-public":
		score += 0 // No points for known public packages
	}
	
	// Factor 3: NPM registry status (0-30 points)
	if npmInfo != nil {
		if !npmInfo.Exists {
			// Package doesn't exist on npm - HIGH RISK
			score += 30
			indicators = append(indicators, "not_on_npm")
		} else if npmInfo.Public {
			// Package exists and is public
			if npmInfo.Downloads < 100 {
				// Low download count - suspicious
				score += 10
				indicators = append(indicators, "low_npm_downloads")
			}
			if npmInfo.Version == "" || npmInfo.Version == "0.0.0" || npmInfo.Version == "0.0.1" {
				// Suspicious version
				score += 5
				indicators = append(indicators, "suspicious_version")
			}
		}
	}
	
	// Factor 4: Package name patterns (0-20 points)
	if strings.Contains(strings.ToLower(pkg), "internal") {
		score += 10
		indicators = append(indicators, "internal_keyword")
	}
	if strings.Contains(strings.ToLower(pkg), "private") {
		score += 10
		indicators = append(indicators, "private_keyword")
	}
	if strings.Contains(strings.ToLower(pkg), "test") || strings.Contains(strings.ToLower(pkg), "dev") {
		score += 5
		indicators = append(indicators, "dev_test_keyword")
	}
	if strings.Contains(pkg, "-internal") || strings.Contains(pkg, "-private") {
		score += 10
		indicators = append(indicators, "internal_suffix")
	}
	
	// Factor 5: Typosquatting detection (0-10 points)
	if s.detectTyposquatting(pkg) {
		score += 10
		indicators = append(indicators, "possible_typosquatting")
	}
	
	// Convert score to risk level
	var riskLevel string
	if score >= 70 {
		riskLevel = "critical"
	} else if score >= 50 {
		riskLevel = "high"
	} else if score >= 30 {
		riskLevel = "medium"
	} else {
		riskLevel = "low"
	}
	
	return riskLevel, score
}

// Detect potential typosquatting attempts
func (s *Scanner) detectTyposquatting(pkg string) bool {
	// Common typosquatting patterns
	popularPackages := []string{
		"react", "angular", "vue", "express", "lodash", "axios", "jquery",
		"webpack", "babel", "typescript", "node-sass", "bootstrap",
	}
	
	pkg = strings.ToLower(pkg)
	pkg = strings.TrimPrefix(pkg, "@")
	if strings.Contains(pkg, "/") {
		parts := strings.Split(pkg, "/")
		pkg = parts[len(parts)-1]
	}
	
	for _, popular := range popularPackages {
		// Check for common typos
		if s.isTyposquatVariant(pkg, popular) {
			return true
		}
	}
	
	return false
}

// Check if a package name is a typosquatting variant
func (s *Scanner) isTyposquatVariant(pkg, original string) bool {
	// Check for character substitution (react -> r3act)
	if len(pkg) == len(original) && s.stringDistance(pkg, original) == 1 {
		return true
	}
	
	// Check for common prefixes/suffixes
	suspiciousPrefixes := []string{"node-", "js-", "@"}
	suspiciousSuffixes := []string{"-js", "-node", "-npm", "js"}
	
	for _, prefix := range suspiciousPrefixes {
		if strings.HasPrefix(pkg, prefix+original) {
			return true
		}
	}
	
	for _, suffix := range suspiciousSuffixes {
		if strings.HasSuffix(pkg, original+suffix) {
			return true
		}
	}
	
	// Check for character swapping (react -> raect)
	if len(pkg) == len(original) {
		differences := 0
		for i := 0; i < len(pkg); i++ {
			if pkg[i] != original[i] {
				differences++
			}
		}
		if differences <= 2 {
			return true
		}
	}
	
	return false
}

// Simple string distance calculation
func (s *Scanner) stringDistance(s1, s2 string) int {
	if len(s1) != len(s2) {
		return -1
	}
	
	distance := 0
	for i := 0; i < len(s1); i++ {
		if s1[i] != s2[i] {
			distance++
		}
	}
	
	return distance
}

// Create an enhanced dependency with npm validation and risk assessment
func (s *Scanner) createEnhancedDependency(pkg, source, method string, lineNum int, extract, context string) Dependency {
	var indicators []string
	
	// Check npm registry if it's a valid package name
	var npmInfo *NPMPackageInfo
	
	// Don't check NPM for subpath imports from known public packages
	isSubpathOfPublicPackage := false
	if strings.Contains(pkg, "/") && strings.Count(pkg, "/") > 1 {
		// This might be a subpath import like @babel/runtime/helpers/typeof
		parts := strings.Split(pkg, "/")
		if len(parts) >= 3 {
			org := parts[0]
			if publicScopedPackages[org] {
				isSubpathOfPublicPackage = true
				s.log(fmt.Sprintf("DEBUG: Skipping NPM check for subpath %s of public org %s", pkg, org), "debug")
			}
		}
	}
	
	if !isSubpathOfPublicPackage && (scopedPackagePattern.MatchString(pkg) || !strings.Contains(pkg, "/")) {
		npmInfo = s.checkNPMPackage(pkg)
	}
	
	// Check GitHub if npm has repository info
	var githubInfo *GitHubRepoInfo
	if npmInfo != nil && npmInfo.Repository != "" {
		githubInfo = s.checkGitHubRepo(npmInfo.Repository)
	}
	
	// Assess risk with all available information
	risk, riskScore := s.assessRisk(pkg, method, npmInfo, indicators)
	
	// Create dependency with enhanced information
	dep := Dependency{
		Package:       pkg,
		Type:          s.classifyPackage(pkg),
		Source:        source,
		Method:        method,
		Risk:          risk,
		RiskScore:     riskScore,
		CodeExtract:   extract,
		LineNumber:    lineNum,
		Context:       context,
		NPMInfo:       npmInfo,
		GitHubInfo:    githubInfo,
		Indicators:    indicators,
	}
	
	// Override classification if npm says it doesn't exist
	if npmInfo != nil && !npmInfo.Exists {
		dep.Type = "scoped-private"
	}
	
	return dep
}

func (s *Scanner) decodeBase64(encoded string) string {
	// Tentative de décodage base64 simple
	// Implementation simplifiée - dans un vrai outil il faudrait une lib
	return "" // TODO: implement proper base64 decoding
}

func (s *Scanner) extractOrganizations(deps []Dependency) []string {
	orgs := make(map[string]bool)
	
	for _, dep := range deps {
		if scopedPackagePattern.MatchString(dep.Package) {
			// Extraire @org de @org/package
			parts := strings.Split(dep.Package, "/")
			if len(parts) > 0 {
				org := parts[0]
				orgs[org] = true
			}
		}
	}
	
	var result []string
	for org := range orgs {
		result = append(result, org)
	}
	
	sort.Strings(result)
	return result
}

// Check if a package exists on npm registry
func (s *Scanner) checkNPMPackage(packageName string) *NPMPackageInfo {
	// Check cache first
	s.npmMutex.RLock()
	if cached, exists := s.npmCache[packageName]; exists {
		// Cache valid for 1 hour
		if time.Since(cached.CheckedAt) < time.Hour {
			s.npmMutex.RUnlock()
			return cached
		}
	}
	s.npmMutex.RUnlock()
	
	// Prepare NPM registry URL
	npmURL := fmt.Sprintf("https://registry.npmjs.org/%s", packageName)
	if strings.HasPrefix(packageName, "@") {
		// Scoped packages need URL encoding
		npmURL = fmt.Sprintf("https://registry.npmjs.org/%s", url.QueryEscape(packageName))
	}
	
	info := &NPMPackageInfo{
		CheckedAt: time.Now(),
	}
	
	// Create request with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", npmURL, nil)
	if err != nil {
		s.log(fmt.Sprintf("Error creating npm request for %s: %v", packageName, err), "debug")
		info.Exists = false
		s.cacheNPMInfo(packageName, info)
		return info
	}
	
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json")
	
	resp, err := s.client.Do(req)
	if err != nil {
		s.log(fmt.Sprintf("Error checking npm for %s: %v", packageName, err), "debug")
		info.Exists = false
		s.cacheNPMInfo(packageName, info)
		return info
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		// Package does not exist on npm - PRIVATE!
		info.Exists = false
		info.Public = false
		s.cacheNPMInfo(packageName, info)
		return info
	}
	
	if resp.StatusCode == 200 {
		// Package exists on npm - PUBLIC
		info.Exists = true
		info.Public = true
		
		// Parse response for additional info
		var npmData map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&npmData); err == nil {
			if version, ok := npmData["dist-tags"].(map[string]interface{}); ok {
				if latest, ok := version["latest"].(string); ok {
					info.Version = latest
				}
			}
			if desc, ok := npmData["description"].(string); ok {
				info.Description = desc
			}
			if repo, ok := npmData["repository"].(map[string]interface{}); ok {
				if url, ok := repo["url"].(string); ok {
					info.Repository = url
				}
			}
		}
		
		s.cacheNPMInfo(packageName, info)
		return info
	}
	
	// Unknown status
	info.Exists = false
	s.cacheNPMInfo(packageName, info)
	return info
}

func (s *Scanner) cacheNPMInfo(packageName string, info *NPMPackageInfo) {
	s.npmMutex.Lock()
	defer s.npmMutex.Unlock()
	
	// Prevent memory exhaustion: limit cache size
	maxCacheSize := 10000
	if len(s.npmCache) >= maxCacheSize {
		// Clear oldest entries (simple approach: clear half the cache)
		for k := range s.npmCache {
			delete(s.npmCache, k)
			if len(s.npmCache) <= maxCacheSize/2 {
				break
			}
		}
	}
	
	s.npmCache[packageName] = info
}

// Check GitHub repository information
func (s *Scanner) checkGitHubRepo(repoPath string) *GitHubRepoInfo {
	// Extract owner/repo from various formats
	// e.g., "github.com/owner/repo", "https://github.com/owner/repo.git"
	repoPath = strings.TrimPrefix(repoPath, "https://")
	repoPath = strings.TrimPrefix(repoPath, "http://")
	repoPath = strings.TrimPrefix(repoPath, "github.com/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	
	// Check cache first
	s.githubMutex.RLock()
	if cached, exists := s.githubCache[repoPath]; exists {
		if time.Since(cached.CheckedAt) < time.Hour {
			s.githubMutex.RUnlock()
			return cached
		}
	}
	s.githubMutex.RUnlock()
	
	info := &GitHubRepoInfo{
		CheckedAt: time.Now(),
	}
	
	// Use GitHub API (no auth for basic checks)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s", repoPath)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		info.Exists = false
		s.cacheGitHubInfo(repoPath, info)
		return info
	}
	
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	resp, err := s.client.Do(req)
	if err != nil {
		info.Exists = false
		s.cacheGitHubInfo(repoPath, info)
		return info
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		info.Exists = false
		info.Public = false
	} else if resp.StatusCode == 200 {
		info.Exists = true
		info.Public = true
		
		// Parse response for additional info
		var ghData map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&ghData); err == nil {
			if stars, ok := ghData["stargazers_count"].(float64); ok {
				info.Stars = int(stars)
			}
			if forks, ok := ghData["forks_count"].(float64); ok {
				info.Forks = int(forks)
			}
			if lang, ok := ghData["language"].(string); ok {
				info.Language = lang
			}
			if topics, ok := ghData["topics"].([]interface{}); ok {
				for _, t := range topics {
					if topic, ok := t.(string); ok {
						info.Topics = append(info.Topics, topic)
					}
				}
			}
		}
	}
	
	s.cacheGitHubInfo(repoPath, info)
	return info
}

func (s *Scanner) cacheGitHubInfo(repoPath string, info *GitHubRepoInfo) {
	s.githubMutex.Lock()
	s.githubCache[repoPath] = info
	s.githubMutex.Unlock()
}

func (s *Scanner) deduplicateSlice(slice []string) []string {
	keys := make(map[string]bool)
	var result []string
	
	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}
	
	return result
}

// Méthode principale de scan (comme reconjsx)
func (s *Scanner) ScanTarget(targetURL string) (*ScanResult, error) {
	startTime := time.Now()
	s.log("🔍 Starting dependency reconnaissance scan", "info")
	
	// Phase 1: Découverte des fichiers JS
	jsFiles, err := s.discoverJSFiles(targetURL)
	if err != nil {
		return nil, err
	}
	
	if len(jsFiles) == 0 {
		s.log("No JavaScript files found", "warning")
		return &ScanResult{}, nil
	}
	
	var allDeps []Dependency
	
	// Phase 2: Analyse de chaque fichier JS
	for i, jsURL := range jsFiles {
		if s.debug {
			s.log(fmt.Sprintf("Analyzing %d/%d: %s", i+1, len(jsFiles), jsURL), "debug")
		}
		
		// Télécharger le fichier JS
		resp, err := s.client.Get(jsURL)
		if err != nil {
			if s.debug {
				s.log(fmt.Sprintf("Error fetching %s: %v", jsURL, err), "debug")
			}
			continue
		}
		
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		
		content := string(body)
		
		// Analyser les dépendances
		deps := s.analyzeDependencies(jsURL, content)
		allDeps = append(allDeps, deps...)
		
		// Délai entre les requêtes (comme reconjsx)
		if s.delay > 0 {
			time.Sleep(s.delay)
		}
	}
	
	// Phase 3: Filtrer les packages intéressants et construire le résultat
	filteredDeps := s.filterInterestingPackages(allDeps)
	organizations := s.extractOrganizations(filteredDeps)
	risks := s.analyzeRisks(filteredDeps)
	
	result := &ScanResult{
		Dependencies:  filteredDeps,
		Organizations: organizations,
		Risks:         risks,
	}
	
	// Remplir le summary (count from original allDeps, but show filtered results)
	result.Summary.Target = targetURL
	result.Summary.JSFilesAnalyzed = len(jsFiles)
	result.Summary.DependenciesFound = len(filteredDeps) // Only count interesting packages
	result.Summary.OrganizationsFound = len(organizations)
	result.Summary.ScanDuration = time.Since(startTime)
	result.Summary.StartTime = startTime
	
	// Compter les packages privés et obfusqués (from filtered results)
	for _, dep := range filteredDeps {
		// Count only interesting packages (high/medium risk)
		if dep.Type == "scoped-private" {
			result.Summary.PrivatePackages++
		}
		if dep.Type == "scoped" || dep.Type == "scoped-public" || dep.Type == "scoped-private" || dep.Type == "company-specific" {
			result.Summary.ScopedPackages++ // Keep for backward compatibility
		}
		if dep.Type == "obfuscated" {
			result.Summary.ObfuscatedDeps++
		}
	}
	
	return result, nil
}

func (s *Scanner) analyzeRisks(deps []Dependency) []RiskItem {
	var risks []RiskItem
	
	highRiskPkgs := []string{}
	mediumRiskPkgs := []string{}
	lowRiskPkgs := []string{}
	
	for _, dep := range deps {
		switch dep.Risk {
		case "high":
			highRiskPkgs = append(highRiskPkgs, dep.Package)
		case "medium":
			mediumRiskPkgs = append(mediumRiskPkgs, dep.Package)
		case "low":
			lowRiskPkgs = append(lowRiskPkgs, dep.Package)
		}
	}
	
	if len(highRiskPkgs) > 0 {
		risks = append(risks, RiskItem{
			Level:       "HIGH",
			Type:        "Obfuscated Dependencies",
			Description: fmt.Sprintf("Found %d potentially obfuscated package references", len(highRiskPkgs)),
			Packages:    highRiskPkgs,
		})
	}
	
	if len(mediumRiskPkgs) > 0 {
		risks = append(risks, RiskItem{
			Level:       "MEDIUM",
			Type:        "Scoped/Conditional Dependencies",
			Description: fmt.Sprintf("Found %d scoped or conditional dependencies", len(mediumRiskPkgs)),
			Packages:    mediumRiskPkgs,
		})
	}
	
	if len(lowRiskPkgs) > 0 {
		risks = append(risks, RiskItem{
			Level:       "LOW",
			Type:        "Standard Dependencies",
			Description: fmt.Sprintf("Found %d standard dependencies", len(lowRiskPkgs)),
			Packages:    lowRiskPkgs,
		})
	}
	
	return risks
}

// Mass Scanning Functions

func NewMassScanner(scanner *Scanner, workers int, timeout time.Duration, httpsFirst, onlyScoped, quiet bool, outputDir string) *MassScanner {
	return &MassScanner{
		scanner:    scanner,
		workers:    workers,
		timeout:    timeout,
		httpsFirst: httpsFirst,
		onlyScoped: onlyScoped,
		outputDir:  outputDir,
		quiet:      quiet,
		stats: &ScanStats{
			StartTime: time.Now(),
		},
	}
}

func (ms *MassScanner) ScanDomainsList(domainsFile string) error {
	// Create output directories
	err := os.MkdirAll(ms.outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output dir: %v", err)
	}

	highValueDir := filepath.Join(ms.outputDir, "high_value_targets")
	err = os.MkdirAll(highValueDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create high value dir: %v", err)
	}

	// Read domains file
	domains, err := ms.readDomainsFile(domainsFile)
	if err != nil {
		return fmt.Errorf("failed to read domains file: %v", err)
	}

	atomic.StoreInt64(&ms.stats.TotalDomains, int64(len(domains)))

	fmt.Printf("🚀 ReconDeps v%s - Mass Scanner\n", VERSION)
	fmt.Printf("================================\n")
	fmt.Printf("📁 Domains file: %s\n", domainsFile)
	fmt.Printf("📊 Total domains: %d\n", len(domains))
	fmt.Printf("⚙️ Workers: %d\n", ms.workers)
	fmt.Printf("⏱️ Timeout: %v\n", ms.timeout)
	fmt.Printf("📁 Output directory: %s\n", ms.outputDir)
	fmt.Printf("🎯 Only scoped packages: %t\n", ms.onlyScoped)
	fmt.Printf("\n")

	// Start progress reporter
	stopProgress := make(chan bool)
	go ms.reportProgress(stopProgress)

	// Process domains concurrently
	domainChan := make(chan string, ms.workers*2)
	resultChan := make(chan DomainResult, ms.workers*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < ms.workers; i++ {
		wg.Add(1)
		go ms.worker(domainChan, resultChan, &wg)
	}

	// Start result processor
	var processorWg sync.WaitGroup
	processorWg.Add(1)
	go ms.processResults(resultChan, &processorWg)

	// Send domains to workers
	go func() {
		defer close(domainChan)
		for _, domain := range domains {
			domainChan <- domain
		}
	}()

	// Wait for all workers to finish
	wg.Wait()
	close(resultChan)

	// Wait for result processor to finish
	processorWg.Wait()

	// Stop progress reporter
	stopProgress <- true

	// Print final stats
	ms.printFinalStats()

	return nil
}

func (ms *MassScanner) ScanDomainsListSample(domainsFile string, sampleSize int) error {
	// Read all domains first
	allDomains, err := ms.readDomainsFile(domainsFile)
	if err != nil {
		return fmt.Errorf("failed to read domains file: %v", err)
	}

	// Limit to sample size
	domains := allDomains
	if sampleSize > 0 && sampleSize < len(allDomains) {
		domains = allDomains[:sampleSize]
	}

	// Create output directories
	err = os.MkdirAll(ms.outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output dir: %v", err)
	}

	highValueDir := filepath.Join(ms.outputDir, "high_value_targets")
	err = os.MkdirAll(highValueDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create high value dir: %v", err)
	}

	atomic.StoreInt64(&ms.stats.TotalDomains, int64(len(domains)))

	fmt.Printf("🚀 ReconDeps v%s - Mass Scanner (Sample Mode)\n", VERSION)
	fmt.Printf("============================================\n")
	fmt.Printf("📁 Domains file: %s\n", domainsFile)
	fmt.Printf("🧪 Sample size: %d (from %d total)\n", len(domains), len(allDomains))
	fmt.Printf("⚙️ Workers: %d\n", ms.workers)
	fmt.Printf("⏱️ Timeout: %v\n", ms.timeout)
	fmt.Printf("📁 Output directory: %s\n", ms.outputDir)
	fmt.Printf("🎯 Only scoped packages: %t\n", ms.onlyScoped)
	fmt.Printf("\n")

	// Start progress reporter
	stopProgress := make(chan bool)
	go ms.reportProgress(stopProgress)

	// Process domains concurrently
	domainChan := make(chan string, ms.workers*2)
	resultChan := make(chan DomainResult, ms.workers*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < ms.workers; i++ {
		wg.Add(1)
		go ms.worker(domainChan, resultChan, &wg)
	}

	// Start result processor
	var processorWg sync.WaitGroup
	processorWg.Add(1)
	go ms.processResults(resultChan, &processorWg)

	// Send domains to workers
	go func() {
		defer close(domainChan)
		for _, domain := range domains {
			domainChan <- domain
		}
	}()

	// Wait for all workers to finish
	wg.Wait()
	close(resultChan)

	// Wait for result processor to finish
	processorWg.Wait()

	// Stop progress reporter
	stopProgress <- true

	// Print final stats
	ms.printFinalStats()

	return nil
}

func (ms *MassScanner) readDomainsFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" && !strings.HasPrefix(domain, "#") {
			domains = append(domains, domain)
		}
	}

	return domains, scanner.Err()
}

func (ms *MassScanner) worker(domainChan <-chan string, resultChan chan<- DomainResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for domain := range domainChan {
		result := ms.scanSingleDomain(domain)
		resultChan <- result
		atomic.AddInt64(&ms.stats.ProcessedDomains, 1)
	}
}

func (ms *MassScanner) scanSingleDomain(domain string) DomainResult {
	result := DomainResult{
		Domain:    domain,
		Timestamp: time.Now(),
	}

	// Try HTTPS first, then HTTP
	urls := []string{}
	if ms.httpsFirst {
		urls = []string{"https://" + domain, "http://" + domain}
	} else {
		urls = []string{"http://" + domain, "https://" + domain}
	}

	for _, url := range urls {
		// Create a timeout context for this specific scan
		ctx, cancel := context.WithTimeout(context.Background(), ms.timeout)
		defer cancel()

		// Scan with timeout
		scanResult, err := ms.scanWithTimeout(ctx, url)
		if err != nil {
			continue // Try next URL
		}

		result.Success = true
		result.Result = scanResult
		atomic.AddInt64(&ms.stats.SuccessfulScans, 1)

		// Update stats
		if scanResult.Summary.JSFilesAnalyzed > 0 {
			atomic.AddInt64(&ms.stats.WithJS, 1)
		}
		if scanResult.Summary.PrivatePackages > 0 {
			atomic.AddInt64(&ms.stats.WithScoped, 1)
		}

		break
	}

	if !result.Success {
		result.Error = "Failed to scan domain with both HTTP and HTTPS"
	}

	return result
}

func (ms *MassScanner) scanWithTimeout(ctx context.Context, url string) (*ScanResult, error) {
	// Channel to receive scan result
	resultChan := make(chan *ScanResult, 1)
	errorChan := make(chan error, 1)

	// Run scan in goroutine
	go func() {
		scanner := NewScanner(false, 2, 50*time.Millisecond) // Optimized for mass scanning
		result, err := scanner.ScanTarget(url)
		if err != nil {
			errorChan <- err
		} else {
			resultChan <- result
		}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("scan timeout")
	}
}

func (ms *MassScanner) processResults(resultChan <-chan DomainResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for result := range resultChan {
		// Save individual result
		ms.saveResult(result)

		// Display real-time results
		ms.displayRealtimeResult(result)

		// If high value target, save to special directory
		if result.Success && result.Result != nil && result.Result.Summary.PrivatePackages > 0 {
			ms.saveHighValueTarget(result)
		}
	}
}

func (ms *MassScanner) displayRealtimeResult(result DomainResult) {
	if !result.Success {
		if !ms.isQuiet() {
			fmt.Printf("❌ %s - Failed to scan\n", result.Domain)
		}
		return
	}

	if result.Result == nil {
		if !ms.isQuiet() {
			fmt.Printf("⚠️ %s - No result data\n", result.Domain)
		}
		return
	}

	summary := result.Result.Summary
	
	// Build status line with icons
	status := "✅"
	details := []string{}
	
	if summary.JSFilesAnalyzed > 0 {
		status = "📄"
		details = append(details, fmt.Sprintf("%d JS files", summary.JSFilesAnalyzed))
	}
	
	if summary.DependenciesFound > 0 {
		status = "📦"
		details = append(details, fmt.Sprintf("%d deps", summary.DependenciesFound))
	}
	
	if summary.PrivatePackages > 0 {
		status = "🎯"
		details = append(details, fmt.Sprintf("\033[31m%d PRIVATE\033[0m", summary.PrivatePackages))
		
		// Always show private packages (even in quiet mode)
		fmt.Printf("🎯 \033[32m%s\033[0m - %s\n", result.Domain, strings.Join(details, ", "))
		
		// Show the private packages details
		for _, dep := range result.Result.Dependencies {
			if dep.Type == "scoped-private" {
				fmt.Printf("  └─ \033[33m%s\033[0m (%s)\n", dep.Package, dep.Method)
			}
		}
		return
	}
	
	if summary.OrganizationsFound > 0 {
		status = "🏢"
		details = append(details, fmt.Sprintf("%d orgs", summary.OrganizationsFound))
	}
	
	// Only show if there's something interesting and not in quiet mode
	if len(details) > 0 && !ms.isQuiet() {
		fmt.Printf("%s %s - %s\n", status, result.Domain, strings.Join(details, ", "))
	} else if !ms.isQuiet() {
		fmt.Printf("⚪ %s - No dependencies found\n", result.Domain)
	}
}

func (ms *MassScanner) isQuiet() bool {
	return ms.quiet
}

func (ms *MassScanner) saveResult(result DomainResult) {
	if ms.onlyScoped && (result.Result == nil || result.Result.Summary.PrivatePackages == 0) {
		return // Skip saving if only-scoped and no private packages
	}

	filename := filepath.Join(ms.outputDir, result.Domain+".json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(filename, data, 0644)
}

func (ms *MassScanner) saveHighValueTarget(result DomainResult) {
	filename := filepath.Join(ms.outputDir, "high_value_targets", result.Domain+".json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(filename, data, 0644)
}

func (ms *MassScanner) reportProgress(stop <-chan bool) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ms.printProgress()
		case <-stop:
			return
		}
	}
}

func (ms *MassScanner) printProgress() {
	ms.stats.mutex.RLock()
	processed := atomic.LoadInt64(&ms.stats.ProcessedDomains)
	total := atomic.LoadInt64(&ms.stats.TotalDomains)
	successful := atomic.LoadInt64(&ms.stats.SuccessfulScans)
	withJS := atomic.LoadInt64(&ms.stats.WithJS)
	withScoped := atomic.LoadInt64(&ms.stats.WithScoped)
	ms.stats.mutex.RUnlock()

	elapsed := time.Since(ms.stats.StartTime)
	rate := float64(processed) / elapsed.Seconds()
	
	// Calculate progress percentage
	progress := float64(processed) / float64(total) * 100
	
	// Estimate time remaining
	var eta string
	if rate > 0 {
		remaining := float64(total-processed) / rate
		eta = fmt.Sprintf("ETA: %v", time.Duration(remaining)*time.Second)
	} else {
		eta = "ETA: calculating..."
	}

	// Color-coded output
	scopedColor := ""
	if withScoped > 0 {
		scopedColor = "\033[31m" // Red for high value targets
	}

	fmt.Printf("\n🔄 \033[36mProgress: %.1f%%\033[0m (%d/%d) | ✅ %d success | 📄 %d with JS | %s🎯 %d PRIVATE\033[0m | ⚡ %.1f/s | %s\n",
		progress, processed, total, successful, withJS, scopedColor, withScoped, rate, eta)
	
	if withScoped > 0 {
		fmt.Printf("🚨 \033[33mHIGH VALUE TARGETS DISCOVERED: %d domains with private packages!\033[0m\n", withScoped)
	}
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

func (ms *MassScanner) printFinalStats() {
	processed := atomic.LoadInt64(&ms.stats.ProcessedDomains)
	total := atomic.LoadInt64(&ms.stats.TotalDomains)
	successful := atomic.LoadInt64(&ms.stats.SuccessfulScans)
	withJS := atomic.LoadInt64(&ms.stats.WithJS)
	withScoped := atomic.LoadInt64(&ms.stats.WithScoped)

	elapsed := time.Since(ms.stats.StartTime)
	rate := float64(processed) / elapsed.Seconds()

	fmt.Printf("\n🎉 Mass scan completed!\n")
	fmt.Printf("======================\n")
	fmt.Printf("📊 Total domains: %d\n", total)
	fmt.Printf("✅ Processed: %d\n", processed)
	fmt.Printf("🟢 Successful: %d\n", successful)
	fmt.Printf("📁 With JavaScript: %d\n", withJS)
	fmt.Printf("🎯 With private packages: %d\n", withScoped)
	fmt.Printf("⏱️ Total time: %v\n", elapsed.Round(time.Second))
	fmt.Printf("⚡ Average rate: %.1f/s\n", rate)
	fmt.Printf("📁 Results saved in: %s\n", ms.outputDir)

	if withScoped > 0 {
		fmt.Printf("\n🚨 \033[31mHIGH VALUE TARGETS FOUND: %d\033[0m\n", withScoped)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		
		// Read and display high value targets details
		ms.displayHighValueTargetsSummary()
		
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("⚠️ \033[33mPOTENTIAL SUPPLY CHAIN ATTACK VECTORS IDENTIFIED!\033[0m\n")
		fmt.Printf("🎯 \033[36mCheck %s/high_value_targets/ for detailed JSON results\033[0m\n", ms.outputDir)
	}
}

func (ms *MassScanner) displayHighValueTargetsSummary() {
	highValueDir := filepath.Join(ms.outputDir, "high_value_targets")
	
	files, err := filepath.Glob(filepath.Join(highValueDir, "*.json"))
	if err != nil {
		fmt.Printf("❌ Error reading high value targets: %v\n", err)
		return
	}

	if len(files) == 0 {
		fmt.Printf("⚠️ No high value target files found\n")
		return
	}

	// Group packages by organization
	orgPackages := make(map[string][]string)
	domainInfo := make(map[string]struct {
		domain   string
		packages int
		jsFiles  int
	})

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var result DomainResult
		if err := json.Unmarshal(content, &result); err != nil {
			continue
		}

		if result.Result == nil {
			continue
		}

		domain := result.Domain
		scopedCount := 0

		for _, dep := range result.Result.Dependencies {
			if dep.Type == "scoped-private" {
				scopedCount++
				
				// Extract organization from @org/package
				if strings.HasPrefix(dep.Package, "@") {
					parts := strings.Split(dep.Package, "/")
					if len(parts) >= 2 {
						org := parts[0] // @org
						if orgPackages[org] == nil {
							orgPackages[org] = []string{}
						}
						// Avoid duplicates
						found := false
						for _, existing := range orgPackages[org] {
							if existing == dep.Package {
								found = true
								break
							}
						}
						if !found {
							orgPackages[org] = append(orgPackages[org], dep.Package)
						}
					}
				}
			}
		}

		if scopedCount > 0 {
			domainInfo[domain] = struct {
				domain   string
				packages int
				jsFiles  int
			}{
				domain:   domain,
				packages: scopedCount,
				jsFiles:  result.Result.Summary.JSFilesAnalyzed,
			}
		}
	}

	// Display by organization
	fmt.Printf("\n🏢 \033[33mPRIVATE ORGANIZATIONS DISCOVERED:\033[0m\n")
	for org, packages := range orgPackages {
		fmt.Printf("\n📋 \033[32m%s\033[0m (%d private packages)\n", org, len(packages))
		for _, pkg := range packages {
			fmt.Printf("  └─ \033[36m%s\033[0m\n", pkg)
		}
	}

	// Display by domain
	fmt.Printf("\n🎯 \033[33mHIGH VALUE DOMAINS:\033[0m\n")
	for _, info := range domainInfo {
		fmt.Printf("  🌐 \033[32m%s\033[0m - %d private packages (%d JS files)\n", 
			info.domain, info.packages, info.jsFiles)
	}
}

func (s *Scanner) displayResults(result *ScanResult) {
	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	s.log(fmt.Sprintf("ReconDeps v%s - Target: %s", VERSION, result.Summary.Target), "info")
	s.log(fmt.Sprintf("Found %d dependencies in %d JavaScript files", result.Summary.DependenciesFound, result.Summary.JSFilesAnalyzed), "info")
	s.log(fmt.Sprintf("Scan completed in %v", result.Summary.ScanDuration.Round(time.Millisecond)), "info")
	
	// Grouper par organisations pour le bug bounty
	orgMap := make(map[string][]Dependency)
	scopedDeps := []Dependency{}
	standardDeps := []Dependency{}
	
	for _, dep := range result.Dependencies {
		if dep.Type == "scoped" {
			scopedDeps = append(scopedDeps, dep)
			// Extraire l'organisation
			parts := strings.Split(dep.Package, "/")
			if len(parts) > 0 {
				org := parts[0]
				orgMap[org] = append(orgMap[org], dep)
			}
		} else {
			standardDeps = append(standardDeps, dep)
		}
	}
	
	// Afficher les organisations (priorité bug bounty)
	if len(orgMap) > 0 {
		fmt.Printf("\n%s\n", strings.Repeat("=", 80))
		s.log("🎯 PRIVATE/SCOPED PACKAGES BY ORGANIZATION (Bug Bounty Targets):", "success")
		
		// Trier par organisation
		var orgs []string
		for org := range orgMap {
			orgs = append(orgs, org)
		}
		sort.Strings(orgs)
		
		for _, org := range orgs {
			fmt.Printf("\n\033[33m[ORG]\033[0m %s\n", org)
			deps := orgMap[org]
			
			for _, dep := range deps {
				fmt.Printf("\n\033[32m[PKG]\033[0m %s\n", dep.Package)
				fmt.Printf("      📍 Source: %s (line %d)\n", dep.Source, dep.LineNumber)
				fmt.Printf("      🔍 Context: %s\n", dep.Context)
				fmt.Printf("      ⚠️  Risk: %s\n", dep.Risk)
				
				// Afficher l'extrait de code (crucial pour bug bounty)
				fmt.Printf("      📝 Code Extract:\n")
				for _, line := range strings.Split(dep.CodeExtract, "\n") {
					fmt.Printf("         %s\n", line)
				}
			}
		}
	}
	
	// Afficher les dépendances standard si pertinentes
	if len(standardDeps) > 0 && s.debug {
		fmt.Printf("\n%s\n", strings.Repeat("-", 60))
		s.log("Standard Dependencies (debug mode):", "info")
		for _, dep := range standardDeps {
			fmt.Printf("  %s (%s:%d)\n", dep.Package, dep.Source, dep.LineNumber)
		}
	}
	
	// Résumé pour bug bounty
	fmt.Printf("\n%s\n", strings.Repeat("=", 80))
	s.log("🔍 BUG BOUNTY SUMMARY:", "warning")
	fmt.Printf("  🎯 Private packages found: %d\n", len(scopedDeps))
	fmt.Printf("  🏢 Organizations identified: %d\n", len(orgMap))
	fmt.Printf("  📁 JavaScript files analyzed: %d\n", result.Summary.JSFilesAnalyzed)
	fmt.Printf("  ⚡ Total scan time: %v\n", result.Summary.ScanDuration.Round(time.Millisecond))
	
	if len(scopedDeps) > 0 {
		fmt.Printf("\n\033[31m⚠️  POTENTIAL SUPPLY CHAIN ATTACK VECTORS IDENTIFIED\033[0m\n")
	}
}

func main() {
	// Single URL scanning flags
	var (
		targetURL   = flag.String("url", "", "Target URL to scan")
		debug       = flag.Bool("debug", false, "Enable debug output")
		jsonOutput  = flag.Bool("json", false, "Output results as JSON")
		outputFile  = flag.String("output", "", "Save results to file")
		maxDepth    = flag.Int("depth", 3, "Maximum crawling depth")
		delay       = flag.Duration("delay", 100*time.Millisecond, "Delay between requests")
		version     = flag.Bool("version", false, "Show version")
		help        = flag.Bool("help", false, "Show help")
	)

	// Mass scanning flags
	var (
		massScan    = flag.String("mass", "", "Mass scan from domains file")
		workers     = flag.Int("workers", runtime.NumCPU()*4, "Number of concurrent workers for mass scanning (max 200)")
		timeout     = flag.Duration("timeout", 30*time.Second, "Timeout per domain")
		httpsFirst  = flag.Bool("https-first", true, "Try HTTPS before HTTP")
		onlyScoped  = flag.Bool("only-scoped", false, "Only save results with scoped packages")
		outputDir   = flag.String("output-dir", "", "Output directory for mass scan results (auto-generated if empty)")
		sample      = flag.Int("sample", 0, "Test with first N domains from file (0 = all)")
		quiet       = flag.Bool("quiet", false, "Quiet mode - only show high value targets and progress")
	)
	
	flag.Parse()
	
	if *version {
		fmt.Printf("ReconDeps v%s - Dependency Reconnaissance Tool\n", VERSION)
		fmt.Println("Inspired by reconjsx architecture for supply chain analysis")
		fmt.Println("🎯 Perfect for Bug Bounty Supply Chain Analysis")
		return
	}
	
	if *help || (*targetURL == "" && *massScan == "") {
		fmt.Printf(`
ReconDeps v%s - JavaScript Dependency Reconnaissance Tool
🎯 Bug Bounty Edition - Supply Chain Analysis

Usage: %s [mode] [options]

SINGLE URL MODE:
  -url string     Target URL to scan (required for single mode)
  -debug          Enable debug output
  -json           Output results as JSON
  -output string  Save results to file
  -depth int      Maximum crawling depth (default 3)
  -delay duration Delay between requests (default 100ms)

MASS SCANNING MODE:
  -mass string        Domains file for mass scanning (required for mass mode)
  -workers int        Number of concurrent workers (default: CPU*4)
  -timeout duration   Timeout per domain (default: 30s)
  -https-first        Try HTTPS before HTTP (default: true)
  -only-scoped        Only save results with scoped packages
  -output-dir string  Output directory (auto-generated if empty)
  -sample int         Test with first N domains (0 = all)
  -quiet              Quiet mode - only show high value targets and progress

GENERAL:
  -version        Show version
  -help           Show this help

EXAMPLES:

Single URL scan:
  %s -url https://example.com
  %s -url https://example.com -debug -json

Mass scanning (Bug Bounty):
  %s -mass tests/alldomains.txt -workers 100 -only-scoped
  %s -mass tests/alldomains.txt -workers 200 -timeout 15s
  %s -mass tests/alldomains.txt -sample 50 -debug

High performance (20k+ domains):
  %s -mass tests/alldomains.txt -workers 200 -timeout 15s -only-scoped

`, VERSION, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0])
		return
	}

	// MASS SCANNING MODE
	if *massScan != "" {
		// Auto-generate output directory if not specified
		if *outputDir == "" {
			*outputDir = fmt.Sprintf("mass_scan_%s", time.Now().Format("20060102_150405"))
		}

		// Limit workers to prevent resource exhaustion (OOM killer)
		maxWorkers := 200
		if *workers > maxWorkers {
			fmt.Printf("⚠️ Workers limited to %d (requested: %d) to prevent memory issues\n", maxWorkers, *workers)
			*workers = maxWorkers
		}

		scanner := NewScanner(*debug, 2, 50*time.Millisecond) // Optimized for mass scanning
		massScanner := NewMassScanner(scanner, *workers, *timeout, *httpsFirst, *onlyScoped, *quiet, *outputDir)

		// Handle sample mode
		if *sample > 0 {
			fmt.Printf("🧪 Sample mode: Testing first %d domains\n", *sample)
			err := massScanner.ScanDomainsListSample(*massScan, *sample)
			if err != nil {
				log.Fatalf("Error in sample mass scan: %v", err)
			}
		} else {
			err := massScanner.ScanDomainsList(*massScan)
			if err != nil {
				log.Fatalf("Error in mass scan: %v", err)
			}
		}
		return
	}

	// SINGLE URL MODE (original functionality)
	if *targetURL == "" {
		fmt.Println("❌ Error: Either -url or -mass flag is required")
		fmt.Printf("Use %s -help for usage information\n", os.Args[0])
		os.Exit(1)
	}

	scanner := NewScanner(*debug, *maxDepth, *delay)
	
	result, err := scanner.ScanTarget(*targetURL)
	if err != nil {
		log.Fatalf("Error scanning target: %v", err)
	}
	
	if *jsonOutput {
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			log.Fatalf("Error marshaling JSON: %v", err)
		}
		fmt.Println(string(jsonData))
	} else {
		scanner.displayResults(result)
	}
	
	if *outputFile != "" {
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			log.Fatalf("Error marshaling JSON: %v", err)
		}
		
		err = os.WriteFile(*outputFile, jsonData, 0644)
		if err != nil {
			log.Fatalf("Error writing output file: %v", err)
		}
		
		scanner.log(fmt.Sprintf("Results saved to %s", *outputFile), "success")
	}
}