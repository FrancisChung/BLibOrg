import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import FilterBar from './FilterBar.svelte';

describe('FilterBar', () => {
  it('renders all filter chips and highlights the active one', () => {
    render(FilterBar, { query: '', activeFilter: 'Partial' });
    const chip = screen.getByRole('button', { name: 'Needs review' });
    expect(chip.className).toContain('active');
    expect(screen.getByRole('button', { name: 'All' }).className).not.toContain('active');
  });

  it('emits filterChange with the clicked filter key', async () => {
    const { baseElement } = render(FilterBar, { query: '', activeFilter: 'all' });
    const handler = vi.fn();
    baseElement.addEventListener('filterChange', handler, true);

    await fireEvent.click(screen.getByRole('button', { name: 'Duplicates' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('duplicates');
  });

  it('emits queryChange as the search input changes', async () => {
    const { baseElement } = render(FilterBar, { query: '', activeFilter: 'all' });
    const handler = vi.fn();
    baseElement.addEventListener('queryChange', handler, true);

    const input = screen.getByPlaceholderText('Search title, author, or filename…');
    await fireEvent.input(input, { target: { value: 'kotlin' } });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('kotlin');
  });
});
