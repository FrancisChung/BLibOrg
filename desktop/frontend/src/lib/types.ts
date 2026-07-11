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
  destPath: string;
  duplicateStatus: string;
  duplicateGroupId: string;
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

export type StatusFilter = 'all' | 'Partial' | 'duplicates' | 'Heuristic' | 'Metadata';

export const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'Partial', label: 'Needs review' },
  { key: 'duplicates', label: 'Duplicates' },
  { key: 'Heuristic', label: 'Heuristic' },
  { key: 'Metadata', label: 'Metadata' },
];

export function matchesFilter(b: BookView, filter: StatusFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'duplicates') return b.duplicateStatus !== 'NotDuplicate';
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
