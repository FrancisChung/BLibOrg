import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import type { CategoryWarningView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListCategoryWarnings: vi.fn(),
}));

import WarningsLogView from './WarningsLogView.svelte';
import { ListCategoryWarnings } from '../../wailsjs/go/main/App';

function makeWarning(overrides: Partial<CategoryWarningView> = {}): CategoryWarningView {
  return {
    timestamp: '2026-07-13T12:00:00Z',
    sourcePath: '/inbox/a.epub',
    category: 'Fiction',
    subcategory: 'SpaceOpera',
    warning: 'rule matched undeclared subcategory "SpaceOpera" under category "Fiction"',
    ...overrides,
  };
}

describe('WarningsLogView', () => {
  it('shows an empty state when there are no warnings', async () => {
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('No warnings yet.')).toBeInTheDocument();
    });
  });

  it('renders a warning row with source path, category, and message', async () => {
    vi.mocked(ListCategoryWarnings).mockResolvedValue([makeWarning()]);
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('/inbox/a.epub')).toBeInTheDocument();
    });
    expect(screen.getByText(/Fiction \/ SpaceOpera/)).toBeInTheDocument();
    expect(screen.getByText(/rule matched undeclared subcategory/)).toBeInTheDocument();
  });

  it('shows an error banner when the load fails', async () => {
    vi.mocked(ListCategoryWarnings).mockRejectedValue(new Error('boom'));
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
