import { describe, it, expect } from 'vitest';
import { groupIntoShelves, type LibraryBookView } from './types';

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

describe('groupIntoShelves', () => {
  it('groups books into one shelf per subcategory, sorted by subcategory name', () => {
    const books = [
      makeBook({ sourcePath: '/a', subcategory: 'Fantasy', title: 'Mistborn' }),
      makeBook({ sourcePath: '/b', subcategory: 'Sci-Fi', title: 'Foundation' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves.map((s) => s.subcategory)).toEqual(['Fantasy', 'Sci-Fi']);
  });

  it('filters to the given category', () => {
    const books = [
      makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
      makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
    ];

    const shelves = groupIntoShelves(books, 'Fiction', 'title');

    expect(shelves).toHaveLength(1);
    expect(shelves[0].subcategory).toBe('Sci-Fi');
  });

  it('shows every category when the filter is empty', () => {
    const books = [
      makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
      makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves).toHaveLength(2);
  });

  it('sorts books within a shelf by title', () => {
    const books = [
      makeBook({ sourcePath: '/a', title: 'Zebra' }),
      makeBook({ sourcePath: '/b', title: 'Alpha' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves[0].books.map((b) => b.title)).toEqual(['Alpha', 'Zebra']);
  });

  it('sorts books within a shelf by author', () => {
    const books = [
      makeBook({ sourcePath: '/a', author: 'Zed' }),
      makeBook({ sourcePath: '/b', author: 'Amy' }),
    ];

    const shelves = groupIntoShelves(books, '', 'author');

    expect(shelves[0].books.map((b) => b.author)).toEqual(['Amy', 'Zed']);
  });

  it('sorts books within a shelf by year', () => {
    const books = [
      makeBook({ sourcePath: '/a', year: '2020' }),
      makeBook({ sourcePath: '/b', year: '1951' }),
    ];

    const shelves = groupIntoShelves(books, '', 'year');

    expect(shelves[0].books.map((b) => b.year)).toEqual(['1951', '2020']);
  });
});
