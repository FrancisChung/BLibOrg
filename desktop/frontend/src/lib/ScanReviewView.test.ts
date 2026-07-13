import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { BookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
}));

import ScanReviewView from './ScanReviewView.svelte';
import { Scan, Apply, ConfirmApply } from '../../wailsjs/go/main/App';

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

describe('ScanReviewView', () => {
  beforeEach(() => {
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

    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByText('other-book.epub')).toBeInTheDocument();
    });

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

    expect(ConfirmApply).toHaveBeenCalledWith(1, '');

    const appliedBooks = vi.mocked(Apply).mock.calls[0][0];
    expect(appliedBooks).toHaveLength(1);
    expect(appliedBooks[0].sourcePath).toBe('/inbox/atomic-kotlin.epub');
  });

  it('shows an error banner when Apply rejects, instead of failing silently', async () => {
    vi.mocked(Scan).mockResolvedValue([makeBook()]);
    vi.mocked(Apply).mockRejectedValue(new Error('boom'));

    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Apply' })).not.toBeDisabled();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
