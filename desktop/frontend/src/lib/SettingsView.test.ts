import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import SettingsView from './SettingsView.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ConfirmResetCoverCache: vi.fn(),
  ResetCoverCache: vi.fn(),
}));

import { ConfirmResetCoverCache, ResetCoverCache } from '../../wailsjs/go/main/App';

describe('SettingsView', () => {
  it('does not reset when the confirmation dialog is declined', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(false);
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    expect(ResetCoverCache).not.toHaveBeenCalled();
  });

  it('resets and shows a success banner when confirmed', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(true);
    vi.mocked(ResetCoverCache).mockResolvedValue(undefined);
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    expect(ResetCoverCache).toHaveBeenCalled();
    await screen.findByText('Cover cache reset. Open or refresh the Library view to rebuild it.');
  });

  it('shows an error banner when ResetCoverCache rejects', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(true);
    vi.mocked(ResetCoverCache).mockRejectedValue(new Error('permission denied'));
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    await screen.findByText('permission denied');
  });
});
