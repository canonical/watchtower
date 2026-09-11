// Package domain contains the core types and pure business logic for Watchtower.
// It has zero dependencies on other internal packages; only stdlib is imported.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const baseLogURL = "https://ubuntu-archive-team.ubuntu.com/cd-build-logs"

// ErrLogNotFound is returned by a LogFetcher when the log URL responds with HTTP 404.
// Callers use errors.Is(err, ErrLogNotFound) to distinguish "not yet available"
// from a genuine network or server error.
var ErrLogNotFound = errors.New("log not found")

// BuildStatusState represents the enriched build status derived from the cd-build-log.
// It is computed at activity time (not from the Test Observer API) and persisted in
// the snapshot so that the bot can answer status queries without refetching logs.
type BuildStatusState string

const (
	// BuildStatusBuilt means an artefact with today's serial exists in Test Observer.
	BuildStatusBuilt BuildStatusState = "BUILT"
	// BuildStatusNotStarted means today's log is unavailable or has no "starting at"
	// line for this artefact's architecture — the build has not been triggered yet.
	BuildStatusNotStarted BuildStatusState = "NOT_STARTED"
	// BuildStatusInProgress means a "starting at" line exists for this arch but no
	// corresponding "finished at" line — the build is currently running.
	BuildStatusInProgress BuildStatusState = "IN_PROGRESS"
	// BuildStatusFailed means a "finished at" line exists without "(Successfully built)",
	// or an infra error was detected before the build could finish.
	BuildStatusFailed BuildStatusState = "FAILED"
	// BuildStatusUnknown means the log fetch failed for a reason other than 404
	// (e.g. network error, unexpected response), so status cannot be determined.
	BuildStatusUnknown BuildStatusState = "UNKNOWN"
	// BuildStatusApproved means no build ran today (no log / 404) but the
	// artefact's Test Observer status is APPROVED — the image is in a healthy,
	// finalised state. Typically seen for stable releases after release day when
	// daily builds have stopped.
	BuildStatusApproved BuildStatusState = "APPROVED"
	// BuildStatusMarkedFailed means no build ran today but the artefact's Test
	// Observer status is MARKED_AS_FAILED — QA has explicitly flagged the image
	// as failed. The build pipeline is quiescent but the image is not approved.
	BuildStatusMarkedFailed BuildStatusState = "MARKED_AS_FAILED"
)

// BuildFailureKind classifies the root cause of a build failure into two high-level
// categories that reflect where in the two-phase build pipeline the failure occurred.
//
// Phase 1 — cdimage orchestration: the cd-build-log shows a Python traceback or
// orphaned arches (started but never finished due to a cdimage crash). These are
// infrastructure issues: disk full on the cdimage host, Launchpad API errors, etc.
//
// Phase 2 — Launchpad build: the build was submitted to Launchpad but the LP builder
// itself reported failure ("Failed to build"). These are typically product issues:
// dependency conflicts, debootstrap errors, snap failures, etc.
//
// Exception: "(Chroot problem)" is reported by Launchpad but indicates an LP builder
// infrastructure problem, not a product defect — it is classified as INFRA.
type BuildFailureKind string

const (
	// BuildFailureKindNone means the artefact did not fail (not applicable).
	BuildFailureKindNone BuildFailureKind = ""
	// BuildFailureKindInfra means the failure occurred in Phase 1 (cdimage crashed
	// before or during Launchpad submission) or is a Launchpad builder infra problem
	// (Chroot problem). Owner: infrastructure / cdimage team.
	BuildFailureKindInfra BuildFailureKind = "INFRA"
	// BuildFailureKindProduct means the failure occurred in Phase 2: Launchpad
	// accepted the build but the LP builder reported "Failed to build". Owner: product
	// team / archive (dependency conflicts, debootstrap failures, snap errors, etc.).
	BuildFailureKindProduct BuildFailureKind = "PRODUCT"
	// BuildFailureKindUnknown means the build failed but the cause cannot be
	// categorised from the available log content.
	BuildFailureKindUnknown BuildFailureKind = "UNKNOWN"
)

// BuildLogIcon returns the display emoji for a BuildStatusState.
func BuildLogIcon(s BuildStatusState) string {
	switch s {
	case BuildStatusBuilt:
		return "✅"
	case BuildStatusNotStarted:
		return "⏳"
	case BuildStatusInProgress:
		return "🔄"
	case BuildStatusFailed:
		return "❌"
	case BuildStatusApproved:
		return "✔️"
	case BuildStatusMarkedFailed:
		return "🚫"
	default:
		return "❓"
	}
}

