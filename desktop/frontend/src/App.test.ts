import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../wailsjs/go/main/App', () => ({
  ConfigStatus: vi.fn(),
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  ListOperationBatches: vi.fn(),
  ListCategoryWarnings: vi.fn(),
  ListLibrary: vi.fn(),
}));
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
}));

import App from './App.svelte';
import { ConfigStatus, ListOperationBatches, ListCategoryWarnings, ListLibrary } from '../wailsjs/go/main/App';

describe('App', () => {
  beforeEach(() => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '', error: '' });
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('shows Library by default', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });
  });

  it('switches to the Operations Log view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));

    await waitFor(() => {
      expect(screen.getByText('Operations Log')).toBeInTheDocument();
    });
  });

  it('switches to the Category Warnings view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Warnings' }));

    await waitFor(() => {
      expect(screen.getByText('Category Warnings')).toBeInTheDocument();
    });
  });

  it('shows a config error banner regardless of active view', async () => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '/fake/config.yaml', error: 'not found' });
    render(App);

    await waitFor(() => {
      expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));
    expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
  });

  it('shows a banner for each config rule warning', async () => {
    vi.mocked(ConfigStatus).mockResolvedValue({
      path: '/fake/config.yaml',
      error: '',
      warnings: ['rule 9 (match_value "(?i)\\bc++\\b"): invalid regex: invalid nested repetition operator: `++`'],
    });
    render(App);

    await waitFor(() => {
      expect(screen.getByText(/rule 9/)).toBeInTheDocument();
    });
  });

  it('switches to Scan & Review and back to Library when their sidebar items are clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Scan & Review' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Library' }));
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });
  });

  it('resizes the sidebar by dragging the resize handle, and persists the final width', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);
    await fireEvent.mouseMove(window, { clientX: 300 });
    await fireEvent.mouseUp(window);

    expect(screen.getByRole('navigation')).toHaveStyle({ width: '300px' });
    expect(localStorage.getItem('sidebarWidth')).toBe('300');
  });

  it('clamps sidebar width to 160-400px while dragging past either bound', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);

    await fireEvent.mouseMove(window, { clientX: 50 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '160px' });

    await fireEvent.mouseMove(window, { clientX: 900 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '400px' });

    await fireEvent.mouseUp(window);
    expect(localStorage.getItem('sidebarWidth')).toBe('400');
  });

  it('restores a previously persisted sidebar width on mount', async () => {
    localStorage.setItem('sidebarWidth', '275');
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('navigation')).toHaveStyle({ width: '275px' });
    });
  });

  it('falls back to the 220px default when the stored width is invalid or out of range', async () => {
    localStorage.setItem('sidebarWidth', 'not-a-number');
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('navigation')).toHaveStyle({ width: '220px' });
    });
  });

  it('stops tracking the drag after mouseup, so further mouse movement has no effect', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);
    await fireEvent.mouseMove(window, { clientX: 300 });
    await fireEvent.mouseUp(window);

    await fireEvent.mouseMove(window, { clientX: 180 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '300px' });
  });
});
