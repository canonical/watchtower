package application

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mclemenceau/watchtower/internal/domain"
)

// FormatBuildsStatusSummary renders a summary table: one row per release with
// built/total counts and a 10-square progress bar (🟩 per 10% built, 🟥 for the rest).
// The counts reflect the enriched BuildLog status when available, falling back to
// the version-date check. Returns a "no snapshot" message when artefacts is empty.
func FormatBuildsStatusSummary(artefacts []domain.Artefact) string {
	if len(artefacts) == 0 {
		return "No snapshot available yet — the first fetch is still in progress."
	}

	type releaseStat struct {
		total      int
		built      int
		inProgress int
		failed     int
		notStarted int
		unknown    int
	}
	stats := make(map[string]*releaseStat)
	for _, art := range artefacts {
		s, ok := stats[art.Release]
		if !ok {
			s = &releaseStat{}
			stats[art.Release] = s
		}
		s.total++
		switch effectiveBuildLog(art) {
		case domain.BuildStatusBuilt:
			s.built++
		case domain.BuildStatusInProgress:
			s.inProgress++
		case domain.BuildStatusFailed:
			s.failed++
		case domain.BuildStatusNotStarted:
			s.notStarted++
		default:
			s.unknown++
		}
	}

	releases := make([]string, 0, len(stats))
	for r := range stats {
		releases = append(releases, r)
	}
	sort.Strings(releases)

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Build Status** · %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	sb.WriteString("| Release | ✅ Built | 🔄 In Progress | ❌ Failed | ⏳ Not Started | ❓ Unknown | Total | Progress |\n")
	sb.WriteString("|---------|---------|--------------|---------|-------------|---------|-------|----------|\n")
	for _, r := range releases {
		s := stats[r]
		pct := 0
		if s.total > 0 {
			pct = s.built * 100 / s.total
		}
		green := pct / 10
		red := 10 - green
		bar := strings.Repeat("🟩", green) + strings.Repeat("🟥", red)
		fmt.Fprintf(&sb, "| **%s** | %d | %d | %d | %d | %d | %d | %s |\n",
			r, s.built, s.inProgress, s.failed, s.notStarted, s.unknown, s.total, bar)
	}
	return sb.String()
}

// logCellForArtefact returns the Markdown log-link cell for an artefact row in
// the builds status table. For PRODUCT failures where a librarian URL has been
// resolved via two-hop analysis, the link points to the livefs build log
// (second hop) rather than the first-hop cd-build-log. For all other states
// the standard LogCell (cd-build-log for today) is used.
func logCellForArtefact(art domain.Artefact, failures domain.FailureStore) string {
	if art.BuildFailureKind == domain.BuildFailureKindProduct {
		if rec := failures.FindActive(art.ID, art.Release, art.OS); rec != nil &&
			rec.LivefsLogURL != "" {
			return fmt.Sprintf("[🔗](%s)", rec.LivefsLogURL)
		}
	}
	return domain.LogCell(art.ImageURL)
}

