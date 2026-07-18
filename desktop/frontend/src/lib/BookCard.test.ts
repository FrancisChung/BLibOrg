import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import BookCard from './BookCard.svelte';
import type { BookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

import { OpenFile } from '../../wailsjs/go/main/App';

function makeBook(overrides: Partial<BookView> = {}): BookView {
  return {
    id: '/inbox/book.epub',
    sourcePath: '/inbox/book.epub',
    oldFilename: 'book.epub',
    format: 'epub',
    sizeBytes: 1024,
    subject: '',
    title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    author: { value: 'Bruce Eckel, Svetlana Isakova', source: 'Heuristic' },
    year: { value: '2021', source: 'Heuristic' },
    status: 'Heuristic',
    category: 'Uncategorized',
    subcategory: '',
    categoryWarning: '',
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel, Svetlana Isakova.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}

describe('BookCard', () => {
  it('renders old filename, editable fields, dest path, and status pill', () => {
    render(BookCard, { book: makeBook() });
    expect(screen.getByText('book.epub')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Atomic Kotlin')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Bruce Eckel, Svetlana Isakova')).toBeInTheDocument();
    expect(screen.getByDisplayValue('2021')).toBeInTheDocument();
    expect(screen.getByText(/Atomic Kotlin \(2021\)/)).toBeInTheDocument();
    expect(screen.getByText('Heuristic')).toBeInTheDocument();
  });

  it('shows a duplicate badge when duplicateStatus is not NotDuplicate', () => {
    render(BookCard, { book: makeBook({ duplicateStatus: 'LikelyDuplicate' }) });
    expect(screen.getByText('Likely dup')).toBeInTheDocument();
  });

  it('dispatches "edited" with source set to Edited after debounce, on title change', async () => {
    vi.useFakeTimers();
    const { component } = render(BookCard, { book: makeBook() });
    const handler = vi.fn();
    component.$on('edited', handler);

    const titleInput = screen.getByDisplayValue('Atomic Kotlin');
    await fireEvent.input(titleInput, { target: { value: 'Atomic Kotlin (Revised)' } });

    expect(handler).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);

    expect(handler).toHaveBeenCalledTimes(1);
    const detail = handler.mock.calls[0][0].detail;
    expect(detail.title.value).toBe('Atomic Kotlin (Revised)');
    expect(detail.title.source).toBe('Edited');
    expect(detail.author.value).toBe('Bruce Eckel, Svetlana Isakova');
    vi.useRealTimers();
  });

  it('checkbox reflects the checked prop and defaults to checked', () => {
    render(BookCard, { book: makeBook() });
    expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).toBeChecked();
  });

  it('renders unchecked when checked prop is false', () => {
    render(BookCard, { book: makeBook(), checked: false });
    expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).not.toBeChecked();
  });

  it('dispatches "toggled" with the new value when the checkbox is clicked', async () => {
    const { component } = render(BookCard, { book: makeBook(), checked: true });
    const handler = vi.fn();
    component.$on('toggled', handler);

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select book.epub' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe(false);
  });

  it('swap button exchanges title and author, marks both Edited, and dispatches immediately', async () => {
    const { component } = render(BookCard, { book: makeBook() });
    const handler = vi.fn();
    component.$on('edited', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Swap title and author' }));

    expect(handler).toHaveBeenCalledTimes(1);
    const detail = handler.mock.calls[0][0].detail;
    expect(detail.title.value).toBe('Bruce Eckel, Svetlana Isakova');
    expect(detail.title.source).toBe('Edited');
    expect(detail.author.value).toBe('Atomic Kotlin');
    expect(detail.author.source).toBe('Edited');
    expect(detail.year.value).toBe('2021');
  });

  it('clicking the filename opens the original file', async () => {
    vi.mocked(OpenFile).mockResolvedValue(undefined);
    render(BookCard, { book: makeBook() });

    await fireEvent.click(screen.getByText('book.epub'));

    await waitFor(() => {
      expect(OpenFile).toHaveBeenCalledWith('/inbox/book.epub');
    });
  });

  it('shows an error banner when OpenFile rejects', async () => {
    vi.mocked(OpenFile).mockRejectedValue(new Error('no application registered for this file type'));
    render(BookCard, { book: makeBook() });

    await fireEvent.click(screen.getByText('book.epub'));

    await waitFor(() => {
      expect(screen.getByText('Error: no application registered for this file type')).toBeInTheDocument();
    });
  });
});
