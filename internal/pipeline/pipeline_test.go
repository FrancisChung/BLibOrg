package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/operations"
)

func baseConfig(workingFolder, libraryFolder string) config.Config {
	return config.Config{
		General: config.General{
			WorkingFolder:  workingFolder,
			LibraryFolder:  libraryFolder,
			FilenameFormat: "{title} ({year}) - {author}",
			Fallbacks:      config.Fallbacks{Year: "Unknown", Author: "Unknown Author"},
		},
		Heuristics: config.Heuristics{KnownJunkTags: []string{"OceanofPDF.com", "libgen.li"}},
		Categories: map[string]config.Category{"Uncategorized": {}},
	}
}

func TestRun_FallsBackToHeuristicsForFileWithNoMetadata(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	// A plain-text stand-in for a PDF with no parseable Info dict: the PDF
	// extractor will find no metadata, so the pipeline must fall back to
	// filename heuristics for every field.
	srcPath := filepath.Join(workDir, "Build+Your+API+with+Spring.pdf")
	if err := os.WriteFile(srcPath, []byte("no pdf metadata here"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("Run returned %d books, want 1", len(books))
	}
	b := books[0]
	if b.Title.Value != "Build Your API with Spring" {
		t.Errorf("Title = %q, want %q", b.Title.Value, "Build Your API with Spring")
	}
	if b.Title.Source != book.SourceHeuristic {
		t.Errorf("Title.Source = %v, want SourceHeuristic", b.Title.Source)
	}
	if b.Status() != book.SourceUnresolved {
		t.Errorf("Status() = %v, want SourceUnresolved (no author/year found anywhere)", b.Status())
	}
	if b.Category != "Uncategorized" {
		t.Errorf("Category = %q, want Uncategorized", b.Category)
	}
	if b.DestPath == "" {
		t.Error("expected a DestPath to be computed even for an Unresolved row")
	}
}

func TestRun_FlagsDuplicatesAcrossScannedFiles(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov.epub"), []byte("aaaaaaaaaa"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov (copy).epub"), []byte("aaaaaaaaaa"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("Run returned %d books, want 2", len(books))
	}
	if books[0].DuplicateGroupID == "" || books[0].DuplicateGroupID != books[1].DuplicateGroupID {
		t.Errorf("expected both books grouped as duplicates, got %q and %q", books[0].DuplicateGroupID, books[1].DuplicateGroupID)
	}
}

// TestRun_DisambiguatesCollidingDestPaths reproduces the Finding-2 review
// scenario: two distinct books whose computed DestPath renders identically
// (here, bracketed "[signed edition]" noise is stripped by filename
// heuristics, so both files parse to the same Title/Author and land in the
// same Category with the same extension). Without disambiguation, applying
// both moves via operations.Manager would have the second move refuse (per
// the Finding-1 fix) or, pre-fix, silently clobber the first. Run must
// instead give every returned book a unique DestPath.
func TestRun_DisambiguatesCollidingDestPaths(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov.epub"), []byte("aaaaaaaaaa"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov [signed edition].epub"), []byte("bbbbbbbbbbbbbbbb"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("Run returned %d books, want 2", len(books))
	}
	if books[0].DestPath == "" || books[1].DestPath == "" {
		t.Fatal("expected both books to have a non-empty DestPath")
	}
	if books[0].DestPath == books[1].DestPath {
		t.Errorf("expected distinct DestPath values after disambiguation, both got %q", books[0].DestPath)
	}
}

func TestRun_ApplyAndUndoEndToEnd(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	srcPath := filepath.Join(workDir, "Build+Your+API+with+Spring.pdf")
	if err := os.WriteFile(srcPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("Run returned %d books, want 1", len(books))
	}
	b := books[0]

	logPath := filepath.Join(t.TempDir(), "ops.jsonl")
	mgr := operations.NewManager(operations.NewLog(logPath))
	cmd := operations.NewMoveCommand("batch-1", b.SourcePath, b.DestPath)

	if err := mgr.ExecuteBatch("batch-1", []operations.Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if _, err := os.Stat(b.DestPath); err != nil {
		t.Fatalf("expected file at DestPath after Apply: %v", err)
	}

	if err := mgr.UndoBatch("batch-1"); err != nil {
		t.Fatalf("UndoBatch error: %v", err)
	}
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("expected file restored to original location after Undo: %v", err)
	}
}
