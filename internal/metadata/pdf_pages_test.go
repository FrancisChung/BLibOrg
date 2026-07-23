// internal/metadata/pdf_pages_test.go
package metadata

import "testing"

func pagesTreeFixture() []byte {
	return []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"5 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
}

func TestWalkPDFPageTree_OrderedByKids(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 3 {
		t.Fatalf("len(pages) = %d, want 3", len(pages))
	}
	for i, p := range pages {
		if p.number != i+1 {
			t.Errorf("pages[%d].number = %d, want %d", i, p.number, i+1)
		}
	}
}

func TestWalkPDFPageTree_RespectsLimit(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 2)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2 (limit)", len(pages))
	}
}

func TestWalkPDFPageTree_ZeroLimitUsesDefault(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 0)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 3 {
		t.Fatalf("len(pages) = %d, want 3 (fixture has fewer pages than the default limit)", len(pages))
	}
}

func TestWalkPDFPageTree_NestedKids(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [6 0 R 4 0 R] >>\nendobj\n" +
			"6 0 obj\n<< /Type /Pages /Parent 2 0 R /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 6 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}
}

func TestWalkPDFPageTree_NoCatalogReturnsNotOK(t *testing.T) {
	idx := buildPDFObjIndex([]byte("1 0 obj\n<< /Title (No page tree here) >>\nendobj\n"))
	if _, ok := walkPDFPageTree(idx, 10); ok {
		t.Error("walkPDFPageTree ok = true, want false (no /Type /Catalog present)")
	}
}
