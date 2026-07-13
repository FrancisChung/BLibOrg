import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import Sidebar from './Sidebar.svelte';

describe('Sidebar', () => {
  it('highlights the active item and not the others', () => {
    render(Sidebar, { active: 'operations' });
    expect(screen.getByRole('button', { name: 'Operations' }).className).toContain('active');
    expect(screen.getByRole('button', { name: 'Scan & Review' }).className).not.toContain('active');
    expect(screen.getByRole('button', { name: 'Warnings' }).className).not.toContain('active');
  });

  it('emits navigate with "scan" when Scan & Review is clicked', async () => {
    const { component } = render(Sidebar, { active: 'operations' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan & Review' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('scan');
  });

  it('emits navigate with "warnings" when Warnings is clicked', async () => {
    const { component } = render(Sidebar, { active: 'scan' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Warnings' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('warnings');
  });
});
