package main

import "strings"

// assess computes risk score, indicators and the exploitability verdict for a
// dependency, using both static classification and resolution results.
func assess(d *Dependency) {
	score := 0
	ind := []string{}

	// 1. Detection method.
	switch d.Method {
	case "obfuscated":
		score += 25
		ind = append(ind, "obfuscated_specifier")
	case "sourcemap":
		score += 10
		ind = append(ind, "from_source_map")
	case "package-json":
		score += 10
		ind = append(ind, "exposed_manifest")
	case "bundler-path":
		score += 8
		ind = append(ind, "bundled_node_modules_path")
	case "import", "require":
		score += 5
	}

	// 2. Static classification.
	switch d.Type {
	case "scoped-private":
		score += 30
		ind = append(ind, "unknown_scope")
	case "company-specific":
		score += 18
		ind = append(ind, "company_signal_in_name")
	case "standard":
		score += 5
	}

	// 3. Name heuristics.
	low := strings.ToLower(d.Package)
	for kw, pts := range map[string]int{"internal": 12, "private": 12, "secret": 12, "-dev": 4, "-test": 4} {
		if strings.Contains(low, kw) {
			score += pts
			ind = append(ind, "keyword_"+strings.Trim(kw, "-"))
		}
	}

	// 4. Registry evidence — the decisive factor.
	d.Exploitability = "unknown"
	if d.NPM != nil && d.NPM.Checked {
		switch {
		case d.NPM.Unknown:
			ind = append(ind, "npm_check_inconclusive")
		case !d.NPM.Exists:
			// Package not published. Whether it's exploitable depends on the SCOPE.
			ind = append(ind, "not_published_on_npm")
			if d.Scope != "" {
				if d.Scope_ != nil && d.Scope_.Checked {
					switch {
					case d.Scope_.KnownPublic || d.Scope_.Occupied:
						// Scope is owned → cannot claim the name. Not a confusion target.
						score += 5
						d.Exploitability = "unlikely"
						ind = append(ind, "scope_occupied")
					case d.Scope_.Claimable:
						score += 45
						d.Exploitability = "confirmed-claimable"
						ind = append(ind, "scope_appears_claimable")
					default:
						score += 20
						d.Exploitability = "likely"
					}
				} else {
					score += 20
					d.Exploitability = "likely"
				}
			} else {
				// Unscoped missing package: classic dependency-confusion / typosquat name.
				score += 30
				d.Exploitability = "likely"
				ind = append(ind, "unscoped_name_free")
			}
		case d.NPM.Public:
			// Published & public. Low confusion value unless very fresh / low trust.
			d.Exploitability = "unlikely"
			if d.Type == "scoped-public" {
				d.Exploitability = "not-applicable"
			}
		}
	} else {
		// No resolution performed.
		if d.Type == "scoped-private" {
			d.Exploitability = "likely"
		} else if d.Type == "scoped-public" {
			d.Exploitability = "not-applicable"
		}
	}

	if score > 100 {
		score = 100
	}
	d.RiskScore = score
	d.Indicators = ind
	switch {
	case score >= 70:
		d.Risk = "critical"
	case score >= 50:
		d.Risk = "high"
	case score >= 30:
		d.Risk = "medium"
	default:
		d.Risk = "low"
	}
}
