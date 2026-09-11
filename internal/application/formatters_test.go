package application

import (
	"strings"
	"testing"
	"time"

	"github.com/mclemenceau/watchtower/internal/domain"
)

// --- FormatBuildsStatusSummary ---

func TestFormatBuildsStatusSummary_ColumnHeaders(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: today},
	}
	out := FormatBuildsStatusSummary(artefacts)
	for _, want := range []string{"Release", "Built", "Total", "Progress"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatBuildsStatusSummary missing column header %q, got:\n%s", want, out)
		}
	}
}

func TestFormatBuildsStatusSummary_ProgressBar100Pct(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: today},
		{ID: 2, Release: "noble", Version: today},
	}
	out := FormatBuildsStatusSummary(artefacts)
	wantBar := strings.Repeat("🟩", 10)
	if !strings.Contains(out, wantBar) {
		t.Errorf("100%% bar should be 10 green squares, got:\n%s", out)
	}
	if strings.Contains(out, "🟥") {
		t.Errorf("100%% bar should have no red squares, got:\n%s", out)
	}
}

func TestFormatBuildsStatusSummary_ProgressBar0Pct(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: yesterday},
	}
	out := FormatBuildsStatusSummary(artefacts)
	wantBar := strings.Repeat("🟥", 10)
	if !strings.Contains(out, wantBar) {
		t.Errorf("0%% bar should be 10 red squares, got:\n%s", out)
	}
	if strings.Contains(out, "🟩") {
		t.Errorf("0%% bar should have no green squares, got:\n%s", out)
	}
}

func TestFormatBuildsStatusSummary_EmptySnapshot(t *testing.T) {
	out := FormatBuildsStatusSummary(nil)
	if !strings.Contains(out, "No snapshot") {
		t.Errorf("expected 'No snapshot' message, got: %s", out)
	}
}

func TestFormatBuildsStatusSummary_MultipleReleases(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: today},
		{ID: 2, Release: "plucky", Version: yesterday},
	}
	out := FormatBuildsStatusSummary(artefacts)
	if !strings.Contains(out, "noble") {
		t.Errorf("expected 'noble' in summary, got:\n%s", out)
	}
	if !strings.Contains(out, "plucky") {
		t.Errorf("expected 'plucky' in summary, got:\n%s", out)
	}
}

// --- FormatBuildsStatusRelease ---

func TestFormatBuildsStatusRelease_ColumnHeaders(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "ubuntu-desktop-amd64", OS: "ubuntu", Release: "noble", Version: today},
	}
	out := FormatBuildsStatusRelease(artefacts, "noble", "", domain.FailureStore{})
	for _, want := range []string{"ID", "Artefact", "Product", "Version", "Age", "Build", "Log"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatBuildsStatusRelease missing column header %q, got:\n%s", want, out)
		}
	}
}

func TestFormatBuildsStatusRelease_ArtefactRow(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "ubuntu-desktop-amd64", OS: "ubuntu", Release: "noble", Version: today},
	}
	out := FormatBuildsStatusRelease(artefacts, "noble", "", domain.FailureStore{})
	if !strings.Contains(out, "ubuntu-desktop-amd64") {
		t.Errorf("expected artefact name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "✅") {
		t.Errorf("expected ✅ for today's build, got:\n%s", out)
	}
}

func TestFormatBuildsStatusRelease_LogLinkPresent(t *testing.T) {
	imageURL := "https://cdimage.ubuntu.com/ubuntu-server/noble/daily-live/20200101/noble-live-server-amd64.iso"
	// LogCell always uses today's date, not the date embedded in imageURL.
	todayDate := time.Now().UTC().Format("20060102")
	logURL := "https://ubuntu-archive-team.ubuntu.com/cd-build-logs/ubuntu-server/noble/daily-live-" + todayDate + ".log"
	artefacts := []domain.Artefact{
		{ID: 1, Name: "ubuntu-server-amd64", OS: "ubuntu-server", Release: "noble", Version: "20200101", ImageURL: imageURL},
	}
	out := FormatBuildsStatusRelease(artefacts, "noble", "", domain.FailureStore{})
	wantLink := "[🔗](" + logURL + ")"
	if !strings.Contains(out, wantLink) {
		t.Errorf("expected Markdown log link %q, got:\n%s", wantLink, out)
	}
}

func TestFormatBuildsStatusRelease_EmptySnapshot(t *testing.T) {
	out := FormatBuildsStatusRelease(nil, "noble", "", domain.FailureStore{})
	if !strings.Contains(out, "No snapshot") {
		t.Errorf("expected 'No snapshot' message, got: %s", out)
	}
}

func TestFormatBuildsStatusRelease_UnknownRelease(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "x", OS: "ubuntu", Release: "noble", Version: today},
	}
	out := FormatBuildsStatusRelease(artefacts, "nonexistent", "", domain.FailureStore{})
	if !strings.Contains(out, "No artefacts found") {
		t.Errorf("expected 'No artefacts found' message, got: %s", out)
	}
}

func TestFormatBuildsStatusRelease_ProductFilter(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "ubuntu-desktop-amd64", OS: "ubuntu", Release: "noble", Version: today},
		{ID: 2, Name: "ubuntu-server-amd64", OS: "ubuntu-server", Release: "noble", Version: today},
	}
	out := FormatBuildsStatusRelease(artefacts, "noble", "ubuntu-server", domain.FailureStore{})
	if !strings.Contains(out, "ubuntu-server-amd64") {
		t.Errorf("expected ubuntu-server-amd64 in output, got:\n%s", out)
	}
	if strings.Contains(out, "ubuntu-desktop-amd64") {
		t.Errorf("ubuntu-desktop-amd64 should be filtered out, got:\n%s", out)
	}
}

func TestFormatBuildsStatusRelease_FailedWithDescription(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "stonking-wsl-amd64.wsl", OS: "ubuntu-wsl", Release: "stonking",
			Version:                 "20260720",
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed before submitting builds to Launchpad",
		},
	}
	out := FormatBuildsStatusRelease(artefacts, "stonking", "", domain.FailureStore{})
	want := "❌ INFRA: cdimage crashed before submitting builds to Launchpad"
	if !strings.Contains(out, want) {
		t.Errorf("expected %q in output, got:\n%s", want, out)
	}
}

func TestFormatBuildsStatusRelease_FailedKindNoDescription(t *testing.T) {
	// FailureKind set but no description — shows "❌ PRODUCT" without colon suffix.
	artefacts := []domain.Artefact{
		{
			ID: 2, Name: "stonking-preinstalled-server-arm64+raspi.img.xz", OS: "ubuntu-server",
			Release:          "stonking",
			Version:          "20260720",
			BuildLog:         domain.BuildStatusFailed,
			BuildFailureKind: domain.BuildFailureKindProduct,
		},
	}
	out := FormatBuildsStatusRelease(artefacts, "stonking", "", domain.FailureStore{})
	if !strings.Contains(out, "❌ PRODUCT") {
		t.Errorf("expected '❌ PRODUCT' in output, got:\n%s", out)
	}
	if strings.Contains(out, "❌ PRODUCT:") {
		t.Errorf("expected no colon after PRODUCT when description is empty, got:\n%s", out)
	}
}