// Artefact mirrors the Test Observer API ArtefactResponse for the image family.
// Only fields used by Watchtower are included; extra API fields are silently discarded.
// Builds is populated by the cron workflow and cached in the snapshot; it is
// omitted from JSON when empty so existing snapshot files remain compatible.
// BuildLog is computed from the cd-build-log at fetch time and persisted in the snapshot.
type Artefact struct {
	ID                      int              `json:"id"`
	Name                    string           `json:"name"`
	Version                 string           `json:"version"` // YYYYMMDD or YYYYMMDD.N (respin); today's date means build succeeded and image is available for testing
	OS                      string           `json:"os"`
	Release                 string           `json:"release"`
	Stage                   string           `json:"stage"`  // pending | current — pipeline release stage, not build state
	Status                  string           `json:"status"` // APPROVED | MARKED_AS_FAILED | UNDECIDED — test review state, unrelated to build availability
	Archived                bool             `json:"archived"`
	ImageURL                string           `json:"image_url"`
	BuildLog                BuildStatusState `json:"build_log,omitempty"`                 // enriched build status derived from today's cd-build-log
	BuildFailureKind        BuildFailureKind `json:"build_failure_kind,omitempty"`        // failure category when BuildLog==FAILED: INFRA or PRODUCT
	BuildFailureDescription string           `json:"build_failure_description,omitempty"` // short human-readable reason for the failure kind
	Builds                  []ArtefactBuild  `json:"builds,omitempty"`                    // cached from /v1/artefacts/{id}/builds
}

// ArtefactBuild represents one architecture-specific build of an artefact,
// with its associated test executions.
type ArtefactBuild struct {
	ID             int             `json:"id"`
	Architecture   string          `json:"architecture"`
	Revision       *int            `json:"revision,omitempty"`
	TestExecutions []TestExecution `json:"test_executions"`
}

// TestExecution represents a single test run within an ArtefactBuild.
type TestExecution struct {
	ID          int         `json:"id"`
	CILink      string      `json:"ci_link"`
	Status      string      `json:"status"`
	TestPlan    string      `json:"test_plan"`
	Environment Environment `json:"environment"`
	CreatedAt   string      `json:"created_at"`
}

// Environment describes the machine or runner that executed the tests.
type Environment struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Architecture string `json:"architecture"`
}

// ChangeReport categorises status changes between two snapshots.
type ChangeReport struct {
	NewFailures  []ArtefactDelta `json:"new_failures"`
	Recoveries   []ArtefactDelta `json:"recoveries"`
	OtherChanges []ArtefactDelta `json:"other_changes"`
	NewArtefacts []Artefact      `json:"new_artefacts"`
	// NewBuilds contains artefacts whose version serial advanced since the last
	// snapshot and whose BuildLog is now BUILT, confirming a successful build.
	// First-boot artefacts (no prior version) are excluded to avoid bulk noise.
	NewBuilds []Artefact `json:"new_builds,omitempty"`
}

// ArtefactDelta represents one artefact's status transition between snapshots.
type ArtefactDelta struct {
	ArtefactID int    `json:"artefact_id"` // set when known; enables exact lookup avoiding name collisions
	Name       string `json:"name"`
	Release    string `json:"release"`
	Version    string `json:"version"`
	OldStatus  string `json:"old_status"`
	NewStatus  string `json:"new_status"`
}

// LogAnalysis is the structured output from an LLM log analysis.
type LogAnalysis struct {
	Category    string   `json:"category"` // infra|code|dependency|flaky|unknown
	Hypothesis  string   `json:"hypothesis"`
	Signature   string   `json:"signature,omitempty"` // canonical slug e.g. "apt:missing:libfoo-dev"
	LogExcerpts []string `json:"log_excerpts"`
	NextAction  string   `json:"next_action"`
}

// FailureRecord tracks a single failing artefact across one or more build cycles.
// Records are keyed by ArtefactID in a FailureStore and are never deleted — instead
// they are marked Resolved when the artefact recovers. This preserves history and
// enables occurrence weighting across builds.
type FailureRecord struct {
	ArtefactID         int              `json:"artefact_id"`
	ArtefactName       string           `json:"artefact_name"`
	Release            string           `json:"release"`
	Product            string           `json:"product"`                       // maps to Artefact.OS
	FirstSeenVersion   string           `json:"first_seen_version"`            // YYYYMMDD of first failure
	LastSeenVersion    string           `json:"last_seen_version"`             // YYYYMMDD of most recent failure
	Occurrences        int              `json:"occurrences"`                   // increments each new failing version
	FailureKind        BuildFailureKind `json:"failure_kind,omitempty"`        // INFRA | PRODUCT — deterministic classification from log parsing
	FailureDescription string           `json:"failure_description,omitempty"` // short human-readable reason
	FailureSignature   string           `json:"failure_signature,omitempty"`   // canonical slug from ExtractFailureSignature or LLM; enables cross-artefact grouping
	LivefsLogURL       string           `json:"livefs_log_url,omitempty"`      // Launchpad librarian URL resolved via two-hop; non-empty only for PRODUCT failures
	Analysis           *LogAnalysis     `json:"analysis,omitempty"`            // nil until FailureAnalysisWorkflow runs
	AnalysedVersion    string           `json:"analysed_version,omitempty"`    // version the analysis was done on
	AnalysedAt         *time.Time       `json:"analysed_at,omitempty"`
	Resolved           bool             `json:"resolved"` // true once a recovery is detected
}

// FailureStore is the top-level structure persisted to failures.json.
// Outer key: release name. Inner key: product name (Artefact.OS).
// Value: slice of FailureRecords for that release+product combination.
type FailureStore map[string]map[string][]FailureRecord

