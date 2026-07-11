import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { BookView } from './lib/types';

vi.mock('../wailsjs/go/main/App', () => ({
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfigStatus: vi.fn(),
  ConfirmApply: vi.fn(),
}));

import App from './App.svelte';
import { Scan, Apply, ConfigStatus, ConfirmApply } from '../wailsjs/go/main/App';

function makeBook(overrides: Partial<BookView> = {}): BookView {
  return {
    id: '/inbox/book.epub',
    sourcePath: '/inbox/book.epub',
    oldFilename: 'book.epub',
    format: 'epub',
    sizeBytes: 1024,
    subject: '',
    title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    author: { value: 'Bruce Eckel', source: 'Heuristic' },
    year: { value: '2021', source: 'Heuristic' },
    status: 'Heuristic',
    category: 'Uncategorized',
    subcategory: '',
    categoryWarning: '',
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}

describe('App', () => {
  beforeEach(() => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '', error: '' });
    vi.mocked(ConfirmApply).mockResolvedValue(true);
    vi.mocked(Apply).mockResolvedValue({ batchId: 'b1', results: [] });
  });

  it('Apply only moves the currently visible (filtered) books, not the whole scan', async () => {
    const visibleBook = makeBook({
      id: '/inbox/atomic-kotlin.epub',
      sourcePath: '/inbox/atomic-kotlin.epub',
      title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    });
    const hiddenBook = makeBook({
      id: '/inbox/other-book.epub',
      sourcePath: '/inbox/other-book.epub',
      oldFilename: 'other-book.epub',
      title: { value: 'Some Other Book', source: 'Heuristic' },
    });
    vi.mocked(Scan).mockResolvedValue([visibleBook, hiddenBook]);

    render(App);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByText('other-book.epub')).toBeInTheDocument();
    });

    // Narrow the visible set down to just `visibleBook` via the search box.
    const search = screen.getByPlaceholderText('Search title, author, or filename…');
    await fireEvent.input(search, { target: { value: 'Atomic' } });

    await waitFor(() => {
      expect(screen.queryByText('other-book.epub')).not.toBeInTheDocument();
    });
    expect(screen.getByText('book.epub', { exact: false })).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });

    // Confirmation dialog must be sized to the visible set (1), not the full scan (2).
    expect(ConfirmApply).toHaveBeenCalledWith(1, '');

    // Apply must only receive the visible book, never the filtered-out one.
    const appliedBooks = vi.mocked(Apply).mock.calls[0][0];
    expect(appliedBooks).toHaveLength(1);
    expect(appliedBooks[0].sourcePath).toBe('/inbox/atomic-kotlin.epub');
  });
});
