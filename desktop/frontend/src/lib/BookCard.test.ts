import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import BookCard from './BookCard.svelte';
import type { BookView } from './types';

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
});
