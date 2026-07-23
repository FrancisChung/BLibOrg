package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func writePDFOverrideFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func twoPageTwoImageFixture() []byte {
	jpeg1 := []byte("\xFF\xD8\xFFpage1jpeg")
	jpeg2 := []byte("\xFF\xD8\xFFpage2jpeg")
	return []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 6 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg1) + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg2) + "\nendstream\nendobj\n")
}

func TestListPDFCoverCandidates_ReturnsOneCandidatePerPage(t *testing.T) {
	path := writePDFOverrideFixture(t, twoPageTwoImageFixture())

	candidates, err := ListPDFCoverCandidates(path, 10)
	if err != nil {
		t.Fatalf("ListPDFCoverCandidates returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].Page != 1 || candidates[1].Page != 2 {
		t.Errorf("pages = %d, %d, want 1, 2", candidates[0].Page, candidates[1].Page)
	}
	if candidates[0].ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", candidates[0].ContentType)
	}
}

func TestListPDFCoverCandidates_UnresolvablePageTreeReturnsEmptyNotError(t *testing.T) {
	path := writePDFOverrideFixture(t, []byte("not a real pdf"))
	candidates, err := ListPDFCoverCandidates(path, 10)
	if err != nil {
		t.Fatalf("ListPDFCoverCandidates returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("len(candidates) = %d, want 0", len(candidates))
	}
}

func TestListPDFCoverCandidates_NonExistentFileReturnsError(t *testing.T) {
	if _, err := ListPDFCoverCandidates(filepath.Join(t.TempDir(), "missing.pdf"), 10); err == nil {
		t.Error("expected an error for a nonexistent file, got nil")
	}
}

func TestExtractPDFPageCover_ReturnsExactPageImage(t *testing.T) {
	path := writePDFOverrideFixture(t, twoPageTwoImageFixture())

	data, contentType, ok, err := ExtractPDFPageCover(path, 2)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if string(data) != "\xFF\xD8\xFFpage2jpeg" {
		t.Errorf("data = %q, want page 2's image", data)
	}
	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want image/jpeg", contentType)
	}
}

func TestExtractPDFPageCover_OutOfRangePageNotOK(t *testing.T) {
	path := writePDFOverrideFixture(t, twoPageTwoImageFixture())
	_, _, ok, err := ExtractPDFPageCover(path, 99)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (page 99 doesn't exist)")
	}
}

func TestExtractPDFPageCover_PageWithNoImageNotOK(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n")
	path := writePDFOverrideFixture(t, data)

	_, _, ok, err := ExtractPDFPageCover(path, 1)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (page 1 has no image)")
	}
}