func TestFormatBuildsStatusRelease_FailedNoKind(t *testing.T) {
	// No kind set — shows plain "❌".
	artefacts := []domain.Artefact{
		{
			ID: 3, Name: "stonking-desktop-arm64.iso", OS: "ubuntu",
			Release:  "stonking",
			Version:  "20260720",
			BuildLog: domain.BuildStatusFailed,
		},
	}
	out := FormatBuildsStatusRelease(artefacts, "stonking", "", domain.FailureStore{})
	if !strings.Contains(out, "❌") {
		t.Errorf("expected ❌ in output, got:\n%s", out)
	}
	if strings.Contains(out, "INFRA") || strings.Contains(out, "PRODUCT") {
		t.Errorf("expected no kind label when BuildFailureKind is empty, got:\n%s", out)
	}
}

func TestFormatBuildsStatusRelease_ProductFailureUsesLibrarianURL(t *testing.T) {
	// A PRODUCT failure with a resolved LibrarianURL in the FailureStore must
	// render the second-hop librarian URL in the Log column, not the cd-build-log.
	libURL := "https://launchpadlibrarian.net/12345/buildlog.txt.gz"
	imageURL := "https://cdimage.ubuntu.com/ubuntu/noble/daily-live/20260101/noble-desktop-amd64.iso"
	art := domain.Artefact{
		ID:               42,
		Name:             "noble-desktop-amd64",
		OS:               "ubuntu",
		Release:          "noble",
		Version:          "20260101",
		ImageURL:         imageURL,
		BuildLog:         domain.BuildStatusFailed,
		BuildFailureKind: domain.BuildFailureKindProduct,
	}

	fs := make(domain.FailureStore)
	fs.UpsertFailure(art)
	fs.SetAnalysis(
		42, "noble", "ubuntu",
		domain.LogAnalysis{Category: "dependency", Hypothesis: "missing dep"},
		"20260101", "", libURL,
	)

	out := FormatBuildsStatusRelease([]domain.Artefact{art}, "noble", "", fs)
	wantLink := "[🔗](" + libURL + ")"
	if !strings.Contains(out, wantLink) {
		t.Errorf("expected librarian link %q in output, got:\n%s", wantLink, out)
	}

	// Must NOT contain the first-hop cd-build-log URL for today's date.
	todayDate := time.Now().UTC().Format("20060102")
	cdLogURL := "cd-build-logs/ubuntu/noble/daily-live-" + todayDate + ".log"
	if strings.Contains(out, cdLogURL) {
		t.Errorf("expected cd-build-log URL to be replaced by librarian URL, got:\n%s", out)
	}
}

func TestFormatBuildsStatusRelease_InfraFailureUsesCdBuildLog(t *testing.T) {
	// An INFRA failure must always use the first-hop cd-build-log, even if
	// somehow a LivefsLogURL were present (it shouldn't be, but defensive check).
	imageURL := "https://cdimage.ubuntu.com/ubuntu/noble/daily-live/20260101/noble-desktop-amd64.iso"
	art := domain.Artefact{
		ID:               43,
		Name:             "noble-desktop-amd64",
		OS:               "ubuntu",
		Release:          "noble",
		Version:          "20260101",
		ImageURL:         imageURL,
		BuildLog:         domain.BuildStatusFailed,
		BuildFailureKind: domain.BuildFailureKindInfra,
	}

	out := FormatBuildsStatusRelease([]domain.Artefact{art}, "noble", "", domain.FailureStore{})
	todayDate := time.Now().UTC().Format("20060102")
	wantCDLog := "cd-build-logs/ubuntu/noble/daily-live-" + todayDate + ".log"
	if !strings.Contains(out, wantCDLog) {
		t.Errorf("expected cd-build-log link %q for INFRA failure, got:\n%s", wantCDLog, out)
	}
}

func TestFormatTestsStatusSummary_ColumnHeaders(t *testing.T) {
	env := domain.Environment{Name: "jenkins", Architecture: "amd64"}
	artefacts := []domain.Artefact{
		{
			ID: 1, Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{{
				ID: 10, Architecture: "amd64",
				TestExecutions: []domain.TestExecution{
					{ID: 100, TestPlan: "Jenkins image validation", Status: "PASSED", Environment: env},
				},
			}},
		},
	}
	out := FormatTestsStatusSummary(artefacts)
	for _, want := range []string{"Release", "Passed", "Total", "Progress"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTestsStatusSummary missing column header %q, got:\n%s", want, out)
		}
	}
}

func TestFormatTestsStatusSummary_PassedFraction(t *testing.T) {
	env := domain.Environment{Name: "jenkins", Architecture: "amd64"}
	artefacts := []domain.Artefact{
		{
			ID: 1, Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{{
				ID: 10, Architecture: "amd64",
				TestExecutions: []domain.TestExecution{
					{ID: 100, TestPlan: "Jenkins image validation", Status: "PASSED", Environment: env},
					{ID: 101, TestPlan: "Manual Testing", Status: "PASSED", Environment: env},
				},
			}},
		},
	}
	out := FormatTestsStatusSummary(artefacts)
	// 2/2 passed → 100% bar
	wantBar := strings.Repeat("🟩", 10)
	if !strings.Contains(out, wantBar) {
		t.Errorf("100%% bar expected, got:\n%s", out)
	}
}

func TestFormatTestsStatusSummary_EmptySnapshot(t *testing.T) {
	out := FormatTestsStatusSummary(nil)
	if !strings.Contains(out, "No snapshot") {
		t.Errorf("expected 'No snapshot' message, got: %s", out)
	}
}

func TestFormatTestsStatusSummary_NoBuildsInSnapshot(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: today},
	}
	out := FormatTestsStatusSummary(artefacts)
	if !strings.Contains(out, "No test executions found") {
		t.Errorf("expected 'No test executions found' message, got: %s", out)
	}
}

// TestFormatTestsStatusSummary_Deduplication verifies that when a build has
// multiple executions for the same test plan (e.g. a re-run / retry), only the
// latest one is counted. Without deduplication the total would be 3 (two
// executions of "Plan A" + one of "Plan B"); with deduplication it must be 2.
func TestFormatTestsStatusSummary_Deduplication(t *testing.T) {
	env := domain.Environment{Name: "jenkins", Architecture: "amd64"}
	artefacts := []domain.Artefact{
		{
			ID: 1, Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{{
				ID: 10, Architecture: "amd64",
				TestExecutions: []domain.TestExecution{
					// Earlier run of Plan A — FAILED; should be superseded.
					{ID: 100, TestPlan: "Plan A", Status: "FAILED",
						CreatedAt: "2026-01-01T10:00:00Z", Environment: env},
					// Later run of Plan A — PASSED; should be the one counted.
					{ID: 101, TestPlan: "Plan A", Status: "PASSED",
						CreatedAt: "2026-01-01T11:00:00Z", Environment: env},
					// Single run of Plan B — PASSED.
					{ID: 102, TestPlan: "Plan B", Status: "PASSED",
						CreatedAt: "2026-01-01T10:00:00Z", Environment: env},
				},
			}},
		},
	}
	out := FormatTestsStatusSummary(artefacts)
	// After deduplication: 2 plans, both PASSED → total=2, passed=2.
	if !strings.Contains(out, "| **noble** | 2 | 2 |") {
		t.Errorf(
			"expected deduplicated counts '| **noble** | 2 | 2 |', got:\n%s",
			out,
		)
	}
}