// FormatBuildsStatusRelease renders a detail table for a single release: one row per
// artefact with name, version, age and build status. When product is non-empty only
// artefacts whose OS matches (case-insensitive) are shown.
// Returns an appropriate message when no artefacts match.
// failures is used to look up the resolved livefs (Launchpad librarian) log URL for
// PRODUCT failures — those rows link to the second-hop log instead of the cd-build-log.
func FormatBuildsStatusRelease(
	artefacts []domain.Artefact,
	release, product string,
	failures domain.FailureStore,
) string {
	if len(artefacts) == 0 {
		return "No snapshot available yet — the first fetch is still in progress."
	}

	var filtered []domain.Artefact
	for _, art := range artefacts {
		if !strings.EqualFold(art.Release, release) {
			continue
		}
		if product != "" && !strings.EqualFold(art.OS, product) {
			continue
		}
		filtered = append(filtered, art)
	}

	if len(filtered) == 0 {
		if product != "" {
			return fmt.Sprintf("No artefacts found for release **%s** and product **%s**.", release, product)
		}
		return fmt.Sprintf("No artefacts found for release **%s**.", release)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].OS != filtered[j].OS {
			return filtered[i].OS < filtered[j].OS
		}
		return filtered[i].Name < filtered[j].Name
	})

	var sb strings.Builder
	if product != "" {
		fmt.Fprintf(&sb, "**Build Status** · %s · %s · %s\n\n",
			release, product, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	} else {
		fmt.Fprintf(&sb, "**Build Status** · %s · %s\n\n",
			release, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	}
	sb.WriteString("| ID | Artefact | Product | Version | Age | Build | Log |\n")
	sb.WriteString("|----|----------|---------|---------|-----|-------|-----|\n")
	for _, art := range filtered {
		fmt.Fprintf(&sb, "| %d | %s | %s | %s | %s | %s | %s |\n",
			art.ID, art.Name, art.OS, art.Version, domain.ImageAge(art.Version),
			artefactStatusCell(art), logCellForArtefact(art, failures))
	}
	return sb.String()
}

// dedupLatestPerPlan filters a slice of TestExecutions to only the most recent
// execution per test plan (by CreatedAt), discarding non-displayable entries.
// The returned slice is in arbitrary order.
func dedupLatestPerPlan(
	tes []domain.TestExecution,
) []domain.TestExecution {
	latest := make(map[string]domain.TestExecution, len(tes))
	for _, te := range tes {
		if !domain.IsDisplayable(te) {
			continue
		}
		prev, seen := latest[te.TestPlan]
		if !seen || te.CreatedAt > prev.CreatedAt {
			latest[te.TestPlan] = te
		}
	}
	out := make([]domain.TestExecution, 0, len(latest))
	for _, te := range latest {
		out = append(out, te)
	}
	return out
}

// FormatTestsStatusSummary renders a summary table of test execution status across
// all releases. Only releases that have at least one displayable test execution are
// shown. The progress bar reflects PASSED executions as a fraction of total
// displayable executions (🟩 per 10%, 🟥 for the rest).
func FormatTestsStatusSummary(artefacts []domain.Artefact) string {
	if len(artefacts) == 0 {
		return "No snapshot available yet — the first fetch is still in progress."
	}

	type releaseStat struct {
		passed int
		total  int
	}
	stats := make(map[string]*releaseStat)

	for _, art := range artefacts {
		for _, b := range art.Builds {
			for _, te := range dedupLatestPerPlan(b.TestExecutions) {
				s, ok := stats[art.Release]
				if !ok {
					s = &releaseStat{}
					stats[art.Release] = s
				}
				s.total++
				if te.Status == "PASSED" {
					s.passed++
				}
			}
		}
	}

	if len(stats) == 0 {
		return "No test executions found in snapshot — test data may still be loading."
	}

	releases := make([]string, 0, len(stats))
	for r := range stats {
		releases = append(releases, r)
	}
	sort.Strings(releases)

	var sb strings.Builder
	fmt.Fprintf(&sb, "**Test Status** · %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	sb.WriteString("| Release | Passed | Total | Progress |\n")
	sb.WriteString("|---------|--------|-------|----------|\n")
	for _, r := range releases {
		s := stats[r]
		pct := 0
		if s.total > 0 {
			pct = s.passed * 100 / s.total
		}
		green := pct / 10
		red := 10 - green
		bar := strings.Repeat("🟩", green) + strings.Repeat("🟥", red)
		fmt.Fprintf(&sb, "| **%s** | %d | %d | %s |\n", r, s.passed, s.total, bar)
	}
	return sb.String()
}

// FormatTestsStatusRelease renders a detail table for a single release. Each row
// represents one displayable test execution. Multiple executions of the same test
// plan for the same artefact+build are deduplicated to the latest only (by
// CreatedAt). Artefacts with no displayable executions are omitted. When product
// is non-empty, only artefacts whose OS matches are shown.
func FormatTestsStatusRelease(artefacts []domain.Artefact, release, product string) string {
	if len(artefacts) == 0 {
		return "No snapshot available yet — the first fetch is still in progress."
	}

	// Filter artefacts to the requested release (and optional product).
	var filtered []domain.Artefact
	for _, art := range artefacts {
		if !strings.EqualFold(art.Release, release) {
			continue
		}
		if product != "" && !strings.EqualFold(art.OS, product) {
			continue
		}
		filtered = append(filtered, art)
	}

	if len(filtered) == 0 {
		if product != "" {
			return fmt.Sprintf("No artefacts found for release **%s** and product **%s**.", release, product)
		}
		return fmt.Sprintf("No artefacts found for release **%s**.", release)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].OS != filtered[j].OS {
			return filtered[i].OS < filtered[j].OS
		}
		return filtered[i].Name < filtered[j].Name
	})

	// execRow is one displayable row in the output table.
	type execRow struct {
		artefactName string
		product      string
		arch         string
		testPlan     string
		status       string
		ciLink       string
	}

	var rows []execRow
	for _, art := range filtered {
		for _, b := range art.Builds {
			deduped := dedupLatestPerPlan(b.TestExecutions)
			if len(deduped) == 0 {
				continue
			}
			plans := make([]string, 0, len(deduped))
			byPlan := make(map[string]domain.TestExecution, len(deduped))
			for _, te := range deduped {
				plans = append(plans, te.TestPlan)
				byPlan[te.TestPlan] = te
			}
			sort.Strings(plans)
			for _, plan := range plans {
				te := byPlan[plan]
				rows = append(rows, execRow{
					artefactName: art.Name,
					product:      art.OS,
					arch:         b.Architecture,
					testPlan:     te.TestPlan,
					status:       te.Status,
					ciLink:       te.CILink,
				})
			}
		}
	}

	if len(rows) == 0 {
		if product != "" {
			return fmt.Sprintf("No test executions found for release **%s** and product **%s**.", release, product)
		}
		return fmt.Sprintf("No test executions found for release **%s**.", release)
	}

	var sb strings.Builder
	if product != "" {
		fmt.Fprintf(&sb, "**Test Status** · %s · %s · %s\n\n",
			release, product, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	} else {
		fmt.Fprintf(&sb, "**Test Status** · %s · %s\n\n",
			release, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	}
	sb.WriteString("| Artefact | Product | Arch | Test Plan | Status |\n")
	sb.WriteString("|----------|---------|------|-----------|--------|\n")
	for _, row := range rows {
		statusCell := domain.ExecStatusEmoji(row.status) + " " + row.status
		if row.ciLink != "" {
			statusCell = fmt.Sprintf("%s [%s](%s)", domain.ExecStatusEmoji(row.status), row.status, row.ciLink)
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n",
			row.artefactName, row.product, row.arch, row.testPlan, statusCell)
	}
	return sb.String()
}

// FormatScheduledSummary renders the scheduled build summary posted to the
// channel on the configured cron schedule. It has two sections separated by a
// blank line: builds and tests.
//
// Builds section — for each release:
//   - Heading with built count, total, weather emoji, and build %.
//   - INFRA and PRODUCT failure lines when failures exist.
//
// Tests section — for each release that has displayable test executions:
//   - Heading with weather emoji, pass rate %, and passed/total counts.
//   - Failures line listing product (arch) [failed plans] when any FAILED.
//
// Example:
//
//	### Build Summary · 2026-07-29 10:00 UTC
//
//	#### plucky (12/37) ⛈️  · 32%
//	  Infra (3): ubuntu (amd64) · ubuntu-server (amd64, arm64)
//
//	#### noble (37/37) ☀️  · 100%
//
//
//	### Test Summary · 2026-07-29 10:00 UTC
//
//	#### plucky ⛈️  · pass rate 85% (34/40)
//	  Failures (6): ubuntu (amd64) [Jenkins image validation]
//
//	#### noble ☀️  · pass rate 100% (40/40)
//
// releasesScope controls which releases to include and in which order. When nil or
// empty, all releases present in artefacts are used (sorted alphabetically).
// Artefacts are expected to be pre-filtered by the caller.
func FormatScheduledSummary(artefacts []domain.Artefact, releasesScope []string) string {
	if len(artefacts) == 0 {
		return "No snapshot available yet — the first fetch is still in progress."
	}

	// Group artefacts by release, normalising to lowercase so that snapshot
	// data from the API (e.g. "Noble") matches operator-configured scope
	// strings (e.g. "noble") regardless of capitalisation.
	byRelease := make(map[string][]domain.Artefact)
	for _, art := range artefacts {
		key := strings.ToLower(art.Release)
		byRelease[key] = append(byRelease[key], art)
	}

	// Determine release order, also normalised to lowercase.
	var ordered []string
	if len(releasesScope) > 0 {
		ordered = make([]string, len(releasesScope))
		for i, r := range releasesScope {
			ordered[i] = strings.ToLower(r)
		}
	} else {
		for r := range byRelease {
			ordered = append(ordered, r)
		}
		sort.Strings(ordered)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "### Build Summary · %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))

	any := false
	for _, release := range ordered {
		arts, ok := byRelease[release]
		if !ok {
			continue // release not in snapshot — skip silently
		}
		any = true

		total := len(arts)
		built := 0
		var infraArts, productArts []domain.Artefact

		for _, art := range arts {
			switch effectiveBuildLog(art) {
			case domain.BuildStatusBuilt:
				built++
			case domain.BuildStatusFailed:
				switch art.BuildFailureKind {
				case domain.BuildFailureKindInfra:
					infraArts = append(infraArts, art)
				case domain.BuildFailureKindProduct:
					productArts = append(productArts, art)
				}
			}
		}

		weather := buildWeatherEmoji(built, total)
		buildPct := 0
		if total > 0 {
			buildPct = built * 100 / total
		}
		fmt.Fprintf(&sb, "#### %s %s  · %d%% (%d/%d)\n",
			release, weather, buildPct, built, total)

		if len(infraArts) > 0 {
			fmt.Fprintf(&sb, "  Infra (%d): %s\n", len(infraArts), formatFailureLine(infraArts))
		}
		if len(productArts) > 0 {
			fmt.Fprintf(&sb, "  Product (%d): %s\n", len(productArts), formatFailureLine(productArts))
		}

		sb.WriteString("\n")
	}

	if !any {
		return "No data available for the configured releases."
	}

	// Append the tests section (blank line separator, then one heading per
	// release that has displayable test executions).
	testsSection := formatTestsSummarySection(byRelease, ordered)
	if testsSection != "" {
		sb.WriteString("\n")
		sb.WriteString(testsSection)
	}

	return sb.String()
}

// buildWeatherEmoji returns a Jenkins-style weather emoji based on the
// percentage of artefacts built today out of the total for a release.
//
//	100%      → ☀️  sunny
//	75–99%    → 🌤️  partly cloudy
//	50–74%    → ⛅  cloudy
//	25–49%    → 🌧️  rainy
//	1–24%     → ⛈️  stormy
//	0% (or 0) → 🌪️  tornado
func buildWeatherEmoji(built, total int) string {
	if total == 0 {
		return "🌪️"
	}
	pct := built * 100 / total
	switch {
	case pct == 100:
		return "☀️"
	case pct >= 75:
		return "🌤️"
	case pct >= 50:
		return "⛅"
	case pct >= 25:
		return "🌧️"
	case pct > 0:
		return "⛈️"
	default:
		return "🌪️"
	}
}

// formatFailureLine renders a compact one-liner for a failure bucket.
// Products are grouped by their sorted arch set; groups sharing the same
// arch set are collapsed onto one token:
//
//	ubuntu, ubuntu-mate (amd64) · ubuntu-server (amd64, arm64)
func formatFailureLine(arts []domain.Artefact) string {
	// Sub-group by product, accumulate arches.
	productOrder := []string{}
	archsByProduct := make(map[string][]string)
	for _, art := range arts {
		if _, seen := archsByProduct[art.OS]; !seen {
			productOrder = append(productOrder, art.OS)
		}
		archsByProduct[art.OS] = append(archsByProduct[art.OS], archFromName(art.Name))
	}
	sort.Strings(productOrder)

	// Collapse products that share an identical sorted arch set onto one token.
	archKey := func(archs []string) string {
		cp := append([]string(nil), archs...)
		sort.Strings(cp)
		return strings.Join(cp, ",")
	}
	type productGroup struct {
		products []string
		archs    []string
	}
	keyOrder := []string{}
	byArchKey := make(map[string]*productGroup)
	for _, prod := range productOrder {
		archs := archsByProduct[prod]
		k := archKey(archs)
		if _, seen := byArchKey[k]; !seen {
			byArchKey[k] = &productGroup{archs: archs}
			keyOrder = append(keyOrder, k)
		}
		byArchKey[k].products = append(byArchKey[k].products, prod)
	}

	tokens := make([]string, 0, len(keyOrder))
	for _, k := range keyOrder {
		g := byArchKey[k]
		sort.Strings(g.archs)
		tokens = append(tokens, fmt.Sprintf("%s (%s)",
			strings.Join(g.products, ", "),
			strings.Join(g.archs, ", ")))
	}
	return strings.Join(tokens, " · ")
}

// testFailureEntry holds the failure data for one artefact+build combination,
// used as input to formatTestsFailureLine.
type testFailureEntry struct {
	os    string
	arch  string
	plans []string // sorted list of failed test plan names
}

// formatTestsFailureLine renders a compact one-liner for test failures.
// Entries are grouped by product (OS); arches and failed plan names are
// accumulated and deduplicated across arches for each product.
//
//	ubuntu (amd64, arm64) [Jenkins image validation] · ubuntu-server (amd64) [Jenkins image validation, Manual Testing]
func formatTestsFailureLine(entries []testFailureEntry) string {
	type productGroup struct {
		archSet map[string]struct{}
		planSet map[string]struct{}
	}
	productOrder := []string{}
	byProduct := make(map[string]*productGroup)

	for _, e := range entries {
		g, seen := byProduct[e.os]
		if !seen {
			g = &productGroup{
				archSet: make(map[string]struct{}),
				planSet: make(map[string]struct{}),
			}
			byProduct[e.os] = g
			productOrder = append(productOrder, e.os)
		}
		g.archSet[e.arch] = struct{}{}
		for _, p := range e.plans {
			g.planSet[p] = struct{}{}
		}
	}
	sort.Strings(productOrder)

	tokens := make([]string, 0, len(productOrder))
	for _, prod := range productOrder {
		g := byProduct[prod]

		archs := make([]string, 0, len(g.archSet))
		for a := range g.archSet {
			archs = append(archs, a)
		}
		sort.Strings(archs)

		plans := make([]string, 0, len(g.planSet))
		for p := range g.planSet {
			plans = append(plans, p)
		}
		sort.Strings(plans)

		tokens = append(tokens, fmt.Sprintf("%s (%s) [%s]",
			prod,
			strings.Join(archs, ", "),
			strings.Join(plans, ", ")))
	}
	return strings.Join(tokens, " · ")
}

// formatTestsSummarySection renders the tests half of the scheduled summary.
// It iterates releases in the given order, computes pass/fail counts from
// deduplicated displayable test executions, and emits one heading per release
// that has at least one such execution. Releases with no displayable executions
// are omitted entirely.
func formatTestsSummarySection(
	byRelease map[string][]domain.Artefact,
	ordered []string,
) string {
	var sb strings.Builder
	headerWritten := false
	for _, release := range ordered {
		arts, ok := byRelease[release]
		if !ok {
			continue
		}

		passed, failed, total := 0, 0, 0
		// failMap collects test failure entries keyed by artefactID+buildID
		// to avoid double-counting.
		type artBuildKey struct{ artID, buildID int }
		failMap := make(map[artBuildKey]*testFailureEntry)

		for _, art := range arts {
			for _, b := range art.Builds {
				for _, te := range dedupLatestPerPlan(b.TestExecutions) {
					total++
					switch te.Status {
					case "PASSED":
						passed++
					case "FAILED":
						failed++
						key := artBuildKey{art.ID, b.ID}
						e, seen := failMap[key]
						if !seen {
							e = &testFailureEntry{
								os:   art.OS,
								arch: b.Architecture,
							}
							failMap[key] = e
						}
						e.plans = append(e.plans, te.TestPlan)
					}
				}
			}
		}

		if total == 0 {
			continue // no displayable executions — omit this release
		}

		if !headerWritten {
			fmt.Fprintf(&sb, "### Test Summary · %s\n\n",
				time.Now().UTC().Format("2006-01-02 15:04 UTC"))
			headerWritten = true
		}

		pct := passed * 100 / total
		weather := buildWeatherEmoji(passed, total)
		fmt.Fprintf(&sb, "#### %s %s  · %d%% (%d/%d)\n",
			release, weather, pct, passed, total)

		if failed > 0 {
			entries := make([]testFailureEntry, 0, len(failMap))
			for _, e := range failMap {
				sort.Strings(e.plans)
				entries = append(entries, *e)
			}
			// Sort entries for deterministic output.
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].os != entries[j].os {
					return entries[i].os < entries[j].os
				}
				return entries[i].arch < entries[j].arch
			})
			fmt.Fprintf(&sb, "  Failures (%d): %s\n",
				failed, formatTestsFailureLine(entries))
		}

		sb.WriteString("\n")
	}
	return sb.String()
}

