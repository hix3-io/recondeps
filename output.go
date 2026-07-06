package main

import (
	"fmt"
	"strings"
)

func displayHuman(res *ScanResult) {
	line := strings.Repeat("=", 78)
	fmt.Printf("\n%s\n", line)
	fmt.Printf("recondeps-ng v%s — %s\n", VERSION, res.Target)
	if res.Error != "" {
		fmt.Printf("\033[31merror: %s\033[0m\n", res.Error)
		return
	}
	fmt.Printf("JS files: %d | source maps: %d | deps: %d | duration: %s\n",
		res.JSFiles, res.SourceMaps, res.Summary.Total, res.Duration)
	fmt.Printf("%s\n", line)

	if len(res.Dependencies) == 0 && len(res.Registries) == 0 {
		fmt.Printf("no dependencies of interest found\n")
		return
	}

	byExpl := map[string][]Dependency{}
	for _, d := range res.Dependencies {
		byExpl[d.Exploitability] = append(byExpl[d.Exploitability], d)
	}
	order := []struct{ key, label, color string }{
		{"confirmed-claimable", "🎯 CONFIRMED CLAIMABLE (dependency-confusion ready)", "31"},
		{"likely", "⚠️  LIKELY CLAIMABLE", "33"},
		{"unknown", "❔ UNKNOWN (resolution inconclusive)", "36"},
		{"unlikely", "· unlikely", "90"},
		{"not-applicable", "· public/known", "90"},
	}
	for _, o := range order {
		deps := byExpl[o.key]
		if len(deps) == 0 {
			continue
		}
		fmt.Printf("\n\033[%sm%s (%d)\033[0m\n", o.color, o.label, len(deps))
		for _, d := range deps {
			printDep(d)
		}
	}

	if len(res.Registries) > 0 {
		fmt.Printf("\n\033[35m🏭 INTERNAL REGISTRIES (%d)\033[0m\n", len(res.Registries))
		for _, r := range res.Registries {
			scope := ""
			if r.Scope != "" {
				scope = "  (bound to " + r.Scope + ")"
			}
			fmt.Printf("  \033[35m%-16s\033[0m %s%s\n", r.Type, r.URL, scope)
			fmt.Printf("      seen in: %s\n", r.Source)
		}
	}

	fmt.Printf("\n%s\n", line)
	fmt.Printf("SUMMARY: %d deps | %d private | %d confirmed-claimable | %d likely | %d high/critical | %d registries\n",
		res.Summary.Total, res.Summary.Private, res.Summary.Confirmed, res.Summary.Likely, res.Summary.HighAndAbove, len(res.Registries))
	if res.Summary.Confirmed > 0 {
		fmt.Printf("\033[31m⚠️  %d confirmed dependency-confusion candidate(s) — verify scope ownership before acting\033[0m\n", res.Summary.Confirmed)
	}
}

func printDep(d Dependency) {
	fmt.Printf("\n  \033[32m%s\033[0m  [%s] risk=%s score=%d method=%s\n", d.Package, d.Type, d.Risk, d.RiskScore, d.Method)
	if d.NPM != nil && d.NPM.Checked {
		npm := "unknown"
		switch {
		case d.NPM.Unknown:
			npm = fmt.Sprintf("inconclusive (%s)", d.NPM.Error)
		case d.NPM.Exists:
			npm = fmt.Sprintf("published v%s", d.NPM.Version)
		default:
			npm = "NOT published (404)"
		}
		fmt.Printf("      npm: %s\n", npm)
	}
	if d.Scope_ != nil && d.Scope_.Checked {
		switch {
		case d.Scope_.KnownPublic:
			fmt.Printf("      scope %s: known public org\n", d.Scope)
		case d.Scope_.Occupied:
			fmt.Printf("      scope %s: OCCUPIED (e.g. %s) → not claimable\n", d.Scope, d.Scope_.SamplePkg)
		case d.Scope_.Claimable:
			fmt.Printf("      scope %s: \033[31mAPPEARS CLAIMABLE\033[0m\n", d.Scope)
		case d.Scope_.Unknown:
			fmt.Printf("      scope %s: check inconclusive\n", d.Scope)
		}
	}
	if len(d.Indicators) > 0 {
		fmt.Printf("      indicators: %s\n", strings.Join(d.Indicators, ", "))
	}
	if len(d.Sources) > 0 {
		src := d.Sources[0]
		if len(d.Sources) > 1 {
			src = fmt.Sprintf("%s (+%d)", src, len(d.Sources)-1)
		}
		fmt.Printf("      source: %s", src)
		if d.LineNumber > 0 {
			fmt.Printf(" (line %d, %s)", d.LineNumber, d.Context)
		}
		fmt.Printf("\n")
	}
	if d.CodeExtract != "" {
		for _, l := range strings.Split(d.CodeExtract, "\n") {
			fmt.Printf("        %s\n", l)
		}
	}
}