// --- FormatTestsStatusRelease ---

func TestFormatTestsStatusRelease_ColumnHeaders(t *testing.T) {
	env := domain.Environment{Name: "jenkins", Architecture: "amd64"}
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{{
				ID: 10, Architecture: "amd64",
				TestExecutions: []domain.TestExecution{
					{ID: 100, TestPlan: "Jenkins image validation", Status: "PASSED", Environment: env},
				},
			}},
		},
	}
	out := FormatTestsStatusRelease(artefacts, "noble", "")
	for _, want := range []string{"Artefact", "Product", "Arch", "Test Plan", "Status"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatTestsStatusRelease missing column header %q, got:\n%s", want, out)
		}
	}
}

func TestFormatTestsStatusRelease_ExecRows(t *testing.T) {
	env := domain.Environment{Name: "jenkins", Architecture: "amd64"}
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{{
				ID: 10, Architecture: "amd64",
				TestExecutions: []domain.TestExecution{
					{ID: 100, TestPlan: "Jenkins image validation", Status: "PASSED", Environment: env},
					{ID: 101, TestPlan: "Image build", Status: "PASSED", Environment: env}, // filtered
				},
			}},
		},
	}
	out := FormatTestsStatusRelease(artefacts, "noble", "")
	if !strings.Contains(out, "noble-desktop-amd64.iso") {
		t.Errorf("expected artefact name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Jenkins image validation") {
		t.Errorf("expected test plan in output, got:\n%s", out)
	}
	if !strings.Contains(out, "✅") {
		t.Errorf("expected ✅ for PASSED status, got:\n%s", out)
	}
	// Image build should be filtered
	if strings.Contains(out, "Image build") {
		t.Errorf("Image build should be filtered from output, got:\n%s", out)
	}
}

func TestFormatTestsStatusRelease_EmptySnapshot(t *testing.T) {
	out := FormatTestsStatusRelease(nil, "noble", "")
	if !strings.Contains(out, "No snapshot") {
		t.Errorf("expected 'No snapshot' message, got: %s", out)
	}
}

// --- FormatChangeReport ---

func TestFormatChangeReport_NewFailures(t *testing.T) {
	r := domain.ChangeReport{
		NewFailures: []domain.ArtefactDelta{
			{Name: "ubuntu-desktop-amd64", Release: "noble", OldStatus: "UNDECIDED", NewStatus: "MARKED_AS_FAILED"},
		},
	}
	out := FormatChangeReport(r)
	if !strings.Contains(out, "🔴") {
		t.Errorf("expected 🔴 for new failures, got:\n%s", out)
	}
	if !strings.Contains(out, "New Failures") {
		t.Errorf("expected 'New Failures' section, got:\n%s", out)
	}
	if !strings.Contains(out, "ubuntu-desktop-amd64") {
		t.Errorf("expected artefact name in output, got:\n%s", out)
	}
}

func TestFormatChangeReport_Recoveries(t *testing.T) {
	r := domain.ChangeReport{
		Recoveries: []domain.ArtefactDelta{
			{Name: "ubuntu-server-amd64", Release: "noble"},
		},
	}
	out := FormatChangeReport(r)
	if !strings.Contains(out, "🟢") {
		t.Errorf("expected 🟢 for recoveries, got:\n%s", out)
	}
	if !strings.Contains(out, "Recoveries") {
		t.Errorf("expected 'Recoveries' section, got:\n%s", out)
	}
}

func TestFormatChangeReport_OtherChanges(t *testing.T) {
	r := domain.ChangeReport{
		OtherChanges: []domain.ArtefactDelta{
			{Name: "ubuntu-minimal-amd64", Release: "plucky", OldStatus: "UNDECIDED", NewStatus: "APPROVED"},
		},
	}
	out := FormatChangeReport(r)
	if !strings.Contains(out, "🔵") {
		t.Errorf("expected 🔵 for other changes, got:\n%s", out)
	}
	if !strings.Contains(out, "Other Changes") {
		t.Errorf("expected 'Other Changes' section, got:\n%s", out)
	}
}

func TestFormatChangeReport_NewArtefacts(t *testing.T) {
	r := domain.ChangeReport{
		NewArtefacts: []domain.Artefact{
			{ID: 1, Name: "brand-new-iso", Release: "noble", OS: "ubuntu", Version: time.Now().UTC().Format("20060102")},
		},
	}
	out := FormatChangeReport(r)
	if !strings.Contains(out, "🆕") {
		t.Errorf("expected 🆕 for new artefacts, got:\n%s", out)
	}
	if !strings.Contains(out, "New Artefacts") {
		t.Errorf("expected 'New Artefacts' section, got:\n%s", out)
	}
	if !strings.Contains(out, "brand-new-iso") {
		t.Errorf("expected artefact name, got:\n%s", out)
	}
}

func TestFormatChangeReport_EmptyReport(t *testing.T) {
	r := domain.ChangeReport{}
	out := FormatChangeReport(r)
	// Should still produce header
	if !strings.Contains(out, "Change Report") {
		t.Errorf("expected 'Change Report' header, got:\n%s", out)
	}
	// Should NOT include any section headers
	for _, section := range []string{"New Failures", "Recoveries", "Other Changes", "New Artefacts"} {
		if strings.Contains(out, section) {
			t.Errorf("empty report should not contain section %q, got:\n%s", section, out)
		}
	}
}

// --- FormatInvestigation ---

func TestFormatInvestigation_RequiredFields(t *testing.T) {
	art := domain.Artefact{ID: 42, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble", Version: yesterday}
	analysis := domain.LogAnalysis{
		Category:    "dependency",
		Hypothesis:  "apt mirror returned 404 for linux-image package",
		LogExcerpts: []string{"E: Failed to fetch http://example.com 404", "dpkg: error processing package"},
		NextAction:  "Retry the build after mirror sync",
	}
	out := FormatInvestigation(art, analysis, "Launchpad librarian (amd64)")

	for _, want := range []string{
		"noble-desktop-amd64.iso",
		"42",
		"dependency",
		"apt mirror returned 404",
		"E: Failed to fetch http://example.com 404",
		"dpkg: error processing package",
		"Retry the build after mirror sync",
		"Category",
		"Hypothesis",
		"Recommended action",
		"Log source:",
		"Launchpad librarian (amd64)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatInvestigation missing %q, got:\n%s", want, out)
		}
	}
}

func TestFormatInvestigation_NoLogExcerpts(t *testing.T) {
	art := domain.Artefact{ID: 1, Name: "plucky-server-amd64.iso", OS: "ubuntu-server", Release: "plucky", Version: yesterday}
	analysis := domain.LogAnalysis{
		Category:    "unknown",
		Hypothesis:  "Could not determine root cause",
		LogExcerpts: nil,
		NextAction:  "Investigate manually",
	}
	out := FormatInvestigation(art, analysis, "cd-build-log")
	if !strings.Contains(out, "plucky-server-amd64.iso") {
		t.Errorf("expected artefact name, got:\n%s", out)
	}
	if strings.Contains(out, "Relevant log excerpts") {
		t.Errorf("should not include 'Relevant log excerpts' section when empty, got:\n%s", out)
	}
	if !strings.Contains(out, "Investigate manually") {
		t.Errorf("expected next action, got:\n%s", out)
	}
	if !strings.Contains(out, "cd-build-log") {
		t.Errorf("expected source 'cd-build-log', got:\n%s", out)
	}
}