// archFromName extracts the CPU architecture token from an artefact name.
// It scans the name for known arch strings and returns the first match.
// Returns "unknown" when no known arch is found.
func archFromName(name string) string {
	// Order matters: longer/more-specific tokens before shorter ones
	// (e.g. "riscv64" before any hypothetical "risc").
	for _, arch := range []string{"amd64", "arm64", "riscv64", "ppc64el", "s390x", "armhf", "i386"} {
		if strings.Contains(name, arch) {
			return arch
		}
	}
	return "unknown"
}

// effectiveBuildLog returns the effective BuildStatusState for an artefact.
// When a BuildLog state has been set (via EnrichBuildStatus), it is returned directly.
// Otherwise the function falls back to version-date logic: today's version = BUILT,
// any other date = NOT_STARTED (conservative fallback; no log data available).
func effectiveBuildLog(art domain.Artefact) domain.BuildStatusState {
	if art.BuildLog != "" {
		return art.BuildLog
	}
	if domain.IsBuiltToday(art.Version) {
		return domain.BuildStatusBuilt
	}
	return domain.BuildStatusNotStarted
}

// artefactStatusCell returns the display cell for an artefact's build status.
// For failed builds it appends a kind label and, when available, a short
// description to distinguish the failure cause:
//   - "❌ INFRA: cdimage crashed before submitting builds to Launchpad"
//   - "❌ PRODUCT: livefs build failure requires analysis"
//   - "❌ INFRA"   — kind known but no description set
//   - "❌"         — failed but kind not yet classified
func artefactStatusCell(art domain.Artefact) string {
	status := effectiveBuildLog(art)
	icon := domain.BuildLogIcon(status)
	if status == domain.BuildStatusFailed && art.BuildFailureKind != "" {
		if art.BuildFailureDescription != "" {
			return icon + " " + string(art.BuildFailureKind) + ": " + art.BuildFailureDescription
		}
		return icon + " " + string(art.BuildFailureKind)
	}
	return icon
}
func FormatChangeReport(r domain.ChangeReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Change Report** · %s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))

	if len(r.NewFailures) > 0 {
		sb.WriteString("🔴 **New Failures**\n\n")
		sb.WriteString("| Release | Artefact | Previous Status |\n")
		sb.WriteString("|---------|----------|-----------------|\n")
		for _, f := range r.NewFailures {
			fmt.Fprintf(&sb, "| %s | %s | %s |\n", f.Release, f.Name, f.OldStatus)
		}
		sb.WriteString("\n")
	}

	if len(r.Recoveries) > 0 {
		sb.WriteString("🟢 **Recoveries**\n\n")
		sb.WriteString("| Release | Artefact |\n")
		sb.WriteString("|---------|----------|\n")
		for _, rec := range r.Recoveries {
			fmt.Fprintf(&sb, "| %s | %s |\n", rec.Release, rec.Name)
		}
		sb.WriteString("\n")
	}

	if len(r.OtherChanges) > 0 {
		sb.WriteString("🔵 **Other Changes**\n\n")
		sb.WriteString("| Release | Artefact | Old Status | New Status |\n")
		sb.WriteString("|---------|----------|------------|------------|\n")
		for _, o := range r.OtherChanges {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", o.Release, o.Name, o.OldStatus, o.NewStatus)
		}
		sb.WriteString("\n")
	}

	if len(r.NewArtefacts) > 0 {
		sb.WriteString("🆕 **New Artefacts**\n\n")
		sb.WriteString("| Release | Product | Artefact | Version | Age | Build | Log |\n")
		sb.WriteString("|---------|---------|----------|---------|-----|-------|-----|\n")
		for _, n := range r.NewArtefacts {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s | %s |\n",
				n.Release, n.OS, n.Name, n.Version, domain.ImageAge(n.Version), artefactStatusCell(n), domain.LogCell(n.ImageURL))
		}
	}

	return sb.String()
}

