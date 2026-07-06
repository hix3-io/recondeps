package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// VERSION is injected at build time via -ldflags, falling back to the embedded default.
var VERSION = "1.0.0"

type Options struct {
	Timeout      time.Duration
	UserAgent    string
	Cookie       string
	MaxBytes     int64
	MaxAssets    int
	Depth        int
	SameHostOnly bool
	Insecure     bool
	Debug        bool
	Resolve      bool
	Workers      int
	Quiet        bool
	Resume       bool
}

func main() {
	var (
		target    = flag.String("url", "", "single target URL")
		mass      = flag.String("mass", "", "mass scan: file with one domain per line")
		jsonOut   = flag.Bool("json", false, "emit JSON (single mode)")
		outFile   = flag.String("output", "", "write JSON result to file (single mode)")
		outDir    = flag.String("output-dir", "", "output directory (mass mode; auto if empty)")
		resolve   = flag.Bool("resolve", true, "resolve packages against npm/scope/github")
		npmRate   = flag.Int("npm-rate", 6, "global npm/github requests per second")
		ghToken   = flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token (raises API limit)")
		workers   = flag.Int("workers", runtime.NumCPU()*4, "mass-scan concurrent workers (max 200)")
		timeout   = flag.Duration("timeout", 20*time.Second, "per-request timeout")
		maxAssets = flag.Int("max-assets", 40, "max JS files fetched per target")
		depth     = flag.Int("depth", 2, "chunk-follow depth")
		maxBytes  = flag.Int64("max-bytes", 8*1024*1024, "max bytes read per file")
		allHosts  = flag.Bool("all-hosts", false, "follow JS across hosts (default same-host only)")
		insecure  = flag.Bool("insecure", true, "skip TLS verification")
		cookie    = flag.String("cookie", "", "Cookie header for authenticated scans")
		ua        = flag.String("ua", "recondeps/"+VERSION, "User-Agent")
		quiet     = flag.Bool("quiet", false, "mass mode: only print claimable targets")
		resume    = flag.Bool("resume", false, "mass mode: resume from existing results.ndjson")
		debug     = flag.Bool("debug", false, "debug output to stderr")
		showVer   = flag.Bool("version", false, "print version")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("recondeps v%s\n", VERSION)
		return
	}
	if *target == "" && *mass == "" {
		usage()
		return
	}
	if *workers > 200 {
		*workers = 200
	}

	opts := Options{
		Timeout:      *timeout,
		UserAgent:    *ua,
		Cookie:       *cookie,
		MaxBytes:     *maxBytes,
		MaxAssets:    *maxAssets,
		Depth:        *depth,
		SameHostOnly: !*allHosts,
		Insecure:     *insecure,
		Debug:        *debug,
		Resolve:      *resolve,
		Workers:      *workers,
		Quiet:        *quiet,
		Resume:       *resume,
	}

	resolver := NewResolver(*resolve, *npmRate, *ghToken, *ua, *debug)
	defer resolver.Close()

	// MASS MODE
	if *mass != "" {
		dir := *outDir
		if dir == "" {
			dir = "recondeps_" + time.Now().Format("20060102_150405")
		}
		ms := NewMassScanner(resolver, opts, dir)
		if err := ms.Run(*mass); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// SINGLE MODE
	sc := NewScanner(resolver, opts)
	res := sc.Scan(*target)

	if *jsonOut || *outFile != "" {
		data, _ := json.MarshalIndent(res, "", "  ")
		if *jsonOut {
			fmt.Println(string(data))
		}
		if *outFile != "" {
			if err := os.WriteFile(*outFile, data, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *outFile, err)
			}
		}
	} else {
		displayHuman(res)
	}
}

func usage() {
	fmt.Print(strings.ReplaceAll(`recondeps vVER — JS supply-chain reconnaissance

Finds dependency-confusion candidates by mining JavaScript for package
references (source maps, bundler paths, imports, obfuscated specifiers),
then verifying against npm — distinguishing "absent" from "couldn't check"
and answering the real question: is the @scope claimable?

SINGLE
  recondeps -url https://target.com
  recondeps -url https://target.com -json -output out.json
  recondeps -url https://target.com -no... use -resolve=false for offline

MASS
  recondeps -mass domains.txt -workers 100
  recondeps -mass domains.txt -resume            # continue an interrupted run
  recondeps -mass domains.txt -quiet -npm-rate 8

KEY FLAGS
  -resolve=false   offline: classify without hitting npm
  -depth N         how deep to follow webpack chunk refs (default 2)
  -max-assets N    cap JS files per target (default 40)
  -npm-rate N      global registry req/s across all workers (default 6)
  -github-token    or GITHUB_TOKEN env, for authenticated GitHub checks
  -all-hosts       follow JS to other hosts (default: same host only)

Authorized security testing only.
`, "VER", VERSION))
}