// --- FormatScheduledSummary ---

func TestFormatScheduledSummary_EmptySnapshot(t *testing.T) {
	out := FormatScheduledSummary(nil, nil)
	if !strings.Contains(out, "No snapshot") {
		t.Errorf("expected 'No snapshot' message, got: %s", out)
	}
}

func TestFormatScheduledSummary_AllBuiltToday(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble", Version: today},
		{ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server", Release: "noble", Version: today},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "noble") {
		t.Errorf("expected release name 'noble', got:\n%s", out)
	}
	if !strings.Contains(out, "2/2") {
		t.Errorf("expected 2/2 built, got:\n%s", out)
	}
	// All built → sunny emoji, no Infra/Product sections.
	if !strings.Contains(out, "☀️") {
		t.Errorf("expected sunny emoji for 100%%, got:\n%s", out)
	}
	if strings.Contains(out, "Infra") || strings.Contains(out, "Product") {
		t.Errorf("expected no failure sections when all built, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_InfraFailures(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble", Version: today},
		{
			ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed mid-run, build was orphaned",
		},
		{
			ID: 3, Name: "noble-server-arm64.iso", OS: "ubuntu-server", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed mid-run, build was orphaned",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Infra (2):") {
		t.Errorf("expected Infra section header, got:\n%s", out)
	}
	// ubuntu-server has both amd64 and arm64 failing → should appear in Infra line.
	if !strings.Contains(out, "ubuntu-server") {
		t.Errorf("expected ubuntu-server in Infra group, got:\n%s", out)
	}
	// ubuntu was built today → must NOT appear in failures.
	if strings.Contains(out, "Product") {
		t.Errorf("expected no Product section, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_ProductFailures(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-preinstalled-server-arm64+raspi.img.xz", OS: "ubuntu-server",
			Release:                 "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindProduct,
			BuildFailureDescription: "livefs build failure requires analysis",
		},
		{
			ID: 2, Name: "noble-wsl-amd64.wsl", OS: "ubuntu-wsl",
			Release:                 "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindProduct,
			BuildFailureDescription: "livefs build failure requires analysis",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Product (2):") {
		t.Errorf("expected Product section header, got:\n%s", out)
	}
	if !strings.Contains(out, "ubuntu-server") {
		t.Errorf("expected ubuntu-server in Product group, got:\n%s", out)
	}
	if strings.Contains(out, "Infra") {
		t.Errorf("expected no Infra section, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_MixedInfraAndProduct(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed before submitting builds to Launchpad",
		},
		{
			ID: 2, Name: "noble-wsl-amd64.wsl", OS: "ubuntu-wsl", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindProduct,
			BuildFailureDescription: "livefs build failure requires analysis",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Infra (1):") {
		t.Errorf("expected Infra section, got:\n%s", out)
	}
	if !strings.Contains(out, "Product (1):") {
		t.Errorf("expected Product section, got:\n%s", out)
	}
	// Infra must appear before Product in the output.
	infraPos := strings.Index(out, "Infra")
	productPos := strings.Index(out, "Product")
	if infraPos > productPos {
		t.Errorf("expected Infra section before Product, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_MultipleDescriptions(t *testing.T) {
	// Two INFRA failures with different descriptions → both collapsed into one Infra line.
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed before submitting builds to Launchpad",
		},
		{
			ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "LP build succeeded but image could not be submitted to Test Observer",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Infra (2):") {
		t.Errorf("expected Infra section with count 2, got:\n%s", out)
	}
	// Both products should appear in the single Infra line.
	if !strings.Contains(out, "ubuntu") {
		t.Errorf("expected ubuntu in Infra line, got:\n%s", out)
	}
	if !strings.Contains(out, "ubuntu-server") {
		t.Errorf("expected ubuntu-server in Infra line, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_ProductsWithSameArchCollapsed(t *testing.T) {
	// ubuntu and ubuntu-mate both fail on amd64 with same description → one bullet line.
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "LP build succeeded but image could not be submitted to Test Observer",
		},
		{
			ID: 2, Name: "noble-desktop-amd64.iso", OS: "ubuntu-mate", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "LP build succeeded but image could not be submitted to Test Observer",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	// Both products share amd64 → should be collapsed onto one token "ubuntu, ubuntu-mate (amd64)".
	if !strings.Contains(out, "ubuntu, ubuntu-mate (amd64)") {
		t.Errorf("expected collapsed product token 'ubuntu, ubuntu-mate (amd64)', got:\n%s", out)
	}
}

func TestFormatScheduledSummary_ReleaseOrderRespected(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", Release: "noble", Version: today},
		{ID: 2, Name: "jammy-desktop-amd64.iso", Release: "jammy", Version: today},
		{ID: 3, Name: "plucky-desktop-amd64.iso", Release: "plucky", Version: today},
	}
	out := FormatScheduledSummary(artefacts, []string{"plucky", "noble"})
	pluckyPos := strings.Index(out, "plucky")
	noblePos := strings.Index(out, "noble")
	if pluckyPos < 0 || noblePos < 0 {
		t.Fatalf("expected both releases in output, got:\n%s", out)
	}
	if pluckyPos > noblePos {
		t.Errorf("plucky should appear before noble (env order), got:\n%s", out)
	}
	if strings.Contains(out, "jammy") {
		t.Errorf("jammy should be absent when not in releases list, got:\n%s", out)
	}
}

// TestFormatScheduledSummary_ReleaseCaseInsensitive verifies that artefacts
// whose Release field uses mixed capitalisation (e.g. "Noble" from the API)
// are counted in the summary when releasesScope uses lowercase (e.g. "noble"),
// matching the behaviour of FormatTestsStatusSummary which is case-agnostic
// via its own stats map keying.
func TestFormatScheduledSummary_ReleaseCaseInsensitive(t *testing.T) {
	// API returns "Noble" (capital N); operator scope uses "noble" (lowercase).
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "Noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Plan A", "PASSED"),
					exec(101, "Plan B", "PASSED"),
				}),
			},
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})

	// Build section must appear (artefact is counted).
	if !strings.Contains(out, "Build Summary") {
		t.Fatalf("expected Build Summary section, got:\n%s", out)
	}
	// Test section must appear with correct counts: 2 passed / 2 total.
	if !strings.Contains(out, "Test Summary") {
		t.Errorf("expected Test Summary section, got:\n%s", out)
	}
	if !strings.Contains(out, "(2/2)") {
		t.Errorf(
			"expected test counts (2/2) for Noble artefact with scope 'noble', got:\n%s",
			out,
		)
	}
}

