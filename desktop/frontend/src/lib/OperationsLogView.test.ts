import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { OperationBatchView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListOperationBatches: vi.fn(),
}));

import OperationsLogView from './OperationsLogView.svelte';
import { ListOperationBatches } from '../../wailsjs/go/main/App';

function makeBatch(overrides: Partial<OperationBatchView> = {}): OperationBatchView {
  return {
    batchId: '20260713-1',
    timestamp: '2026-07-13T12:00:00Z',
    entryCount: 1,
    undoneCount: 0,
    entries: [{ oldPath: '/inbox/a.epub', newPath: '/library/a.epub', opType: 'move', undone: false }],
    ...overrides,
  };
}

describe('OperationsLogView', () => {
  it('shows an empty state when there are no batches', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('No operations yet.')).toBeInTheDocument();
    });
  });

  it('renders a batch row with file count and expands to show entries on click', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Batch 20260713-1')).toBeInTheDocument();
    });
    expect(screen.queryByText('/inbox/a.epub')).not.toBeInTheDocument();

    await fireEvent.click(screen.getByText('Batch 20260713-1'));

    expect(screen.getByText('/inbox/a.epub')).toBeInTheDocument();
    expect(screen.getByText('/library/a.epub')).toBeInTheDocument();
  });

  it('shows an error banner when the load fails', async () => {
    vi.mocked(ListOperationBatches).mockRejectedValue(new Error('boom'));
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
