import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { BookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  Categories: vi.fn(),
}));

import ScanReviewView from './ScanReviewView.svelte';
import { Scan, Apply, ConfirmApply, Categories } from '../../wailsjs/go/main/App';

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
    categoryManual: false,
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}

describe('ScanReviewView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(ConfirmApply).mockResolvedValue(true);
    vi.mocked(Apply).mockResolvedValue({ batchId: 'b1', results: [] });
    vi.mocked(Categories).mockResolvedValue([]);
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

  it('defaults every scanned book to checked and includes all of them in Apply', async () => {
    vi.mocked(Scan).mockResolvedValue([makeBook()]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).toBeChecked();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(Apply).mock.calls[0][0]).toHaveLength(1);
  });

  it('unchecking a book excludes it from the Apply payload', async () => {
    const bookA = makeBook({ id: '/inbox/a.epub', sourcePath: '/inbox/a.epub', oldFilename: 'a.epub' });
    const bookB = makeBook({ id: '/inbox/b.epub', sourcePath: '/inbox/b.epub', oldFilename: 'b.epub' });
    vi.mocked(Scan).mockResolvedValue([bookA, bookB]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.epub' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });
    const applied = vi.mocked(Apply).mock.calls[0][0];
    expect(applied).toHaveLength(1);
    expect(applied[0].sourcePath).toBe('/inbox/b.epub');
  });

  it('select-all only affects currently visible (filtered) books', async () => {
    const bookA = makeBook({
      id: '/inbox/a.epub',
      sourcePath: '/inbox/a.epub',
      oldFilename: 'a.epub',
      title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    });
    const bookB = makeBook({
      id: '/inbox/b.epub',
      sourcePath: '/inbox/b.epub',
      oldFilename: 'b.epub',
      title: { value: 'Some Other Book', source: 'Heuristic' },
    });
    vi.mocked(Scan).mockResolvedValue([bookA, bookB]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).toBeInTheDocument();
    });

    const search = screen.getByPlaceholderText('Search title, author, or filename…');
    await fireEvent.input(search, { target: { value: 'Atomic' } });
    await waitFor(() => {
      expect(screen.queryByRole('checkbox', { name: 'Select b.epub' })).not.toBeInTheDocument();
    });

    // Only "a" is visible now; unchecking select-all must only affect "a".
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }));

    await fireEvent.input(search, { target: { value: '' } });
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select b.epub' })).toBeInTheDocument();
    });

    expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Select b.epub' })).toBeChecked();
  });

  it('the Uncategorized filter chip shows only books without a category', async () => {
    const categorized = makeBook({
      id: '/inbox/a.epub',
      sourcePath: '/inbox/a.epub',
      oldFilename: 'a.epub',
      category: 'Technology',
    });
    const uncategorized = makeBook({
      id: '/inbox/b.epub',
      sourcePath: '/inbox/b.epub',
      oldFilename: 'b.epub',
      category: 'Uncategorized',
    });
    vi.mocked(Scan).mockResolvedValue([categorized, uncategorized]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByText('a.epub')).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Uncategorized' }));

    await waitFor(() => {
      expect(screen.queryByText('a.epub')).not.toBeInTheDocument();
    });
    expect(screen.getByText('b.epub')).toBeInTheDocument();
  });

  it('tints a book card once it has been successfully moved by Apply', async () => {
    const book = makeBook();
    vi.mocked(Scan).mockResolvedValue([book]);
    vi.mocked(Apply).mockResolvedValue({
      batchId: 'b1',
      results: [{ sourcePath: book.sourcePath, ok: true, error: '', skipped: false }],
    });
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Apply' })).not.toBeDisabled();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(screen.getByText('Moved ✓')).toBeInTheDocument();
    });
    expect(screen.getByText('book.epub').closest('.card')?.className).toContain('moved');
  });

  it('does not tint a book card that Apply skipped', async () => {
    const book = makeBook();
    vi.mocked(Scan).mockResolvedValue([book]);
    vi.mocked(Apply).mockResolvedValue({
      batchId: 'b1',
      results: [{ sourcePath: book.sourcePath, ok: true, error: '', skipped: true }],
    });
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Apply' })).not.toBeDisabled();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(screen.getByText('Skipped')).toBeInTheDocument();
    });
    expect(screen.getByText('book.epub').closest('.card')?.className).not.toContain('moved');
  });

  it('shows an empty-state message when Scan finds no books', async () => {
    vi.mocked(Scan).mockResolvedValue([]);
    render(ScanReviewView);

    expect(screen.queryByText('No books found in the unsorted folder.')).not.toBeInTheDocument();

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));

    await waitFor(() => {
      expect(screen.getByText('No books found in the unsorted folder.')).toBeInTheDocument();
    });
  });

  it('fetches destinations on mount and shows them in every book\'s picker', async () => {
    vi.mocked(Categories).mockResolvedValue([{ category: 'Fiction', subcategory: 'Sci-Fi' }]);
    vi.mocked(Scan).mockResolvedValue([makeBook({ category: 'Uncategorized' })]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));

    await waitFor(() => {
      expect(screen.getByText('Fiction / Sci-Fi')).toBeInTheDocument();
    });
  });
});