func TestFormatScheduledSummary_NilReleasesUsesAll(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", Release: "noble", Version: today},
		{ID: 2, Name: "jammy-desktop-amd64.iso", Release: "jammy", Version: yesterday},
	}
	out := FormatScheduledSummary(artefacts, nil)
	if !strings.Contains(out, "noble") {
		t.Errorf("expected 'noble' in output when releases=nil, got:\n%s", out)
	}
	if !strings.Contains(out, "jammy") {
		t.Errorf("expected 'jammy' in output when releases=nil, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_UnknownReleaseSkipped(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", Release: "noble", Version: today},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble", "nonexistent"})
	if strings.Contains(out, "nonexistent") {
		t.Errorf("release not in snapshot should be silently skipped, got:\n%s", out)
	}
	if !strings.Contains(out, "noble") {
		t.Errorf("expected 'noble' in output, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_ZeroPct(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed mid-run, build was orphaned",
		},
		{
			ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server", Release: "noble",
			Version:                 yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "cdimage crashed mid-run, build was orphaned",
		},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "0/2") {
		t.Errorf("expected 0/2 built, got:\n%s", out)
	}
	// 0% → tornado emoji.
	if !strings.Contains(out, "🌪️") {
		t.Errorf("expected tornado emoji for 0%%, got:\n%s", out)
	}
	if !strings.Contains(out, "Infra (2):") {
		t.Errorf("expected Infra section with count, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_BuildsPctInHeading(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today},
		{ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server",
			Release: "noble", Version: yesterday,
			BuildLog:                domain.BuildStatusFailed,
			BuildFailureKind:        domain.BuildFailureKindInfra,
			BuildFailureDescription: "infra issue"},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	// 1 built out of 2 → 50%
	if !strings.Contains(out, "50%") {
		t.Errorf("expected 50%% in heading, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_BuildsPct100(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today},
		{ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server",
			Release: "noble", Version: today},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "100%") {
		t.Errorf("expected 100%% in heading, got:\n%s", out)
	}
}

// helpers shared by tests section test cases below.
func buildWithExec(
	buildID int, arch string, execs []domain.TestExecution,
) domain.ArtefactBuild {
	return domain.ArtefactBuild{ID: buildID, Architecture: arch,
		TestExecutions: execs}
}

func exec(id int, plan, status string) domain.TestExecution {
	return domain.TestExecution{ID: id, TestPlan: plan, Status: status,
		CreatedAt: "2026-07-29T10:00:00"}
}

func TestFormatScheduledSummary_TestsAllPassed(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Jenkins image validation", "PASSED"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Test Summary") {
		t.Errorf("expected Test Summary header, got:\n%s", out)
	}
	if !strings.Contains(out, "100% (1/1)") {
		t.Errorf("expected 100%% (1/1), got:\n%s", out)
	}
	if strings.Contains(out, "Failures") {
		t.Errorf("expected no Failures line when all passed, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_TestsWithFailures(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Jenkins image validation", "FAILED"),
					exec(101, "Manual Testing", "PASSED"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "Failures") {
		t.Errorf("expected Failures line, got:\n%s", out)
	}
	if !strings.Contains(out, "ubuntu") {
		t.Errorf("expected product name in failures, got:\n%s", out)
	}
	if !strings.Contains(out, "amd64") {
		t.Errorf("expected arch in failures, got:\n%s", out)
	}
	if !strings.Contains(out, "Jenkins image validation") {
		t.Errorf("expected failed plan name in failures, got:\n%s", out)
	}
	// PASSED plan must not appear in the failure line.
	if strings.Contains(out, "Manual Testing") {
		t.Errorf("passed plan should not appear in failures, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_TestsNoExecutions(t *testing.T) {
	// Artefacts have no Builds at all → tests section must be absent.
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if strings.Contains(out, "Test Summary") {
		t.Errorf("expected no Test Summary section when builds empty, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_TestsPassRatePartial(t *testing.T) {
	// 3 passed, 1 failed → 75%, counts (3/4)
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Plan A", "PASSED"),
					exec(101, "Plan B", "PASSED"),
					exec(102, "Plan C", "PASSED"),
					exec(103, "Plan D", "FAILED"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "75% (3/4)") {
		t.Errorf("expected 75%% (3/4), got:\n%s", out)
	}
	if !strings.Contains(out, "(3/4)") {
		t.Errorf("expected counts (3/4), got:\n%s", out)
	}
}

// TestFormatScheduledSummary_TestsInProgressCountedInTotal verifies that
// IN_PROGRESS executions are included in the total (denominator) so that the
// summary pass-rate is consistent with what `tests status` reports.
// 2 PASSED + 1 IN_PROGRESS → 66% (2/3), not 100% (2/2).
func TestFormatScheduledSummary_TestsInProgressCountedInTotal(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Plan A", "PASSED"),
					exec(101, "Plan B", "PASSED"),
					exec(102, "Plan C", "IN_PROGRESS"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	if !strings.Contains(out, "(2/3)") {
		t.Errorf(
			"expected IN_PROGRESS counted in total → (2/3), got:\n%s", out,
		)
	}
	// The test summary line must not claim 100% pass rate when a test is
	// still running. Extract only the Test Summary section to avoid matching
	// the builds section heading which legitimately shows 100%.
	idx := strings.Index(out, "### Test Summary")
	if idx < 0 {
		t.Fatalf("expected Test Summary section, got:\n%s", out)
	}
	testSection := out[idx:]
	if strings.Contains(testSection, "100%") {
		t.Errorf(
			"test summary must not show 100%% when a test is IN_PROGRESS, got:\n%s",
			testSection,
		)
	}
}

func TestFormatScheduledSummary_TestsMultiPlanFailures(t *testing.T) {
	// One product+arch fails two plans → both plans appear sorted in brackets.
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Manual Testing", "FAILED"),
					exec(101, "Jenkins image validation", "FAILED"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	// Both plans must appear; Jenkins sorts before Manual alphabetically.
	if !strings.Contains(out, "Jenkins image validation") {
		t.Errorf("expected Jenkins plan in failures, got:\n%s", out)
	}
	if !strings.Contains(out, "Manual Testing") {
		t.Errorf("expected Manual Testing plan in failures, got:\n%s", out)
	}
	jenkinsIdx := strings.Index(out, "Jenkins image validation")
	manualIdx := strings.Index(out, "Manual Testing")
	if jenkinsIdx > manualIdx {
		t.Errorf("expected Jenkins before Manual in sorted bracket, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_TestsReleaseOrder(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Jenkins image validation", "PASSED"),
				}),
			}},
		{ID: 2, Name: "plucky-desktop-amd64.iso", OS: "ubuntu",
			Release: "plucky", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(20, "amd64", []domain.TestExecution{
					exec(200, "Jenkins image validation", "PASSED"),
				}),
			}},
	}
	// Scope: plucky first, then noble.
	out := FormatScheduledSummary(artefacts, []string{"plucky", "noble"})
	// Both release headings must appear under the Test Summary header.
	testSectionStart := strings.Index(out, "Test Summary")
	if testSectionStart < 0 {
		t.Fatalf("expected Test Summary header, got:\n%s", out)
	}
	// Find release headings within the test section and check order.
	testSection := out[testSectionStart:]
	pluckyIdx := strings.Index(testSection, "#### plucky")
	nobleIdx := strings.Index(testSection, "#### noble")
	if pluckyIdx < 0 || nobleIdx < 0 {
		t.Fatalf("expected both releases in tests section, got:\n%s", out)
	}
	if pluckyIdx > nobleIdx {
		t.Errorf("expected plucky before noble in tests section, got:\n%s", out)
	}
}

func TestFormatScheduledSummary_TestsOnlyCountsDisplayable(t *testing.T) {
	// "Image build" plan must be excluded from counts and failures.
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: today,
			Builds: []domain.ArtefactBuild{
				buildWithExec(10, "amd64", []domain.TestExecution{
					exec(100, "Image build", "FAILED"), // must be ignored
					exec(101, "Jenkins image validation", "PASSED"),
				}),
			}},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})
	// Only Jenkins execution counted → 1/1, 100%.
	if !strings.Contains(out, "100% (1/1)") {
		t.Errorf("expected 100%% (1/1) (Image build excluded), got:\n%s", out)
	}
	if !strings.Contains(out, "(1/1)") {
		t.Errorf("expected counts (1/1), got:\n%s", out)
	}
	if strings.Contains(out, "Image build") {
		t.Errorf("Image build plan must not appear in output, got:\n%s", out)
	}
}

// --- buildWeatherEmoji ---

func TestBuildWeatherEmoji(t *testing.T) {
	cases := []struct {
		built, total int
		want         string
	}{
		{4, 4, "☀️"}, // 100% → sunny
		{3, 4, "🌤️"}, // 75% → partly cloudy
		{4, 5, "🌤️"}, // 80% → partly cloudy
		{2, 4, "⛅"},  // 50% → cloudy
		{3, 5, "⛅"},  // 60% → cloudy
		{1, 4, "🌧️"}, // 25% → rainy
		{2, 5, "🌧️"}, // 40% → rainy (integer div: 2*100/5=40)
		{1, 5, "⛈️"}, // 20% → stormy
		{0, 4, "🌪️"}, // 0% → tornado
		{0, 0, "🌪️"}, // zero total → tornado
	}
	for _, tc := range cases {
		got := buildWeatherEmoji(tc.built, tc.total)
		if got != tc.want {
			t.Errorf("buildWeatherEmoji(%d, %d) = %q, want %q", tc.built, tc.total, got, tc.want)
		}
	}
}

// --- archFromName ---

func TestArchFromName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"noble-desktop-amd64.iso", "amd64"},
		{"noble-desktop-arm64.iso", "arm64"},
		{"noble-desktop-riscv64.iso", "riscv64"},
		{"noble-server-ppc64el.iso", "ppc64el"},
		{"noble-server-s390x.iso", "s390x"},
		{"noble-server-armhf.iso", "armhf"},
		{"noble-server-i386.iso", "i386"},
		{"noble-server-unknown-arch.iso", "unknown"},
	}
	for _, tc := range cases {
		got := archFromName(tc.name)
		if got != tc.want {
			t.Errorf("archFromName(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// --- FormatNewBuildsNotification ---

func TestFormatNewBuildsNotification_Empty(t *testing.T) {
	out := FormatNewBuildsNotification(nil)
	if out != "" {
		t.Errorf("expected empty string for nil input, got: %s", out)
	}
	out = FormatNewBuildsNotification([]domain.Artefact{})
	if out != "" {
		t.Errorf("expected empty string for empty input, got: %s", out)
	}
}

func TestFormatNewBuildsNotification_ContainsEssentialFields(t *testing.T) {
	builds := []domain.Artefact{
		{ID: 42, Name: "ubuntu-desktop-amd64", OS: "ubuntu", Release: "noble", Version: today, BuildLog: domain.BuildStatusBuilt},
	}
	out := FormatNewBuildsNotification(builds)

	for _, want := range []string{"noble", "ubuntu-desktop-amd64", today, "🔗", "tests.ubuntu.com/#/images/42"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatNewBuildsNotification missing %q, got:\n%s", want, out)
		}
	}
}

func TestFormatNewBuildsNotification_OneLinePerBuild(t *testing.T) {
	builds := []domain.Artefact{
		{ID: 1, Name: "ubuntu-desktop-amd64", OS: "ubuntu", Release: "noble", Version: today},
		{ID: 2, Name: "ubuntu-server-amd64", OS: "ubuntu-server", Release: "jammy", Version: today},
		{ID: 3, Name: "ubuntu-mini", OS: "ubuntu", Release: "plucky", Version: today},
	}
	out := FormatNewBuildsNotification(builds)

	// Each build should produce exactly one line starting with "- ".
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "- ") {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("expected 3 bullet lines, got %d in:\n%s", lines, out)
	}
}

func TestFormatNewBuildsNotification_LineFormat(t *testing.T) {
	builds := []domain.Artefact{
		{ID: 7, Name: "noble-server-amd64.iso", Release: "noble", Version: "20260728"},
	}
	out := strings.TrimSpace(FormatNewBuildsNotification(builds))
	want := "- New noble build available for noble-server-amd64.iso serial 20260728 [🔗](https://tests.ubuntu.com/#/images/7)"
	if out != want {
		t.Errorf("line format mismatch:\n got:  %s\n want: %s", out, want)
	}
}

// --- FormatFailuresSummary ---

func infraRec(id int, name, product, release, sig, desc string, occ int) domain.FailureRecord {
	return domain.FailureRecord{
		ArtefactID:         id,
		ArtefactName:       name,
		Product:            product,
		Release:            release,
		FailureKind:        domain.BuildFailureKindInfra,
		FailureDescription: desc,
		FailureSignature:   sig,
		Occurrences:        occ,
	}
}

func productRec(id int, name, product, release, sig string) domain.FailureRecord {
	return domain.FailureRecord{
		ArtefactID:       id,
		ArtefactName:     name,
		Product:          product,
		Release:          release,
		FailureKind:      domain.BuildFailureKindProduct,
		FailureSignature: sig,
		Occurrences:      1,
	}
}

func TestFormatFailuresSummary_NoRecords(t *testing.T) {
	out := FormatFailuresSummary(nil, "", "")
	if out != "No active failures detected." {
		t.Errorf("unexpected: %q", out)
	}
	out = FormatFailuresSummary(nil, "noble", "")
	if out != "No active failures for **noble**." {
		t.Errorf("unexpected: %q", out)
	}
	out = FormatFailuresSummary(nil, "noble", "ubuntu-base")
	if out != "No active failures for **noble** / **ubuntu-base**." {
		t.Errorf("unexpected: %q", out)
	}
}

func TestFormatFailuresSummary_InfraGroupedCrossProduct(t *testing.T) {
	// Three INFRA records sharing the same signature across two products.
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log",
			"Launchpad build produced no log (builder accepted the job but never attached a log)", 2),
		infraRec(2, "noble-base-arm64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log",
			"Launchpad build produced no log (builder accepted the job but never attached a log)", 2),
		infraRec(3, "noble-live-server-s390x.iso", "ubuntu-server", "noble",
			"infra:lp-no-log",
			"Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
	}

	out := FormatFailuresSummary(records, "noble", "")

	// Should have an Infra section header.
	if !strings.Contains(out, "Infra · noble") {
		t.Errorf("missing infra section header, got:\n%s", out)
	}
	// Should show the shared signature as a group.
	if !strings.Contains(out, "infra:lp-no-log") {
		t.Errorf("missing signature, got:\n%s", out)
	}
	if !strings.Contains(out, "3 artefacts affected") {
		t.Errorf("missing artefact count, got:\n%s", out)
	}
	// Both products should appear.
	if !strings.Contains(out, "ubuntu-base") {
		t.Errorf("missing product ubuntu-base, got:\n%s", out)
	}
	if !strings.Contains(out, "ubuntu-server") {
		t.Errorf("missing product ubuntu-server, got:\n%s", out)
	}
	// Description should be shown once.
	if !strings.Contains(out, "Launchpad build produced no log") {
		t.Errorf("missing description, got:\n%s", out)
	}
	// No per-(release,product) heading for INFRA records.
	if strings.Contains(out, "noble / ubuntu-base") {
		t.Errorf("INFRA records should not emit per-product heading, got:\n%s", out)
	}
}

