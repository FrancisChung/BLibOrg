import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { OperationBatchView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListOperationBatches: vi.fn(),
  ConfirmUndo: vi.fn(),
  UndoBatch: vi.fn(),
}));

import OperationsLogView from './OperationsLogView.svelte';
import { ListOperationBatches, ConfirmUndo, UndoBatch } from '../../wailsjs/go/main/App';

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
  beforeEach(() => {
    vi.clearAllMocks();
  });

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

  it('hides the Undo button once a batch is fully undone', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch({ entryCount: 1, undoneCount: 1 })]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Batch 20260713-1')).toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
  });

  it('shows the Undo button when a batch has entries left to undo', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });
  });

  it('confirms, undoes the batch, and refreshes the list', async () => {
    vi.mocked(ListOperationBatches)
      .mockResolvedValueOnce([makeBatch()])
      .mockResolvedValueOnce([makeBatch({ undoneCount: 1 })]);
    vi.mocked(ConfirmUndo).mockResolvedValue(true);
    vi.mocked(UndoBatch).mockResolvedValue(undefined);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(ConfirmUndo).toHaveBeenCalledWith(1);
      expect(UndoBatch).toHaveBeenCalledWith('20260713-1');
    });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    });
  });

  it('does not call UndoBatch when the confirmation is declined', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    vi.mocked(ConfirmUndo).mockResolvedValue(false);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(ConfirmUndo).toHaveBeenCalled();
    });
    expect(UndoBatch).not.toHaveBeenCalled();
  });

  it('shows an error banner when UndoBatch rejects', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    vi.mocked(ConfirmUndo).mockResolvedValue(true);
    vi.mocked(UndoBatch).mockRejectedValue(new Error('boom'));
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
