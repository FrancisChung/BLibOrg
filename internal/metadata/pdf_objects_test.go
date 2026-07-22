// internal/metadata/pdf_objects_test.go
package metadata

import (
	"bytes"
	"testing"
)

func TestBuildPDFObjIndex_LiteralLookup(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("/Type /Catalog")) {
		t.Errorf("lookup(1) = %q, want it to contain /Type /Catalog", body)
	}

	if _, ok := idx.lookup(99); ok {
		t.Error("lookup(99) found, want not found")
	}
}

func TestBuildPDFObjIndex_LastIncrementalUpdateWins(t *testing.T) {
	data := []byte("1 0 obj\n<< /Title (Old) >>\nendobj\n" +
		"1 0 obj\n<< /Title (New) >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("(New)")) {
		t.Errorf("lookup(1) = %q, want it to contain the later revision's (New)", body)
	}
}
