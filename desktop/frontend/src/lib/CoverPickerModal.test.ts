import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import CoverPickerModal from './CoverPickerModal.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListPDFCoverCandidates: vi.fn(),
  SetCoverOverride: vi.fn(),
  SetCoverOverrideCustomFromFile: vi.fn(),
  ClearCoverOverride: vi.fn(),
  PickCoverImageFile: vi.fn(),
}));

import {
  ListPDFCoverCandidates,
  SetCoverOverride,
  SetCoverOverrideCustomFromFile,
  ClearCoverOverride,
  PickCoverImageFile,
} from '../../wailsjs/go/main/App';

// vitest.config.ts sets neither restoreMocks nor clearMocks, so call
// history (not just return values) would otherwise leak between `it`
// blocks in this file -- e.g. the "cancelling" test's
// `not.toHaveBeenCalled()` on SetCoverOverrideCustomFromFile would
// falsely fail after the earlier "uploading" test already called it.
beforeEach(() => {
  vi.mocked(ListPDFCoverCandidates).mockReset();
  vi.mocked(SetCoverOverride).mockReset();
  vi.mocked(SetCoverOverrideCustomFromFile).mockReset();
  vi.mocked(ClearCoverOverride).mockReset();
  vi.mocked(PickCoverImageFile).mockReset();
});

describe('CoverPickerModal', () => {
  it('renders a thumbnail per candidate returned by ListPDFCoverCandidates', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([
      { page: 1, thumbnailUrl: '/covers/candidate-a-p1.jpg' },
      { page: 3, thumbnailUrl: '/covers/candidate-a-p3.jpg' },
    ]);

    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });

    await waitFor(() => {
      expect(screen.getByAltText('Page 1')).toBeTruthy();
      expect(screen.getByAltText('Page 3')).toBeTruthy();
    });
  });

  it('choosing a thumbnail calls SetCoverOverride and dispatches updated + close', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([{ page: 2, thumbnailUrl: '/covers/candidate-a-p2.jpg' }]);
    vi.mocked(SetCoverOverride).mockResolvedValue('/covers/abc.jpg');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    const updated = vi.fn();
    const closed = vi.fn();
    component.$on('updated', updated);
    component.$on('close', closed);

    await waitFor(() => screen.getByAltText('Page 2'));
    await fireEvent.click(screen.getByAltText('Page 2'));

    await waitFor(() => {
      expect(SetCoverOverride).toHaveBeenCalledWith('/library/book.pdf', 2);
      expect(updated).toHaveBeenCalledTimes(1);
      expect(closed).toHaveBeenCalledTimes(1);
    });
    expect(updated.mock.calls[0][0].detail).toEqual({ coverPath: '/covers/abc.jpg', coverOverridden: true });
  });

  it('uploading a custom image calls PickCoverImageFile then SetCoverOverrideCustomFromFile', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(PickCoverImageFile).mockResolvedValue('/tmp/chosen.png');
    vi.mocked(SetCoverOverrideCustomFromFile).mockResolvedValue('/covers/override-xyz.png');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    const updated = vi.fn();
    component.$on('updated', updated);

    await waitFor(() => screen.getByText('Upload custom image…'));
    await fireEvent.click(screen.getByText('Upload custom image…'));

    await waitFor(() => {
      expect(PickCoverImageFile).toHaveBeenCalled();
      expect(SetCoverOverrideCustomFromFile).toHaveBeenCalledWith('/library/book.pdf', '/tmp/chosen.png');
      expect(updated).toHaveBeenCalledTimes(1);
    });
  });

  it('cancelling the native file dialog does not call SetCoverOverrideCustomFromFile', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(PickCoverImageFile).mockResolvedValue('');

    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });

    await waitFor(() => screen.getByText('Upload custom image…'));
    await fireEvent.click(screen.getByText('Upload custom image…'));

    expect(SetCoverOverrideCustomFromFile).not.toHaveBeenCalled();
  });

  it('shows "Reset to auto-detected" only when coverOverridden is true', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: true });
    await waitFor(() => expect(screen.getByText('Reset to auto-detected')).toBeTruthy());
  });

  it('clicking "Reset to auto-detected" calls ClearCoverOverride', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(ClearCoverOverride).mockResolvedValue('/covers/auto.jpg');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: true });
    const updated = vi.fn();
    component.$on('updated', updated);

    await waitFor(() => screen.getByText('Reset to auto-detected'));
    await fireEvent.click(screen.getByText('Reset to auto-detected'));

    await waitFor(() => {
      expect(ClearCoverOverride).toHaveBeenCalledWith('/library/book.pdf');
      expect(updated).toHaveBeenCalledTimes(1);
    });
    expect(updated.mock.calls[0][0].detail).toEqual({ coverPath: '/covers/auto.jpg', coverOverridden: false });
  });

  it('shows the error message if ListPDFCoverCandidates rejects', async () => {
    vi.mocked(ListPDFCoverCandidates).mockRejectedValue(new Error('boom'));
    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    await waitFor(() => expect(screen.getByText(/boom/)).toBeTruthy());
  });
});
