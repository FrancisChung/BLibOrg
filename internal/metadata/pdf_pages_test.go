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

func TestWalkPDFPageTree_ResolvesIndirectKidsReference(t *testing.T) {
	// Reproduces the real "Designing for AI"/"Grokking Software
	// Architecture"/"Grokking Streaming Systems" bug: /Kids is an
	// indirect reference to a separate object holding the array ("/Kids
	// 6 0 R"), not inlined ("/Kids [...]") -- pdfKidsRe alone never
	// matches this shape, which previously made the whole page-tree walk
	// fail (ok=false), forcing cover selection all the way back to
	// findPDFCover's unbounded whole-file scan.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids 6 0 R /Count 2 >>\nendobj\n" +
			"6 0 obj\n[3 0 R 4 0 R]\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok, want true (indirect /Kids reference should resolve)")
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

func TestWalkPDFPageTree_CycleProtection(t *testing.T) {
	// Fixture: self-referential Pages node (object 2 includes itself in Kids).
	// This tests cycle-protection: the visited map should prevent infinite recursion.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [2 0 R] >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	// Pure cycle with no real /Type /Page leaves should return no pages and ok=false.
	if ok {
		t.Error("walkPDFPageTree ok = true, want false (cycle with no Page leaves)")
	}
	if len(pages) != 0 {
		t.Fatalf("len(pages) = %d, want 0 (pure cycle contains no Page leaves)", len(pages))
	}
	// Test completing without hanging is proof cycle-protection worked.
}

func TestWalkPDFPageTree_CycleWithRealPage(t *testing.T) {
	// Variant: cycle alongside a reachable real Page leaf.
	// Verifies the walk finds real pages despite the cycle elsewhere in the tree.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Pages /Parent 2 0 R /Kids [4 0 R] >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	// Should find the one real Page leaf (object 3) and handle the self-referential cycle in 4.
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
}