// UpsertFailure updates the FailureStore with a new or recurring failure for
// the given artefact. If an unresolved record already exists for this artefact ID,
// its occurrence count is incremented and LastSeenVersion is updated. The Analysis
// field is cleared only if the new version differs from the analysed version
// (indicating the failure may have a new root cause). If no record exists, a new
// one is created with Analysis=nil (pending analysis).
// Returns true if the record is newly created (first time this artefact fails).
func (fs FailureStore) UpsertFailure(art Artefact) bool {
	if fs[art.Release] == nil {
		fs[art.Release] = make(map[string][]FailureRecord)
	}
	records := fs[art.Release][art.OS]

	for i := range records {
		if records[i].ArtefactID == art.ID && !records[i].Resolved {
			records[i].LastSeenVersion = art.Version
			records[i].Occurrences++
			// Always refresh the deterministic classification fields — they may
			// change if the failure kind or description changes across versions.
			records[i].FailureKind = art.BuildFailureKind
			records[i].FailureDescription = art.BuildFailureDescription
			if art.BuildFailureKind == BuildFailureKindInfra {
				records[i].FailureSignature = InfraSignatureFor(
					art.BuildFailureDescription,
				)
			}
			// If the version changed since the last analysis, the old analysis
			// may no longer be accurate — clear it so re-analysis is triggered.
			if records[i].Analysis != nil && records[i].AnalysedVersion != art.Version {
				records[i].Analysis = nil
				records[i].AnalysedAt = nil
				records[i].AnalysedVersion = ""
			}
			fs[art.Release][art.OS] = records
			return false // existing record updated
		}
	}

	// New failure record.
	newRec := FailureRecord{
		ArtefactID:         art.ID,
		ArtefactName:       art.Name,
		Release:            art.Release,
		Product:            art.OS,
		FirstSeenVersion:   art.Version,
		LastSeenVersion:    art.Version,
		Occurrences:        1,
		FailureKind:        art.BuildFailureKind,
		FailureDescription: art.BuildFailureDescription,
	}
	if art.BuildFailureKind == BuildFailureKindInfra {
		newRec.FailureSignature = InfraSignatureFor(art.BuildFailureDescription)
	}
	fs[art.Release][art.OS] = append(records, newRec)
	return true
}

// ResolveFailure marks all unresolved FailureRecords for the given artefact ID
// as resolved. This is called when a recovery is detected in the diff.
func (fs FailureStore) ResolveFailure(artefactID int, release, product string) {
	records := fs[release][product]
	for i := range records {
		if records[i].ArtefactID == artefactID && !records[i].Resolved {
			records[i].Resolved = true
		}
	}
	if fs[release] != nil {
		fs[release][product] = records
	}
}

