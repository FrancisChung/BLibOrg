import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import LibraryView from './LibraryView.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListLibrary: vi.fn(),
  OpenFile: vi.fn(),
}));

import { ListLibrary } from '../../wailsjs/go/main/App';

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
});