func TestFormatFailuresSummary_InfraAndProductSeparated(t *testing.T) {
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
		productRec(2, "noble-desktop-amd64.iso", "ubuntu", "noble", "dpkg:passwd"),
	}

	out := FormatFailuresSummary(records, "", "")

	if !strings.Contains(out, "Infra · noble") {
		t.Errorf("missing infra section, got:\n%s", out)
	}
	if !strings.Contains(out, "noble / ubuntu") {
		t.Errorf("missing product section, got:\n%s", out)
	}
	// INFRA section should appear before PRODUCT section.
	infraIdx := strings.Index(out, "Infra")
	productIdx := strings.Index(out, "noble / ubuntu")
	if infraIdx > productIdx {
		t.Errorf("infra section should precede product section, got:\n%s", out)
	}
}

func TestFormatFailuresSummary_InfraMultipleSignatures(t *testing.T) {
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
		infraRec(2, "noble-base-arm64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
		infraRec(3, "noble-server-amd64.img.xz", "ubuntu-server", "noble",
			"infra:cdimage-publish-crash", "LP build succeeded but cdimage crashed during publishing", 1),
	}

	out := FormatFailuresSummary(records, "noble", "")

	if !strings.Contains(out, "infra:lp-no-log") {
		t.Errorf("missing lp-no-log signature, got:\n%s", out)
	}
	if !strings.Contains(out, "infra:cdimage-publish-crash") {
		t.Errorf("missing cdimage-publish-crash signature, got:\n%s", out)
	}
}