// ActiveFailures returns all unresolved FailureRecords across the store, optionally
// filtered by release and/or product. Pass empty strings to include all.
func (fs FailureStore) ActiveFailures(release, product string) []FailureRecord {
	var out []FailureRecord
	for rel, byProduct := range fs {
		if release != "" && rel != release {
			continue
		}
		for prod, records := range byProduct {
			if product != "" && prod != product {
				continue
			}
			for _, r := range records {
				if !r.Resolved {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

// PendingAnalysis returns all unresolved FailureRecords that need LLM analysis,
// capped at max records. Pass max=0 for no cap.
//
// INFRA records are skipped: they already carry a deterministic description
// from cd-build-log parsing (e.g. "disk full", "LP API error") that is more
// precise than anything the LLM can produce from the same log. Sending them
// to the LLM wastes tokens without improving the user experience.
// UNKNOWN records are included alongside PRODUCT ones — the UNKNOWN kind is
// reserved for future classification and may benefit from LLM analysis.
func (fs FailureStore) PendingAnalysis(max int) []FailureRecord {
	var out []FailureRecord
	for _, byProduct := range fs {
		for _, records := range byProduct {
			for _, r := range records {
				if r.Resolved || r.Analysis != nil {
					continue
				}
				if r.FailureKind == BuildFailureKindInfra {
					continue // deterministic description already set; skip
				}
				out = append(out, r)
				if max > 0 && len(out) >= max {
					return out
				}
			}
		}
	}
	return out
}

// SetAnalysis stores a completed LogAnalysis on the record matching artefactID
// in the given release+product bucket. The Signature field is also promoted to
// FailureSignature on the record to enable cross-artefact grouping queries
// without having to dereference Analysis.
//
// reclassify may be non-empty to override the record's FailureKind. This is
// used when log resolution reveals that a previously-classified PRODUCT failure
// is actually an infrastructure failure — for example when Launchpad ran the
// build but produced no log (ErrNoLPLog).
//
// livefLogURL is the Launchpad librarian URL resolved during two-hop log
// resolution. It is stored on the record so that the builds status formatter
// can link directly to the livefs build log for PRODUCT failures instead of
// the first-hop cd-build-log. Pass "" when not applicable (INFRA failures or
// when only the cd-build-log was available).
func (fs FailureStore) SetAnalysis(
	artefactID int,
	release, product string,
	analysis LogAnalysis,
	version string,
	reclassify BuildFailureKind,
	livefLogURL string,
) {
	records := fs[release][product]
	now := time.Now().UTC()
	for i := range records {
		if records[i].ArtefactID == artefactID && !records[i].Resolved {
			records[i].Analysis = &analysis
			records[i].AnalysedVersion = version
			records[i].AnalysedAt = &now
			if analysis.Signature != "" {
				records[i].FailureSignature = analysis.Signature
			}
			if reclassify != "" {
				records[i].FailureKind = reclassify
				// Also clear any stale deterministic description — the new
				// kind supersedes what ParseBuildStatusFromLog set.
				records[i].FailureDescription = analysis.Hypothesis
			}
			if livefLogURL != "" {
				records[i].LivefsLogURL = livefLogURL
			}
		}
	}
	if fs[release] != nil {
		fs[release][product] = records
	}
}

// FindActive returns a pointer to the first unresolved FailureRecord for the
// given artefact ID within the release+product bucket, or nil if no such record
// exists. The returned pointer is into the live slice — callers must not mutate
// it; use SetAnalysis or UpsertFailure for modifications.
func (fs FailureStore) FindActive(
	artefactID int, release, product string,
) *FailureRecord {
	records := fs[release][product]
	for i := range records {
		if records[i].ArtefactID == artefactID && !records[i].Resolved {
			return &records[i]
		}
	}
	return nil
}

// IsBuiltToday returns true if the version's base date (YYYYMMDD) matches today in UTC.
func IsBuiltToday(version string) bool {
	base := version
	if i := strings.IndexByte(version, '.'); i != -1 {
		base = version[:i]
	}
	return base == time.Now().UTC().Format("20060102")
}

// BuildStatus returns an emoji reflecting whether the image was built today.
// ✅ means built today; ❌ means not built. Log URL is handled separately via LogURLFromImageURL.
func BuildStatus(version string) string {
	if IsBuiltToday(version) {
		return "✅"
	}
	return "❌"
}

// testObserverWebURL is the base URL of the Test Observer web UI.
const testObserverWebURL = "https://tests.ubuntu.com"

// TestObserverArtefactURL returns the Test Observer web UI URL for an artefact.
// Format: https://tests.ubuntu.com/#/images/<id>
func TestObserverArtefactURL(artefactID int) string {
	return fmt.Sprintf("%s/#/images/%d", testObserverWebURL, artefactID)
}

// LogCell returns a Markdown 🔗 hyperlink to today's build log when imageURL is a
// recognised cdimage.ubuntu.com URL, or ❌ when no log URL can be derived.
// The URL always uses today's UTC date so that the link reflects the current day's
// build attempt rather than the last known working build.
func LogCell(imageURL string) string {
	today := time.Now().UTC().Format("20060102")
	if logURL := LogURLFromImageURLForDate(imageURL, today); logURL != "" {
		return fmt.Sprintf("[🔗](%s)", logURL)
	}
	return "❌"
}

// LogURLFromImageURL derives the cd-build-log URL from a cdimage.ubuntu.com image URL,
// using the date embedded in that URL.
//
// The image URL is expected to follow the pattern:
//
//	https://cdimage.ubuntu.com/{folder}/{release}/{log_prefix}/{date}/{filename}
//
// The returned log URL follows:
//
//	https://ubuntu-archive-team.ubuntu.com/cd-build-logs/{folder}/{release}/{log_prefix}-{date}.log
//
// Returns "" if imageURL is empty, the host is not cdimage.ubuntu.com, or the path
// does not contain the required number of segments.
func LogURLFromImageURL(imageURL string) string {
	folder, release, logPrefix, ok := parseImageURLParts(imageURL)
	if !ok {
		return ""
	}
	// Extract and validate the date from the URL itself (parts[4]).
	rest := imageURL
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}
	parts := strings.SplitN(rest, "/", 8)
	date := parts[4]
	if i := strings.IndexByte(date, '.'); i != -1 {
		date = date[:i]
	}
	if len(date) != 8 {
		return ""
	}
	for _, c := range date {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return fmt.Sprintf("%s/%s/%s/%s-%s.log", baseLogURL, folder, release, logPrefix, date)
}

// LogURLFromImageURLForDate derives the cd-build-log URL from a cdimage.ubuntu.com image
// URL but substitutes the provided date (YYYYMMDD) in place of the date embedded in the
// image URL. This is used to construct a log link for a specific date — typically today —
// regardless of which build date the Test Observer API last reported.
//
// Returns "" if imageURL cannot be parsed (same conditions as LogURLFromImageURL).
func LogURLFromImageURLForDate(imageURL, date string) string {
	folder, release, logPrefix, ok := parseImageURLParts(imageURL)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s/%s-%s.log", baseLogURL, folder, release, logPrefix, date)
}

// parseImageURLParts extracts the (folder, release, logPrefix) structural components
// from a cdimage.ubuntu.com image URL. Returns ok=false if the URL is empty, has the
// wrong host, or has too few path segments.
func parseImageURLParts(imageURL string) (folder, release, logPrefix string, ok bool) {
	if imageURL == "" {
		return "", "", "", false
	}
	rest := imageURL
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}
	// Expected: host / folder / release / log_prefix / date / filename
	parts := strings.SplitN(rest, "/", 8)
	if len(parts) < 6 {
		return "", "", "", false
	}
	if parts[0] != "cdimage.ubuntu.com" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

// ImageAge returns a human-readable age string for a YYYYMMDD or YYYYMMDD.N version field.
func ImageAge(version string) string {
	if i := strings.IndexByte(version, '.'); i != -1 {
		version = version[:i]
	}
	if len(version) != 8 {
		return "unknown"
	}
	t, err := time.Parse("20060102", version)
	if err != nil {
		return "unknown"
	}
	days := int(time.Since(t).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day"
	case days < 14:
		return fmt.Sprintf("%d days", days)
	case days < 60:
		weeks := days / 7
		if weeks == 1 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", weeks)
	default:
		months := days / 30
		if months == 1 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", months)
	}
}

// IsDisplayable returns true when the execution represents a real test result
// worth surfacing to the user. Filtering rules:
//   - "Image build" is always skipped — it is a build availability notification,
//     not a test result.
//   - "Manual Testing" with status IN_PROGRESS is skipped — it means no tester
//     has submitted results yet (placeholder execution with no test_results).
func IsDisplayable(te TestExecution) bool {
	if te.TestPlan == "Image build" {
		return false
	}
	if te.TestPlan == "Manual Testing" && te.Status == "IN_PROGRESS" {
		return false
	}
	return true
}

// ExecStatusEmoji returns an emoji for a TestExecution status string.
func ExecStatusEmoji(status string) string {
	switch status {
	case "PASSED":
		return "✅"
	case "FAILED":
		return "❌"
	case "IN_PROGRESS":
		return "🔄"
	case "NOT_STARTED":
		return "⏳"
	default:
		return "⚠️"
	}
}

// ParseLaunchpadBuildURLs scans a cd-build-log for lines of the form:
//
//	ubuntu-{label}: https://launchpad.net/~ubuntu-cdimage/+livefs/...
//
// and returns a map of label → Launchpad build page URL. The label is the
// portion of the build name after "ubuntu-" (e.g. "amd64" for standard builds,
// "desktop-preinstalled-arm64-raspi" for variant builds). Callers that need to
// match by architecture should use substring matching against these labels.
// Lines that do not match the pattern are silently ignored.
func ParseLaunchpadBuildURLs(cdBuildLog string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(cdBuildLog, "\n") {
		line = strings.TrimSpace(line)
		// Expected format: "{flavour}-{arch}: https://launchpad.net/..."
		// The flavour prefix may be "ubuntu", "edubuntu", "xubuntu", "kubuntu", etc.
		// We match on the URL content rather than the prefix to stay flavour-agnostic.
		colon := strings.Index(line, ": ")
		if colon < 0 {
			continue
		}
		label := line[:colon]
		url := strings.TrimSpace(line[colon+2:])
		if !strings.HasPrefix(url, "https://launchpad.net/") {
			continue
		}
		if !strings.Contains(url, "/+build/") {
			continue
		}
		// Strip the leading "<flavour>-" prefix from the label so that callers
		// can match against the arch portion (e.g. "amd64", "arm64-raspi").
		// The flavour prefix is everything up to and including the first "-".
		if dash := strings.Index(label, "-"); dash >= 0 {
			label = label[dash+1:]
		}
		result[label] = url
	}
	return result
}

// PrimaryBuildArch returns the architecture to investigate from a slice of
// ArtefactBuilds. Preference order: amd64 > arm64 (or arm64+variant) > first alphabetically.
// Returns "" when builds is empty.
func PrimaryBuildArch(builds []ArtefactBuild) string {
	if len(builds) == 0 {
		return ""
	}
	for _, pref := range []string{"amd64", "arm64"} {
		for _, b := range builds {
			// Use HasPrefix so that composite arches like "arm64+raspi" match "arm64".
			if b.Architecture == pref || strings.HasPrefix(b.Architecture, pref+"+") {
				return b.Architecture
			}
		}
	}
	// Fall back to alphabetically first.
	first := builds[0].Architecture
	for _, b := range builds[1:] {
		if b.Architecture < first {
			first = b.Architecture
		}
	}
	return first
}

// ArtefactArch extracts the CPU architecture from an artefact name, normalising
// "+" to "-" to match the convention used in cd-build-log labels.
//
// The artefact name follows the pattern:
//
//	{release}-{product...}-{arch}[+{variant}...].{ext...}
//
// The arch token is identified by scanning for known base arch strings
// (amd64, arm64, riscv64, ppc64el, s390x, armhf, i386) and returning from
// that position to the end of the stripped name (before the extension),
// capturing any "+variant" suffix.
//
// Examples:
//
//	stonking-desktop-amd64.iso                         → "amd64"
//	stonking-live-server-arm64+largemem.iso             → "arm64-largemem"
//	stonking-preinstalled-server-arm64+raspi.img.xz     → "arm64-raspi"
//	jammy-preinstalled-server-arm64+tegra-jetson.img.xz → "arm64-tegra-jetson"
//	stonking-wsl-amd64.wsl                             → "amd64"
//
// Returns "" when no known architecture is found.
func ArtefactArch(name string) string {
	// Strip all known extensions (may be compound, e.g. ".img.xz").
	for _, ext := range []string{".img.xz", ".tar.gz", ".iso", ".wsl", ".img"} {
		name = strings.TrimSuffix(name, ext)
	}
	// Scan for known base arch strings; return from the "-arch" boundary to end.
	// Order matters: longer tokens first to avoid "arm64" matching inside "arm64+raspi"
	// at the wrong position.
	for _, baseArch := range []string{"riscv64", "ppc64el", "s390x", "amd64", "arm64", "armhf", "i386"} {
		// Look for "-{baseArch}" as a boundary — the arch always follows a dash.
		needle := "-" + baseArch
		idx := strings.Index(name, needle)
		if idx < 0 {
			continue
		}
		arch := name[idx+1:] // strip the leading "-"
		// Normalise "+" → "-" to match cd-build-log label convention.
		return strings.ReplaceAll(arch, "+", "-")
	}
	return ""
}

// LogPrefixFromImageURL extracts the log-prefix path segment (third component
// after the host in a cdimage.ubuntu.com URL, e.g. "daily-preinstalled",
// "daily-live"). Returns "" when the URL cannot be parsed.
func LogPrefixFromImageURL(imageURL string) string {
	_, _, logPrefix, ok := parseImageURLParts(imageURL)
	if !ok {
		return ""
	}
	return logPrefix
}

// preinstalledLabelMap maps a (logPrefix, baseArch) pair to the canonical label
// suffix used in the corresponding cd-build-log "on Launchpad" lines.
//
// Background: the daily-preinstalled logs use labels of the form
// "{product}-{arch}-{variant}" (e.g. "ubuntu-server-amd64-generic") rather than
// "{product}-{arch}" as used by daily-live logs. Because each preinstalled artefact
// that does NOT encode a hardware variant in its filename (e.g.
// "stonking-preinstalled-server-amd64.img.xz") only carries the base arch, the
// standard suffix-match in labelMatchesArch fails to match any log label.
//
// The mapping is intentionally explicit (Option C) rather than using a generic
// infix rule, because:
//   - The naming convention in cd-build-logs is not uniform across log prefixes.
//   - For arches with multiple variants (e.g. riscv64 on noble has 7 hardware
//     variants), an infix rule would match all of them and produce ambiguous
//     multi-label semantics (some succeed, some fail).
//   - The canonical variant ("generic") represents the reference platform for
//     each arch; its outcome is the most meaningful single signal for the artefact.
//
// This mapping is expected to be temporary; a future Test Observer replacement
// will expose build status directly through the API, making log parsing obsolete.
//
// Key format: "logPrefix/baseArch" (e.g. "daily-preinstalled/amd64").
// Value: canonical label suffix (e.g. "amd64-generic").
var preinstalledLabelMap = map[string]string{
	"daily-preinstalled/amd64":   "amd64-generic",
	"daily-preinstalled/arm64":   "arm64-generic",
	"daily-preinstalled/riscv64": "riscv64-generic",
}

// ResolveLogLabel returns the canonical label suffix to use when scanning a
// cd-build-log for the given logPrefix and arch combination.
//
// For most artefacts the arch itself is the correct label suffix (e.g. "amd64"
// in "ubuntu-server-live-amd64 on Launchpad starting at ..."). For preinstalled
// builds the log uses "{arch}-{variant}" labels; this function returns the
// canonical variant label from preinstalledLabelMap, or arch unchanged when no
// mapping entry exists.
//
// logPrefix is the third path segment of the cdimage URL
// (e.g. "daily-preinstalled", "daily-live").
func ResolveLogLabel(logPrefix, arch string) string {
	key := logPrefix + "/" + arch
	if canonical, ok := preinstalledLabelMap[key]; ok {
		return canonical
	}
	return arch
}

// ParseBuildStatusFromLog determines the build status, failure kind, and a short
// failure description for a specific architecture by scanning the content of a
// cd-build-log.
//
// The function looks for lines of the form:
//
//	{label} on Launchpad starting at {datetime}
//	{label} on Launchpad finished at {datetime} ({result})
//
// where {label} is suffix-matched against the normalised arch (e.g. "amd64"
// matches "ubuntu-amd64", "ubuntu-server-live-amd64", etc.).
//
// Status detection rules (in evaluation order):
//   - Empty content or empty arch                                         → NOT_STARTED, kind=none
//   - No "starting at" line for this arch + run_live_builds traceback     → FAILED, INFRA
//   - No "starting at" line for this arch                                 → NOT_STARTED, kind=none
//   - "starting at" present, no "finished at", any traceback              → FAILED, INFRA  (orphaned)
//   - "starting at" present, no "finished at", no traceback               → IN_PROGRESS, kind=none
//   - "finished at" with "(Successfully built)" + Test Observer error     → FAILED, INFRA  (TO submit failure)
//   - "finished at" with "(Successfully built)" + any traceback           → FAILED, INFRA  (publish crash)
//   - "finished at" with "(Successfully built)", no traceback             → BUILT, kind=none
//   - "finished at" with "(Chroot problem)"                               → FAILED, INFRA  (LP builder)
//   - "finished at" with any other non-success suffix                     → FAILED, PRODUCT
//
// The arch-specific "finished at" result always takes precedence over a
// run_live_builds traceback. A run_live_builds traceback that appears after an
// arch's "(Failed to build)" result reflects a subsequent cdimage crash (e.g.
// LiveBuildsFailed raised because another arch failed), not a pre-submission
// infrastructure problem. Only when no "finished at" line exists for this arch
// does the run_live_builds traceback indicate a true Phase 1 infra failure.
//
// Phase 1 infra detection covers four cases:
//  1. No "starting at" line AND run_live_builds traceback: cdimage crashed before
//     submitting builds to Launchpad.
//  2. "starting at" present, no "finished at", any traceback: cdimage crashed
//     mid-run (e.g. disk full), orphaning in-flight builds.
//  3. "finished at (Successfully built)" AND Test Observer submission error:
//     LP build succeeded and image was published but cdimage failed to register
//     it in Test Observer — artefact is missing from Test Observer despite the build.
//  4. "finished at (Successfully built)" AND any other traceback: LP build succeeded
//     but cdimage crashed during publishing — image is unavailable despite the LP result.
func ParseBuildStatusFromLog(logContent, arch string) (BuildStatusState, BuildFailureKind, string) {
	if logContent == "" || arch == "" {
		return BuildStatusNotStarted, BuildFailureKindNone, ""
	}

	// Normalise arch for matching: "arm64+raspi" → "arm64-raspi".
	normArch := strings.ReplaceAll(arch, "+", "-")

	started := false
	finished := false
	finishedSuccess := false
	finishedChroot := false

	for _, line := range strings.Split(logContent, "\n") {
		line = strings.TrimSpace(line)

		// Match lines containing "on Launchpad starting at" or "on Launchpad finished at".
		// The label before "on Launchpad" is checked by suffix against normArch.
		if idx := strings.Index(line, " on Launchpad starting at "); idx >= 0 {
			label := line[:idx]
			if labelMatchesArch(label, normArch) {
				started = true
			}
			continue
		}
		if idx := strings.Index(line, " on Launchpad finished at "); idx >= 0 {
			label := line[:idx]
			if labelMatchesArch(label, normArch) {
				finished = true
				finishedSuccess = strings.Contains(line, "(Successfully built)")
				finishedChroot = strings.Contains(line, "(Chroot problem)")
			}
			continue
		}
	}

	switch {
	case !started:
		// Phase 1 check A: run_live_builds traceback with no "starting at" line means
		// cdimage crashed before posting any builds to Launchpad.
		if hasRunLiveBuildsTraceback(logContent) {
			return BuildStatusFailed, BuildFailureKindInfra,
				"cdimage crashed before submitting builds to Launchpad"
		}
		return BuildStatusNotStarted, BuildFailureKindNone, ""
	case !finished:
		// Phase 1 check B: build started but never finished AND the log contains any
		// Python traceback — cdimage crashed mid-run (e.g. disk full on the cdimage host).
		// The arch is treated as an orphaned victim of the infra crash.
		if hasAnyTraceback(logContent) {
			return BuildStatusFailed, BuildFailureKindInfra,
				"cdimage crashed mid-run, build was orphaned"
		}
		return BuildStatusInProgress, BuildFailureKindNone, ""
	case finishedSuccess:
		// Phase 1 check C: LP reported success but the image did not reach Test Observer.
		// Check the most specific signal first so the description is actionable.
		if hasTestObserverSubmitFailure(logContent) {
			return BuildStatusFailed, BuildFailureKindInfra,
				"LP build succeeded but image could not be submitted to Test Observer"
		}
		// Phase 1 check D: LP reported success but a traceback is present, meaning
		// cdimage crashed after the LP build completed — typically during publishing.
		// The image is unavailable despite the LP result; this is an infra failure.
		if hasAnyTraceback(logContent) {
			return BuildStatusFailed, BuildFailureKindInfra,
				"LP build succeeded but cdimage crashed during publishing"
		}
		// The build finished successfully on Launchpad, but the artefact may not yet be
		// in Test Observer (publishing can lag). Callers should prefer checking
		// IsBuiltToday first; this value is returned only for log-only assessment.
		return BuildStatusBuilt, BuildFailureKindNone, ""
	case finishedChroot:
		// "(Chroot problem)" is reported by Launchpad but reflects an LP builder
		// infrastructure failure, not a product defect.
		return BuildStatusFailed, BuildFailureKindInfra,
			"Launchpad builder reported a chroot problem"
	default:
		// "(Failed to build)" or any other non-success suffix: Phase 2 product failure.
		// A run_live_builds traceback may also be present (e.g. LiveBuildsFailed raised
		// because this arch failed), but the arch-specific LP result takes precedence.
		return BuildStatusFailed, BuildFailureKindProduct, ""
	}
}

// labelMatchesArch reports whether a cd-build-log label (e.g. "ubuntu-server-live-amd64")
// contains the normalised arch token (e.g. "amd64" or "arm64-largemem") as a
// complete token suffix. Matching is done by checking that the label ends with
// "-{normArch}" so that "arm64" does not match "arm64-largemem".
// Case-insensitive to be robust against future label format changes.
func labelMatchesArch(label, normArch string) bool {
	label = strings.ToLower(label)
	arch := strings.ToLower(normArch)
	return strings.HasSuffix(label, "-"+arch) || label == arch
}

// hasRunLiveBuildsTraceback reports whether the cd-build-log content contains a
// Python traceback that passed through run_live_builds in cdimage. This indicates
// a Phase 1 infrastructure failure: cdimage crashed before it could post any builds
// to Launchpad, so no "on Launchpad starting at" lines will appear for any arch.
//
// Detection requires both markers to be present:
//   - "Traceback (most recent call last):" — standard Python traceback header
//   - "in run_live_builds" — confirms the crash site is in cdimage's livefs.py
func hasRunLiveBuildsTraceback(logContent string) bool {
	return strings.Contains(logContent, "Traceback (most recent call last):") &&
		strings.Contains(logContent, "in run_live_builds")
}

// hasAnyTraceback reports whether the cd-build-log contains any Python traceback.
// This is used as a secondary Phase 1 infra signal: if a traceback is present and
// a specific arch's build was started but never finished, cdimage likely crashed
// mid-run (e.g. disk full on the cdimage host), orphaning the in-flight builds.
func hasAnyTraceback(logContent string) bool {
	return strings.Contains(logContent, "Traceback (most recent call last):")
}

// hasTestObserverSubmitFailure reports whether the cd-build-log contains the
// cdimage error line emitted when the Test Observer API call fails after a
// successful LP build. This indicates that the image was built and published
// to cdimage but was not registered in Test Observer — the artefact is absent
// from Test Observer not because the build failed, but because the submission
// step encountered an error (e.g. Test Observer returned a 5xx response).
func hasTestObserverSubmitFailure(logContent string) bool {
	return strings.Contains(logContent, "Couldn't submit artifact to Test Observer")
}

// signaturePattern pairs a compiled regexp with the template used to build the
// canonical signature slug. Capture group 1 (when present) is substituted for
// "$1" in the template.
type signaturePattern struct {
	re       *regexp.Regexp
	template string // use $1 to include the first capture group
}

// signaturePatterns is the ordered priority list used by ExtractFailureSignature.
// Patterns are checked top-to-bottom; the first match wins.
var signaturePatterns = []signaturePattern{
	// dpkg errors — most specific first so we don't fall through to sub-process error
	{regexp.MustCompile(`dpkg: error processing package (\S+)`), "dpkg:$1"},
	{regexp.MustCompile(`dpkg-deb: error:`), "dpkg-deb:error"},
	{regexp.MustCompile(`Sub-process /usr/bin/dpkg returned an error`), "dpkg:subprocess-error"},
	// apt errors
	{regexp.MustCompile(`E: Unable to locate package (\S+)`), "apt:missing:$1"},
	{regexp.MustCompile(`E: Package '(\S+)' has no installation candidate`), "apt:no-candidate:$1"},
	{regexp.MustCompile(`(\S+) : Depends:`), "apt:unmet-dep:$1"},
	{regexp.MustCompile(`Cannot initiate the connection to|Temporary failure resolving`), "apt:network-error"},
	// snap errors
	{regexp.MustCompile(`error: cannot install '(\S+)'`), "snap:install:$1"},
	{regexp.MustCompile(`error: snap "(\S+)"`), "snap:$1"},
	// debootstrap
	{regexp.MustCompile(`debootstrap: `), "debootstrap:error"},
}

// ExtractFailureSignature scans the last 200 lines of logContent for known
// PRODUCT build-failure patterns and returns a short canonical slug
// (e.g. "apt:missing:libfoo-dev", "dpkg:libbar") that can be used to group
// multiple failing artefacts sharing the same root cause.
//
// Returns "" when no known pattern is matched. Callers should fall back to
// LLM analysis in that case.
//
// The function is pure (no I/O) and safe for concurrent use.
func ExtractFailureSignature(logContent string) string {
	// Work on the last 200 lines to match the same window the LLM sees.
	lines := strings.Split(logContent, "\n")
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		for _, p := range signaturePatterns {
			m := p.re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			sig := p.template
			if len(m) > 1 {
				// Strip trailing punctuation from the captured package name
				// so "libfoo-dev," and "libfoo-dev" both produce the same slug.
				pkg := strings.TrimRight(m[1], ",:;()")
				sig = strings.ReplaceAll(sig, "$1", pkg)
			}
			return sig
		}
	}
	return ""
}

// GroupBySignature groups active FailureRecords by their FailureSignature.
// Records with an empty FailureSignature are placed under the "" key.
// The returned map is never nil.
func GroupBySignature(records []FailureRecord) map[string][]FailureRecord {
	out := make(map[string][]FailureRecord)
	for _, r := range records {
		out[r.FailureSignature] = append(out[r.FailureSignature], r)
	}
	return out
}

// InfraSignatureFor returns a canonical slug for a known INFRA failure
// description, enabling cross-artefact grouping without LLM analysis.
// Returns "" for unrecognised or empty descriptions (no signature assigned).
//
// The two distinct wordings for "Launchpad produced no log" are normalised
// to the same slug so that records created via different code paths are
// grouped together correctly.
func InfraSignatureFor(desc string) string {
	switch desc {
	case "Launchpad build produced no log (builder accepted the job but never attached a log)",
		"launchpad build completed with no log available":
		return "infra:lp-no-log"
	case "LP build succeeded but cdimage crashed during publishing":
		return "infra:cdimage-publish-crash"
	case "LP build succeeded but image could not be submitted to Test Observer":
		return "infra:to-submit-failure"
	case "cdimage crashed mid-run, build was orphaned":
		return "infra:cdimage-midrun-crash"
	case "cdimage crashed before submitting builds to Launchpad":
		return "infra:cdimage-presubmit-crash"
	case "Launchpad builder reported a chroot problem":
		return "infra:lp-chroot-problem"
	}
	return ""
}