// FormatNewBuildsNotification renders one line per newly successful build,
// posting each as a standalone sentence with a link to the Test Observer page.
// Returns an empty string when the builds slice is empty.
func FormatNewBuildsNotification(builds []domain.Artefact) string {
	if len(builds) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, a := range builds {
		url := domain.TestObserverArtefactURL(a.ID)
		fmt.Fprintf(&sb, "- New %s build available for %s serial %s [🔗](%s)\n", a.Release, a.Name, a.Version, url)
	}
	return sb.String()
}

// FormatInvestigation renders the LLM log analysis result for a single artefact.
// source is a human-readable description of which log was analysed
// (e.g. "Launchpad librarian (amd64)" or "cd-build-log").
func FormatInvestigation(art domain.Artefact, analysis domain.LogAnalysis, source string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Investigation — %s** (ID: %d) · %s\n\n",
		art.Name, art.ID, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&sb, "**Log source:** %s\n", source)
	fmt.Fprintf(&sb, "**Category:** %s\n", analysis.Category)
	fmt.Fprintf(&sb, "**Hypothesis:** %s\n", analysis.Hypothesis)
	if len(analysis.LogExcerpts) > 0 {
		sb.WriteString("\n**Relevant log excerpts:**\n")
		for _, line := range analysis.LogExcerpts {
			fmt.Fprintf(&sb, "- `%s`\n", line)
		}
	}
	fmt.Fprintf(&sb, "\n**Recommended action:** %s\n", analysis.NextAction)
	return sb.String()
}