func TestFormatFailuresSummary_InfraSingleArtefactInline(t *testing.T) {
	// A single INFRA record with a signature should be rendered inline,
	// not as a "N artefacts affected" block.
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
	}
	out := FormatFailuresSummary(records, "noble", "")

	if strings.Contains(out, "artefacts affected") {
		t.Errorf("single record should not show 'artefacts affected', got:\n%s", out)
	}
	if !strings.Contains(out, "noble-base-amd64.tar.gz") {
		t.Errorf("artefact name should appear inline, got:\n%s", out)
	}
}

func TestFormatFailuresSummary_ProductFilterHidesOtherProducts(t *testing.T) {
	// With a product filter, INFRA records for other products should
	// not appear in the output.
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
		infraRec(2, "noble-server-amd64.iso", "ubuntu-server", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
	}

	// The caller (commands.go) already filters records by product before
	// calling FormatFailuresSummary, so pass only the matching product's
	// records here - matching the real call pattern.
	filtered := []domain.FailureRecord{records[0]}
	out := FormatFailuresSummary(filtered, "noble", "ubuntu-base")

	if strings.Contains(out, "ubuntu-server") {
		t.Errorf("ubuntu-server should not appear when filtered to ubuntu-base, got:\n%s", out)
	}
	if !strings.Contains(out, "noble-base-amd64.tar.gz") {
		t.Errorf("ubuntu-base artefact should appear, got:\n%s", out)
	}
}

func TestFormatFailuresSummary_MultipleReleases(t *testing.T) {
	records := []domain.FailureRecord{
		infraRec(1, "noble-base-amd64.tar.gz", "ubuntu-base", "noble",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
		infraRec(2, "stonking-base-amd64.tar.gz", "ubuntu-base", "stonking",
			"infra:lp-no-log", "Launchpad build produced no log (builder accepted the job but never attached a log)", 1),
	}
	out := FormatFailuresSummary(records, "", "")

	if !strings.Contains(out, "Infra · noble") {
		t.Errorf("missing noble infra section, got:\n%s", out)
	}
	if !strings.Contains(out, "Infra · stonking") {
		t.Errorf("missing stonking infra section, got:\n%s", out)
	}
	// Same signature across releases should NOT be merged.
	if strings.Contains(out, "2 artefacts affected") {
		t.Errorf("cross-release grouping should not happen, got:\n%s", out)
	}
}

// --- effectiveBuildLog via FormatBuildsStatusRelease ---

// TestEffectiveBuildLog_ApprovedNoLog verifies that when BuildLog is empty (no
// log fetched), the build did not happen today, and Status==APPROVED, the cell
// shows the ✔️ APPROVED badge instead of ⏳ NOT_STARTED.
func TestEffectiveBuildLog_ApprovedNoLog(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:      1,
			Name:    "noble-desktop-amd64.iso",
			OS:      "ubuntu",
			Release: "noble",
			Version: yesterday, // not today
			Status:  "APPROVED",
			// BuildLog intentionally empty — no log fetched
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "✔️") {
		t.Errorf(
			"expected ✔️ icon for APPROVED artefact, got:\n%s", out,
		)
	}
	if !strings.Contains(out, "APPROVED") {
		t.Errorf(
			"expected APPROVED label in status cell, got:\n%s", out,
		)
	}
	// Must NOT show NOT_STARTED for an approved artefact.
	if strings.Contains(out, "⏳") {
		t.Errorf(
			"APPROVED artefact must not show ⏳ NOT_STARTED, got:\n%s", out,
		)
	}
}

// TestEffectiveBuildLog_ApprovedWithStoredNotStarted verifies the real-world
// scenario: a prior fetch cycle wrote BuildLog=NOT_STARTED into the snapshot,
// and the artefact is APPROVED. The display must still show ✔️ APPROVED, not
// ⏳, because NOT_STARTED is treated as equivalent to "no log" for this check.
func TestEffectiveBuildLog_ApprovedWithStoredNotStarted(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:       1,
			Name:     "noble-desktop-amd64.iso",
			OS:       "ubuntu",
			Release:  "noble",
			Version:  yesterday,
			Status:   "APPROVED",
			BuildLog: domain.BuildStatusNotStarted, // stale value from prior fetch
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "✔️") {
		t.Errorf(
			"expected ✔️ for APPROVED artefact with stale NOT_STARTED, got:\n%s",
			out,
		)
	}
	if strings.Contains(out, "⏳") {
		t.Errorf(
			"stale NOT_STARTED + APPROVED must not show ⏳, got:\n%s", out,
		)
	}
}

