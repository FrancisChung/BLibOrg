import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../wailsjs/go/main/App', () => ({
  ConfigStatus: vi.fn(),
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  ListOperationBatches: vi.fn(),
  ListCategoryWarnings: vi.fn(),
}));

import App from './App.svelte';
import { ConfigStatus, ListOperationBatches, ListCategoryWarnings } from '../wailsjs/go/main/App';

describe('App', () => {
  beforeEach(() => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '', error: '' });
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
  });

  it('shows Scan & Review by default', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan' })).toBeInTheDocument();
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
});