// GreetText returns a short, friendly greeting shown when the bot is mentioned
// with no command. It summarises capabilities and invites natural-language use.
func GreetText() string {
	return `Hey! I'm Watchtower, your Ubuntu image build pipeline monitor.

Here's what I can help with:
- **Build status** — are today's images built yet?
- **Test status** — how are the test runs looking?
- **Failures** — which builds are failing and why?
- **Investigate** — deep-dive into a specific build log with AI analysis

Just ask me in plain English — _how is noble doing today?_, _what's failing for desktop?_ — or type ` + "`help`" + ` for the full command reference.`
}

// HelpText returns the Markdown help message listing all available commands.
func HelpText() string {
	return `**Watchtower — available commands:**

| Command | Description |
|---------|-------------|
| ` + "`summary`" + `                                        | Scheduled build summary (same as the automatic post) |
| ` + "`builds status`" + `                                 | Build summary for all releases with progress bar |
| ` + "`builds status <release>`" + `                       | Detailed build status for a specific release (includes artefact IDs) |
| ` + "`builds status <release> <product>`" + `             | Filter detail view to a single product |
| ` + "`tests status`" + `                                  | Test summary for all releases with progress bar |
| ` + "`tests status <release>`" + `                        | Detailed test status for a specific release |
| ` + "`tests status <release> <product>`" + `              | Filter test detail view to a single product |
| ` + "`failures`" + `                                      | Active failures across all releases |
| ` + "`failures <release>`" + `                            | Active failures for a specific release |
| ` + "`failures <release> <product>`" + `                  | Active failures for a release and product |
| ` + "`failure detail <artefact-id>`" + `                  | Full detail for one failure including LLM analysis |
| ` + "`analyse failures`" + `                              | Trigger LLM log analysis on pending failures (background) |
| ` + "`analyse failures <release>`" + `                    | Trigger analysis scoped to a release |
| ` + "`investigate <artefact-id>`" + `                     | Fetch build log and run LLM root-cause analysis |
| ` + "`help`" + `                                          | Show this message |

The scheduled build summary is posted automatically per SUMMARY_CRON_SCHEDULE.
Failure analysis runs automatically per FAILURE_ANALYSIS_CRON_SCHEDULE (default every 8 h).`
}

