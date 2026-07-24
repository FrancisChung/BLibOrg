export interface FieldView {
  value: string;
  source: string;
}

export interface BookView {
  id: string;
  sourcePath: string;
  oldFilename: string;
  format: string;
  sizeBytes: number;
  subject: string;
  title: FieldView;
  author: FieldView;
  year: FieldView;
  status: string;
  category: string;
  subcategory: string;
  categoryWarning: string;
  categoryManual: boolean;
  destPath: string;
  duplicateStatus: string;
  duplicateGroupId: string;
}

export interface DestinationView {
  category: string;
  subcategory: string;
}

export interface ApplyResultEntry {
  sourcePath: string;
  ok: boolean;
  error: string;
  skipped: boolean;
}

export interface ApplyResult {
  batchId: string;
  results: ApplyResultEntry[];
}

export type StatusFilter = 'all' | 'Partial' | 'duplicates' | 'Heuristic' | 'Metadata' | 'uncategorized';

export const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'Partial', label: 'Needs review' },
  { key: 'duplicates', label: 'Duplicates' },
  { key: 'uncategorized', label: 'Uncategorized' },
  { key: 'Heuristic', label: 'Heuristic' },
  { key: 'Metadata', label: 'Metadata' },
];

export function matchesFilter(b: BookView, filter: StatusFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'duplicates') return b.duplicateStatus !== 'NotDuplicate';
  if (filter === 'uncategorized') return b.category === 'Uncategorized';
  return b.status === filter;
}

export function matchesQuery(b: BookView, query: string): boolean {
  if (!query.trim()) return true;
  const q = query.toLowerCase();
  return (
    b.title.value.toLowerCase().includes(q) ||
    b.author.value.toLowerCase().includes(q) ||
    b.oldFilename.toLowerCase().includes(q)
  );
}

export interface OperationEntryView {
  oldPath: string;
  newPath: string;
  opType: string;
  undone: boolean;
}

export interface OperationBatchView {
  batchId: string;
  timestamp: string;
  entryCount: number;
  undoneCount: number;
  entries: OperationEntryView[];
}

export interface CategoryWarningView {
  timestamp: string;
  sourcePath: string;
  category: string;
  subcategory: string;
  warning: string;
}

export interface LibraryBookView {
  sourcePath: string;
  format: string;
  title: string;
  author: string;
  year: string;
  category: string;
  subcategory: string;
  coverPath: string;
  coverOverridden: boolean;
}

export interface CoverCandidateView {
  page: number;
  thumbnailUrl: string;
}

export type LibrarySortMode = 'title' | 'author' | 'year';

export interface LibraryShelf {
  subcategory: string;
  books: LibraryBookView[];
}

// groupIntoShelves filters books to category (or keeps all when category is
// ""), groups the result into one shelf per subcategory (subcategories
// sorted alphabetically; an empty subcategory groups under
// "(No subcategory)"), and sorts each shelf's books by sortMode -- the
// single global sort control applies to every shelf at once, per the design
// spec.
export function groupIntoShelves(
  books: LibraryBookView[],
  category: string,
  sortMode: LibrarySortMode,
): LibraryShelf[] {
  const filtered = category ? books.filter((b) => b.category === category) : books;

  const bySubcategory = new Map<string, LibraryBookView[]>();
  for (const b of filtered) {
    const key = b.subcategory || '(No subcategory)';
    const list = bySubcategory.get(key) ?? [];
    list.push(b);
    bySubcategory.set(key, list);
  }

  const compare = (a: LibraryBookView, b: LibraryBookView): number => {
    if (sortMode === 'year') return a.year.localeCompare(b.year);
    if (sortMode === 'author') return a.author.localeCompare(b.author);
    return a.title.localeCompare(b.title);
  };

  return Array.from(bySubcategory.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([subcategory, shelfBooks]) => ({
      subcategory,
      books: [...shelfBooks].sort(compare),
    }));
}

export type SidebarView = 'scan' | 'library' | 'operations' | 'warnings' | 'settings';
