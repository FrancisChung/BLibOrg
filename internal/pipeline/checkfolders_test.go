package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func checkFoldersTestConfig(working, library, log string) config.Config {
	return config.Config{General: config.General{
		WorkingFolder: working,
		LibraryFolder: library,
		LogFolder:     log,
	}}
}

func TestCheckFolders_AllValidCreatesMissingDestinationFolders(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "inbox")
	if err := os.Mkdir(working, 0755); err != nil {
		t.Fatalf("mkdir working: %v", err)
	}
	library := filepath.Join(root, "library", "nested")
	logFolder := filepath.Join(root, "logs", "nested")

	if err := CheckFolders(checkFoldersTestConfig(working, library, logFolder)); err != nil {
		t.Fatalf("CheckFolders error: %v", err)
	}

	for _, dir := range []string{library, logFolder} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("expected %s to be created: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", dir)
		}
	}
}

func TestCheckFolders_WorkingFolderMissing(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "does-not-exist")

	err := CheckFolders(checkFoldersTestConfig(working, filepath.Join(root, "library"), filepath.Join(root, "logs")))
	if err == nil {
		t.Fatal("expected an error when working_folder does not exist")
	}
}

func TestCheckFolders_WorkingFolderIsAFile(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "inbox-is-a-file")
	if err := os.WriteFile(working, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := CheckFolders(checkFoldersTestConfig(working, filepath.Join(root, "library"), filepath.Join(root, "logs")))
	if err == nil {
		t.Fatal("expected an error when working_folder is a file, not a directory")
	}
}

func TestCheckFolders_LibraryFolderUnset(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "inbox")
	if err := os.Mkdir(working, 0755); err != nil {
		t.Fatalf("mkdir working: %v", err)
	}

	err := CheckFolders(checkFoldersTestConfig(working, "", filepath.Join(root, "logs")))
	if err == nil {
		t.Fatal("expected an error when library_folder is unset")
	}
}

func TestCheckFolders_LogFolderUnset(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "inbox")
	if err := os.Mkdir(working, 0755); err != nil {
		t.Fatalf("mkdir working: %v", err)
	}

	err := CheckFolders(checkFoldersTestConfig(working, filepath.Join(root, "library"), ""))
	if err == nil {
		t.Fatal("expected an error when log_folder is unset")
	}
}

func TestCheckFolders_LibraryFolderUncreatable(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "inbox")
	if err := os.Mkdir(working, 0755); err != nil {
		t.Fatalf("mkdir working: %v", err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	library := filepath.Join(blocker, "library") // blocker is a file, not a dir

	err := CheckFolders(checkFoldersTestConfig(working, library, filepath.Join(root, "logs")))
	if err == nil {
		t.Fatal("expected an error when library_folder cannot be created")
	}
}