// FormatFailuresSummary renders a compact list of active (unresolved) failures.
// Records are grouped by release then product. Within each release/product group,
// records sharing the same FailureSignature are collapsed into a single block
// showing all affected artefacts — this surfaces cross-artefact patterns like
// "apt:missing:libfoo-dev affects 5 stonking builds". Records with no signature
// fall through to the existing per-record format.
//
// release and product are used only in the header when non-empty; the caller is
// responsible for pre-filtering records.
func FormatFailuresSummary(records []domain.FailureRecord, release, product string) string {
	if len(records) == 0 {
		switch {
		case release != "" && product != "":
			return fmt.Sprintf("No active failures for **%s** / **%s**.", release, product)
		case release != "":
			return fmt.Sprintf("No active failures for **%s**.", release)
		default:
			return "No active failures detected."
		}
	}

	var sb strings.Builder
	title := "**Active Failures**"
	if release != "" && product != "" {
		title = fmt.Sprintf("**Active Failures · %s / %s**", release, product)
	} else if release != "" {
		title = fmt.Sprintf("**Active Failures · %s**", release)
	}
	fmt.Fprintf(&sb, "%s · %s\n\n", title, time.Now().UTC().Format("2006-01-02 15:04 UTC"))

	// Split records into INFRA and non-INFRA (PRODUCT/UNKNOWN/unset).
	var infraRecs, productRecs []domain.FailureRecord
	for _, r := range records {
		if r.FailureKind == domain.BuildFailureKindInfra {
			infraRecs = append(infraRecs, r)
		} else {
			productRecs = append(productRecs, r)
		}
	}

	// --- INFRA section ---
	// Group by release, then by signature, collapsing artefacts
	// across products into a single block per root cause.
	if len(infraRecs) > 0 {
		sb.WriteString(formatInfraFailures(infraRecs))
	}

	// --- PRODUCT / UNKNOWN section ---
	// Existing per-(release, product) grouping with per-product
	// GroupBySignature sub-grouping.
	if len(productRecs) > 0 {
		sb.WriteString(formatProductFailures(productRecs))
	}

	return sb.String()
}

