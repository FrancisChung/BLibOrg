import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import SettingsView from './SettingsView.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ConfirmResetCoverCache: vi.fn(),
  ResetCoverCache: vi.fn(),
  GetScanConcurrency: vi.fn(),
  SetScanConcurrency: vi.fn(),
}));

import {
  ConfirmResetCoverCache,
  ResetCoverCache,
  GetScanConcurrency,
  SetScanConcurrency,
} from '../../wailsjs/go/main/App';

describe('SettingsView', () => {
  beforeEach(() => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
  });

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

  it('pre-fills the concurrency field with the detected core count when unset', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    expect(input).toHaveValue(8);
  });

  it('pre-fills the concurrency field with the configured value when set', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 3, detected: 8 });
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    expect(input).toHaveValue(3);
  });

  it('saves the concurrency value and shows a success banner', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    vi.mocked(SetScanConcurrency).mockResolvedValue(undefined);
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    await fireEvent.input(input, { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(SetScanConcurrency).toHaveBeenCalledWith(2);
    await screen.findByText('Saved. Takes effect on the next Library refresh.');
  });

  it('saves 0 when the concurrency field is cleared', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 3, detected: 8 });
    vi.mocked(SetScanConcurrency).mockResolvedValue(undefined);
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    await fireEvent.input(input, { target: { value: '' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(SetScanConcurrency).toHaveBeenCalledWith(0);
    await screen.findByText('Saved. Takes effect on the next Library refresh.');
  });

  it('shows an error banner when SetScanConcurrency rejects', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    vi.mocked(SetScanConcurrency).mockRejectedValue(new Error('permission denied'));
    render(SettingsView);

    await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await screen.findByText('permission denied');
  });
});