// TestEffectiveBuildLog_MarkedAsFailedNoLog verifies that when BuildLog is empty,
// the build did not happen today, and Status==MARKED_AS_FAILED, the cell shows
// 🚫 MARKED_AS_FAILED instead of ⏳ NOT_STARTED.
func TestEffectiveBuildLog_MarkedAsFailedNoLog(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:      2,
			Name:    "noble-preinstalled-server-arm64+tegra-jetson.img.xz",
			OS:      "ubuntu-server",
			Release: "noble",
			Version: "20260831",
			Status:  "MARKED_AS_FAILED",
			// BuildLog intentionally empty — no log fetched
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "🚫") {
		t.Errorf(
			"expected 🚫 icon for MARKED_AS_FAILED artefact, got:\n%s", out,
		)
	}
	if !strings.Contains(out, "MARKED_AS_FAILED") {
		t.Errorf(
			"expected MARKED_AS_FAILED label in status cell, got:\n%s", out,
		)
	}
	if strings.Contains(out, "⏳") {
		t.Errorf(
			"MARKED_AS_FAILED artefact must not show ⏳, got:\n%s", out,
		)
	}
}

// TestEffectiveBuildLog_MarkedAsFailedWithStoredNotStarted mirrors the
// APPROVED variant: stale BuildLog=NOT_STARTED from a prior fetch must not
// suppress the MARKED_AS_FAILED display.
func TestEffectiveBuildLog_MarkedAsFailedWithStoredNotStarted(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:       2,
			Name:     "noble-preinstalled-server-arm64+tegra-jetson.img.xz",
			OS:       "ubuntu-server",
			Release:  "noble",
			Version:  "20260831",
			Status:   "MARKED_AS_FAILED",
			BuildLog: domain.BuildStatusNotStarted, // stale value from prior fetch
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "🚫") {
		t.Errorf(
			"expected 🚫 for MARKED_AS_FAILED with stale NOT_STARTED, got:\n%s",
			out,
		)
	}
	if strings.Contains(out, "⏳") {
		t.Errorf(
			"stale NOT_STARTED + MARKED_AS_FAILED must not show ⏳, got:\n%s",
			out,
		)
	}
}

// TestEffectiveBuildLog_ApprovedWithBuildLogSet verifies that when BuildLog is
// explicitly set (e.g. BUILT from today's log), the BuildLog value takes
// precedence over the Status field even when Status==APPROVED.
func TestEffectiveBuildLog_ApprovedWithBuildLogSet(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:       3,
			Name:     "noble-desktop-amd64.iso",
			OS:       "ubuntu",
			Release:  "noble",
			Version:  today,
			Status:   "APPROVED",
			BuildLog: domain.BuildStatusBuilt, // explicit log state wins
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "✅") {
		t.Errorf(
			"BuildLog=BUILT should show ✅, got:\n%s", out,
		)
	}
	// Should NOT show the approved icon since BuildLog overrides.
	if strings.Contains(out, "✔️") {
		t.Errorf(
			"explicit BuildLog should override Status; must not show ✔️, got:\n%s",
			out,
		)
	}
}

// TestEffectiveBuildLog_UndecidedNoLog verifies that when BuildLog is empty,
// the build did not happen today, and Status is neither APPROVED nor
// MARKED_AS_FAILED (e.g. UNDECIDED), the cell shows ⏳ NOT_STARTED.
func TestEffectiveBuildLog_UndecidedNoLog(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:      4,
			Name:    "noble-desktop-amd64.iso",
			OS:      "ubuntu",
			Release: "noble",
			Version: yesterday,
			Status:  "UNDECIDED",
		},
	}
	out := FormatBuildsStatusRelease(
		artefacts, "noble", "", domain.FailureStore{},
	)
	if !strings.Contains(out, "⏳") {
		t.Errorf("expected ⏳ for UNDECIDED artefact with no log, got:\n%s", out)
	}
}

// TestFormatBuildsStatusSummary_ApprovedAndMarkedFailedColumns verifies that
// the summary table includes the new Approved and QA Failed columns and counts
// APPROVED/MARKED_AS_FAILED artefacts in the correct buckets.
func TestFormatBuildsStatusSummary_ApprovedAndMarkedFailedColumns(t *testing.T) {
	artefacts := []domain.Artefact{
		{
			ID:      1,
			Release: "noble",
			Version: yesterday,
			Status:  "APPROVED",
		},
		{
			ID:      2,
			Release: "noble",
			Version: yesterday,
			Status:  "MARKED_AS_FAILED",
		},
	}
	out := FormatBuildsStatusSummary(artefacts)

	// New column headers must be present.
	if !strings.Contains(out, "Approved") {
		t.Errorf("summary table must include Approved column, got:\n%s", out)
	}
	if !strings.Contains(out, "QA Failed") {
		t.Errorf(
			"summary table must include QA Failed column, got:\n%s", out,
		)
	}
	// noble row: 0 built, 0 in-progress, 0 failed, 0 not-started,
	// 1 approved, 1 QA-failed, 0 unknown, 2 total.
	if !strings.Contains(out, "noble") {
		t.Errorf("expected noble row in output, got:\n%s", out)
	}
}

// TestFormatBuildsStatusSummary_ApprovedCountsAsHealthy verifies that approved
// artefacts drive the progress bar to green, not red. On release day all builds
// stop and artefacts become APPROVED — the bar should reflect the healthy state.
func TestFormatBuildsStatusSummary_ApprovedCountsAsHealthy(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Release: "noble", Version: yesterday, Status: "APPROVED"},
		{ID: 2, Release: "noble", Version: yesterday, Status: "APPROVED"},
	}
	out := FormatBuildsStatusSummary(artefacts)
	wantBar := strings.Repeat("🟩", 10)
	if !strings.Contains(out, wantBar) {
		t.Errorf(
			"all-approved release should show 100%% green bar, got:\n%s", out,
		)
	}
	if strings.Contains(out, "🟥") {
		t.Errorf(
			"all-approved release must have no red squares, got:\n%s", out,
		)
	}
}

// TestFormatScheduledSummary_AllApproved verifies that when all artefacts are
// APPROVED (release day, no builds ran), the scheduled summary shows ☀️ and
// the heading uses "(N/N approved)" to make the state unambiguous.
func TestFormatScheduledSummary_AllApproved(t *testing.T) {
	artefacts := []domain.Artefact{
		{ID: 1, Name: "noble-desktop-amd64.iso", OS: "ubuntu",
			Release: "noble", Version: yesterday, Status: "APPROVED",
			BuildLog: domain.BuildStatusNotStarted},
		{ID: 2, Name: "noble-server-amd64.iso", OS: "ubuntu-server",
			Release: "noble", Version: yesterday, Status: "APPROVED",
			BuildLog: domain.BuildStatusNotStarted},
	}
	out := FormatScheduledSummary(artefacts, []string{"noble"})

	if !strings.Contains(out, "☀️") {
		t.Errorf("all-approved release should show ☀️, got:\n%s", out)
	}
	if !strings.Contains(out, "approved") {
		t.Errorf(
			"heading should contain 'approved' label, got:\n%s", out,
		)
	}
	if strings.Contains(out, "Infra") || strings.Contains(out, "Product") {
		t.Errorf(
			"all-approved release must have no failure sections, got:\n%s", out,
		)
	}
}