// formatInfraFailures renders INFRA records grouped by (release, signature)
// across all products, so a single infrastructure outage that hits many
// artefacts appears as one collapsed block rather than per-product lines.
func formatInfraFailures(records []domain.FailureRecord) string {
	// Collect distinct releases in sorted order.
	releaseSet := make(map[string]struct{})
	for _, r := range records {
		releaseSet[r.Release] = struct{}{}
	}
	releases := make([]string, 0, len(releaseSet))
	for rel := range releaseSet {
		releases = append(releases, rel)
	}
	sort.Strings(releases)

	var sb strings.Builder
	for _, rel := range releases {
		// Collect all INFRA records for this release.
		var relRecs []domain.FailureRecord
		for _, r := range records {
			if r.Release == rel {
				relRecs = append(relRecs, r)
			}
		}

		// Group by signature; unsig'd records go under "".
		bySig := domain.GroupBySignature(relRecs)
		sigs := make([]string, 0, len(bySig))
		for sig := range bySig {
			sigs = append(sigs, sig)
		}
		sort.Strings(sigs)

		fmt.Fprintf(&sb, "**Infra · %s** — %d failing\n", rel, len(relRecs))
		for _, sig := range sigs {
			group := bySig[sig]
			if sig == "" {
				// No signature - render individually.
				for _, r := range group {
					sb.WriteString(formatFailureRecord(r))
				}
				continue
			}
			if len(group) == 1 {
				r := group[0]
				occStr := ""
				if r.Occurrences > 1 {
					occStr = fmt.Sprintf(" (%d× recurring)", r.Occurrences)
				}
				fmt.Fprintf(&sb, " - **%s**%s — `%s`\n",
					r.ArtefactName, occStr, sig)
				continue
			}
			// Multiple artefacts - group by product for compactness.
			fmt.Fprintf(&sb, " - `%s` — %d artefacts affected\n",
				sig, len(group))
			sb.WriteString(formatInfraArtefactsByProduct(group))
			// Emit the human-readable description once, from the first
			// record (all records in the group share the same description).
			if group[0].FailureDescription != "" {
				fmt.Fprintf(&sb, "   _%s_\n",
					group[0].FailureDescription)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatInfraArtefactsByProduct compacts a group of INFRA records that share
// a signature into per-product lines of the form:
//
//   - ubuntu-server (amd64, arm64, s390x)
func formatInfraArtefactsByProduct(
	group []domain.FailureRecord,
) string {
	// Collect products in order.
	productOrder := []string{}
	byProduct := make(map[string][]domain.FailureRecord)
	for _, r := range group {
		if _, seen := byProduct[r.Product]; !seen {
			productOrder = append(productOrder, r.Product)
		}
		byProduct[r.Product] = append(byProduct[r.Product], r)
	}
	sort.Strings(productOrder)

	var sb strings.Builder
	for _, prod := range productOrder {
		recs := byProduct[prod]
		// Extract arch suffix from artefact name for brevity.
		// Names are like "stonking-base-amd64.tar.gz" - arch is
		// the component before the first ".".
		archs := make([]string, 0, len(recs))
		for _, r := range recs {
			archs = append(archs, archVariantFromName(r.ArtefactName))
		}
		sort.Strings(archs)
		occMax := 0
		for _, r := range recs {
			if r.Occurrences > occMax {
				occMax = r.Occurrences
			}
		}
		occStr := ""
		if occMax > 1 {
			occStr = fmt.Sprintf(" (%d× recurring)", occMax)
		}
		fmt.Fprintf(&sb, "   - %s (%s)%s\n",
			prod, strings.Join(archs, ", "), occStr)
	}
	return sb.String()
}

// archVariantFromName extracts the full arch+variant token from an artefact
// filename, preserving sub-variants like "arm64+largemem".
// e.g. "stonking-base-amd64.tar.gz"        -> "amd64"
//
//	"noble-live-server-arm64+largemem.iso" -> "arm64+largemem"
//
// Falls back to the full name if the pattern is not recognised.
func archVariantFromName(name string) string {
	// Strip any extension clusters (.tar.gz, .img.xz, .iso, …).
	base := name
	for {
		ext := filepath.Ext(base)
		if ext == "" {
			break
		}
		base = base[:len(base)-len(ext)]
	}
	// The last dash-separated token is the arch (may include +variant).
	parts := strings.Split(base, "-")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return name
}

// formatProductFailures renders PRODUCT/UNKNOWN/unset records using the
// original per-(release, product) grouping with GroupBySignature within
// each bucket.
func formatProductFailures(records []domain.FailureRecord) string {
	type key struct{ release, product string }
	order := []key{}
	byKey := make(map[key][]domain.FailureRecord)
	for _, r := range records {
		k := key{r.Release, r.Product}
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], r)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].release != order[j].release {
			return order[i].release < order[j].release
		}
		return order[i].product < order[j].product
	})

	var sb strings.Builder
	for _, k := range order {
		recs := byKey[k]
		fmt.Fprintf(&sb, "**%s / %s** — %d failing\n",
			k.release, k.product, len(recs))

		bySig := domain.GroupBySignature(recs)
		sigs := make([]string, 0, len(bySig))
		for sig := range bySig {
			sigs = append(sigs, sig)
		}
		sort.Strings(sigs)

		for _, sig := range sigs {
			group := bySig[sig]
			if sig == "" {
				for _, r := range group {
					sb.WriteString(formatFailureRecord(r))
				}
				continue
			}
			if len(group) == 1 {
				r := group[0]
				occStr := ""
				if r.Occurrences > 1 {
					occStr = fmt.Sprintf(" (%d× recurring)", r.Occurrences)
				}
				fmt.Fprintf(&sb, " - **%s**%s — `%s`", r.ArtefactName, occStr, sig)
				if r.Analysis != nil && r.Analysis.NextAction != "" {
					fmt.Fprintf(&sb, " → %s", r.Analysis.NextAction)
				}
				sb.WriteString("\n")
				continue
			}
			// Multiple artefacts share the same root cause.
			var hypothesis, nextAction string
			for _, r := range group {
				if r.Analysis != nil {
					if hypothesis == "" {
						hypothesis = r.Analysis.Hypothesis
					}
					if nextAction == "" {
						nextAction = r.Analysis.NextAction
					}
				}
			}
			fmt.Fprintf(&sb, " - `%s` — %d artefacts affected\n",
				sig, len(group))
			for _, r := range group {
				occStr := ""
				if r.Occurrences > 1 {
					occStr = fmt.Sprintf(" (%d×)", r.Occurrences)
				}
				fmt.Fprintf(&sb, "   - %s%s\n", r.ArtefactName, occStr)
			}
			if hypothesis != "" {
				fmt.Fprintf(&sb, "   Hypothesis: %s\n", hypothesis)
			}
			if nextAction != "" {
				fmt.Fprintf(&sb, "   Next action: %s\n", nextAction)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// formatFailureRecord renders a single FailureRecord line for use in the
// no-signature path of FormatFailuresSummary.
func formatFailureRecord(r domain.FailureRecord) string {
	occStr := ""
	if r.Occurrences > 1 {
		occStr = fmt.Sprintf(" (%d× recurring)", r.Occurrences)
	}
	var analysisStr string
	switch {
	case r.Analysis != nil:
		analysisStr = fmt.Sprintf(" — %s: %s", r.Analysis.Category, r.Analysis.Hypothesis)
	case r.FailureKind != "" && r.FailureDescription != "":
		analysisStr = fmt.Sprintf(" — %s: %s", r.FailureKind, r.FailureDescription)
	case r.FailureKind != "":
		analysisStr = fmt.Sprintf(" — %s", r.FailureKind)
	default:
		analysisStr = " _(analysis pending)_"
	}
	return fmt.Sprintf(" - **%s**%s%s\n", r.ArtefactName, occStr, analysisStr)
}

// FormatFailureDetail renders the full detail for a single FailureRecord,
// including its LLM analysis if available.
func FormatFailureDetail(r domain.FailureRecord) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Failure Detail — %s** · %s / %s\n\n", r.ArtefactName, r.Release, r.Product)
	fmt.Fprintf(&sb, "**First seen:** %s · **Last seen:** %s · **Occurrences:** %d\n",
		r.FirstSeenVersion, r.LastSeenVersion, r.Occurrences)

	if r.FailureKind != "" {
		if r.FailureDescription != "" {
			fmt.Fprintf(&sb, "**Kind:** %s — %s\n", r.FailureKind, r.FailureDescription)
		} else {
			fmt.Fprintf(&sb, "**Kind:** %s\n", r.FailureKind)
		}
	}

	if r.Analysis == nil {
		sb.WriteString("\n_Analysis not yet available. Run `analyse failures` to trigger LLM analysis._\n")
		return sb.String()
	}

	fmt.Fprintf(&sb, "**Analysed version:** %s", r.AnalysedVersion)
	if r.AnalysedAt != nil {
		fmt.Fprintf(&sb, " · **Analysed at:** %s", r.AnalysedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	sb.WriteString("\n\n")
	fmt.Fprintf(&sb, "**Category:** %s\n", r.Analysis.Category)
	fmt.Fprintf(&sb, "**Hypothesis:** %s\n", r.Analysis.Hypothesis)
	if len(r.Analysis.LogExcerpts) > 0 {
		sb.WriteString("\n**Relevant log excerpts:**\n")
		for _, line := range r.Analysis.LogExcerpts {
			fmt.Fprintf(&sb, "- `%s`\n", line)
		}
	}
	fmt.Fprintf(&sb, "\n**Recommended action:** %s\n", r.Analysis.NextAction)
	return sb.String()
}
