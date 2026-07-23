package appapi

import (
	"sort"

	"github.com/FrancisChung/book-organiser/internal/librarian"
)

// LibraryBookView is the JSON view of a librarian.Book -- one
// already-organized book shown in the frontend's Library/Bookshelf view.
type LibraryBookView struct {
	SourcePath      string `json:"sourcePath"`
	Format          string `json:"format"`
	Title           string `json:"title"`
	Author          string `json:"author"`
	Year            string `json:"year"`
	Category        string `json:"category"`
	Subcategory     string `json:"subcategory"`
	CoverPath       string `json:"coverPath"`
	CoverOverridden bool   `json:"coverOverridden"`
}

// LibraryView is the full result of ListLibrary: every already-organized
// book, plus the sorted list of distinct category names actually present
// (used for the frontend's Library submenu; unlike Categories(), this
// includes "Uncategorized" if any organized book genuinely lives there).
type LibraryView struct {
	Books      []LibraryBookView `json:"books"`
	Categories []string          `json:"categories"`
}

func libraryBookToView(b librarian.Book) LibraryBookView {
	return LibraryBookView{
		SourcePath:      b.SourcePath,
		Format:          b.Format,
		Title:           b.Title,
		Author:          b.Author,
		Year:            b.Year,
		Category:        b.Category,
		Subcategory:     b.Subcategory,
		CoverPath:       b.CoverPath,
		CoverOverridden: b.CoverOverridden,
	}
}

// ListLibrary walks the configured library folder and returns every
// already-organized book found there, for the frontend's Library/Bookshelf
// view. It never touches the filesystem beyond reading -- no moves, no
// categorization, no destination-path computation (those are Scan/Apply's
// job for the *working* folder; this reads back what's already organized).
func (a *App) ListLibrary() (LibraryView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return LibraryView{}, err
	}

	books, err := librarian.Scan(cfg)
	if err != nil {
		return LibraryView{}, err
	}

	views := make([]LibraryBookView, 0, len(books))
	categorySet := map[string]bool{}
	for _, b := range books {
		views = append(views, libraryBookToView(b))
		if b.Category != "" {
			categorySet[b.Category] = true
		}
	}
	categories := make([]string, 0, len(categorySet))
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	return LibraryView{Books: views, Categories: categories}, nil
}
