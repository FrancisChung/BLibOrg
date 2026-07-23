import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import LibraryBookCard from './LibraryBookCard.svelte';
import type { LibraryBookView, CoverCandidateView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

import { OpenFile } from '../../wailsjs/go/main/App';

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/Foundation (1951) - Isaac Asimov.epub',
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

describe('LibraryBookCard', () => {
  it('shows the cover image when coverPath is set', () => {
    render(LibraryBookCard, { book: makeBook({ coverPath: '/covers/abc123.jpg' }) });
    const img = screen.getByRole('img') as HTMLImageElement;
    expect(img.src).toContain('/covers/abc123.jpg');
  });

  it('shows a placeholder with the title when coverPath is empty', () => {
    render(LibraryBookCard, { book: makeBook({ coverPath: '' }) });
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByText('Foundation')).toBeInTheDocument();
  });

  it('reveals the filename minus extension via the hover title attribute', () => {
    render(LibraryBookCard, { book: makeBook() });
    const cover = document.querySelector('.cover') as HTMLElement;
    expect(cover.title).toBe('Foundation (1951) - Isaac Asimov');
  });

  it('calls OpenFile with sourcePath when clicked', async () => {
    const book = makeBook();
    render(LibraryBookCard, { book });
    const cover = document.querySelector('.cover') as HTMLElement;

    await fireEvent.click(cover);

    expect(OpenFile).toHaveBeenCalledWith(book.sourcePath);
  });

  it('shows an error banner when OpenFile rejects', async () => {
    vi.mocked(OpenFile).mockRejectedValueOnce(new Error('file moved'));
    render(LibraryBookCard, { book: makeBook() });
    const cover = document.querySelector('.cover') as HTMLElement;

    await fireEvent.click(cover);
    await screen.findByText('Error: file moved');
  });
});

describe('CoverCandidateView / LibraryBookView.coverOverridden', () => {
  it('LibraryBookView accepts coverOverridden', () => {
    const book: LibraryBookView = {
      sourcePath: '/library/book.pdf',
      format: 'pdf',
      title: 'Title',
      author: 'Author',
      year: '2020',
      category: 'Fiction',
      subcategory: '',
      coverPath: '/covers/abc.jpg',
      coverOverridden: true,
    };
    expect(book.coverOverridden).toBe(true);
  });

  it('CoverCandidateView shape', () => {
    const candidate: CoverCandidateView = { page: 1, thumbnailUrl: '/covers/candidate-abc-p1.jpg' };
    expect(candidate.page).toBe(1);
  });
});
