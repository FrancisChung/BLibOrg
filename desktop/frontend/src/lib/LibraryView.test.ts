import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import LibraryView from './LibraryView.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListLibrary: vi.fn(),
  OpenFile: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
}));

import { ListLibrary } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/Foundation.epub',
    format: 'epub',
    title: 'Foundation',
    author: 'Isaac Asimov',
    year: '1951',
    category: 'Fiction',
    subcategory: 'Sci-Fi',
    coverPath: '',
    coverOverridden: false,
    ...overrides,
  };
}

describe('LibraryView', () => {
  it('groups books into one shelf per subcategory', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', title: 'Foundation', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', title: 'Mistborn', subcategory: 'Fantasy' }),
      ],
      categories: ['Fiction'],
    });

    render(LibraryView, { category: '' });

    await screen.findByText('Sci-Fi');
    expect(screen.getByText('Fantasy')).toBeInTheDocument();
  });

  it('filters to the given category', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
      ],
      categories: ['Fiction', 'Non-Fiction'],
    });

    render(LibraryView, { category: 'Fiction' });

    await screen.findByText('Sci-Fi');
    expect(screen.queryByText('Science')).toBeNull();
  });

  it('emits categoriesLoaded with the categories from the response', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: ['Fiction', 'Non-Fiction'] });
    const { component } = render(LibraryView, { category: '' });
    const handler = vi.fn();
    component.$on('categoriesLoaded', handler);

    await waitFor(() => expect(handler).toHaveBeenCalledTimes(1));
    expect(handler.mock.calls[0][0].detail).toEqual(['Fiction', 'Non-Fiction']);
  });

  it('re-sorts shelves when a sort button is clicked', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', title: 'Zebra', year: '1990', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', title: 'Alpha', year: '2020', subcategory: 'Sci-Fi' }),
      ],
      categories: ['Fiction'],
    });

    render(LibraryView, { category: '' });
    await screen.findByText('Sci-Fi');

    const shelfRow = document.querySelector('.shelf-row') as HTMLElement;
    expect(shelfRow.textContent?.indexOf('Alpha')).toBeLessThan(shelfRow.textContent?.indexOf('Zebra') ?? -1);

    await fireEvent.click(screen.getByRole('button', { name: 'Year' }));

    expect(shelfRow.textContent?.indexOf('Zebra')).toBeLessThan(shelfRow.textContent?.indexOf('Alpha') ?? -1);
  });

  it('shows an error banner when ListLibrary rejects', async () => {
    vi.mocked(ListLibrary).mockRejectedValue(new Error('no config'));
    render(LibraryView, { category: '' });
    await screen.findByText('Error: no config');
  });

  it('shows an empty-state message when there are no books', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');
  });

  it('calls ListLibrary(false) on initial mount', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');

    expect(ListLibrary).toHaveBeenCalledWith(false);
  });

  it('calls ListLibrary(true) when the Refresh button is clicked', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');

    await fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    expect(ListLibrary).toHaveBeenLastCalledWith(true);
  });

  it('shows elapsed time immediately, then the book counter once a progress event lands, and unsubscribes when done', async () => {
    vi.useFakeTimers();

    let resolveList!: (v: { books: LibraryBookView[]; categories: string[] }) => void;
    const pending = new Promise<{ books: LibraryBookView[]; categories: string[] }>((resolve) => {
      resolveList = resolve;
    });
    vi.mocked(ListLibrary).mockReturnValue(pending);

    let progressHandler: (p: { done: number; total: number }) => void = () => {};
    const unsubscribe = vi.fn();
    vi.mocked(EventsOn).mockImplementation((_name, cb) => {
      progressHandler = cb;
      return unsubscribe;
    });

    render(LibraryView, { category: '' });

    await vi.advanceTimersByTimeAsync(2000);
    expect(screen.getByText('Loading library… 2s')).toBeInTheDocument();

    progressHandler({ done: 3, total: 10 });
    await vi.advanceTimersByTimeAsync(1000);
    expect(screen.getByText('Loading library… 3 / 10 books · 3s')).toBeInTheDocument();

    resolveList({ books: [], categories: [] });
    await screen.findByText('No books found in the library folder yet.');
    expect(unsubscribe).toHaveBeenCalledTimes(1);

    vi.useRealTimers();
  });

  it('formats elapsed time as minutes and seconds once loading passes 60s', async () => {
    vi.useFakeTimers();

    const pending = new Promise<{ books: LibraryBookView[]; categories: string[] }>(() => {});
    vi.mocked(ListLibrary).mockReturnValue(pending);
    vi.mocked(EventsOn).mockImplementation(() => () => {});

    render(LibraryView, { category: '' });

    await vi.advanceTimersByTimeAsync(61000);
    expect(screen.getByText('Loading library… 1m 1s')).toBeInTheDocument();

    vi.useRealTimers();
  });
});
