import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import ShelfRow from './ShelfRow.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/book.epub',
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

function setRowMetrics(
  row: HTMLElement,
  { scrollLeft, clientWidth, scrollWidth }: { scrollLeft: number; clientWidth: number; scrollWidth: number },
) {
  Object.defineProperty(row, 'scrollLeft', { value: scrollLeft, configurable: true });
  Object.defineProperty(row, 'clientWidth', { value: clientWidth, configurable: true });
  Object.defineProperty(row, 'scrollWidth', { value: scrollWidth, configurable: true });
}

describe('ShelfRow', () => {
  it('renders the subcategory heading and one card per book', () => {
    render(ShelfRow, {
      subcategory: 'Sci-Fi',
      books: [makeBook({ sourcePath: '/a' }), makeBook({ sourcePath: '/b', title: 'Mistborn' })],
    });
    expect(screen.getByText('Sci-Fi')).toBeInTheDocument();
    expect(screen.getByText('Foundation')).toBeInTheDocument();
    expect(screen.getByText('Mistborn')).toBeInTheDocument();
  });

  it('scrolls the row right when the next button is clicked', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 0, clientWidth: 200, scrollWidth: 800 });
    row.scrollBy = vi.fn();
    await fireEvent.scroll(row);

    await fireEvent.click(screen.getByRole('button', { name: 'Scroll shelf right' }));

    expect(row.scrollBy).toHaveBeenCalledWith({ left: 180, behavior: 'smooth' });
  });

  it('scrolls the row left when the previous button is clicked', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 400, clientWidth: 200, scrollWidth: 800 });
    row.scrollBy = vi.fn();
    await fireEvent.scroll(row);

    await fireEvent.click(screen.getByRole('button', { name: 'Scroll shelf left' }));

    expect(row.scrollBy).toHaveBeenCalledWith({ left: -180, behavior: 'smooth' });
  });

  it('disables the previous button when scrolled to the start', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 0, clientWidth: 200, scrollWidth: 800 });
    await fireEvent.scroll(row);

    expect(screen.getByRole('button', { name: 'Scroll shelf left' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Scroll shelf right' })).not.toBeDisabled();
  });

  it('disables the next button when scrolled to the end', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 600, clientWidth: 200, scrollWidth: 800 });
    await fireEvent.scroll(row);

    expect(screen.getByRole('button', { name: 'Scroll shelf right' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Scroll shelf left' })).not.toBeDisabled();
  });
});
