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

  it('highlights Library as active and not the others', () => {
    render(Sidebar, { active: 'library' });
    expect(screen.getByRole('button', { name: 'Library' }).className).toContain('active');
    expect(screen.getByRole('button', { name: 'Scan & Review' }).className).not.toContain('active');
  });

  it('emits navigate with "library" when Library is clicked', async () => {
    const { component } = render(Sidebar, { active: 'scan' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Library' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('library');
  });

  it('shows no category submenu when libraryCategories is empty', () => {
    render(Sidebar, { active: 'library', libraryCategories: [] });
    expect(screen.queryByRole('button', { name: 'All' })).toBeNull();
  });

  it('shows "All" plus each category when libraryCategories is set', () => {
    render(Sidebar, { active: 'library', libraryCategories: ['Fiction', 'Non-Fiction'] });
    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Fiction' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Non-Fiction' })).toBeInTheDocument();
  });

  it('emits navigate("library") and selectCategory when a category is clicked', async () => {
    const { component } = render(Sidebar, { active: 'scan', libraryCategories: ['Fiction'] });
    const navHandler = vi.fn();
    const categoryHandler = vi.fn();
    component.$on('navigate', navHandler);
    component.$on('selectCategory', categoryHandler);

    await fireEvent.click(screen.getByRole('button', { name: 'Fiction' }));

    expect(navHandler.mock.calls[0][0].detail).toBe('library');
    expect(categoryHandler.mock.calls[0][0].detail).toBe('Fiction');
  });

  it('highlights "All" when active is library and activeLibraryCategory is empty', () => {
    render(Sidebar, { active: 'library', libraryCategories: ['Fiction'], activeLibraryCategory: '' });
    expect(screen.getByRole('button', { name: 'All' }).className).toContain('active');
    expect(screen.getByRole('button', { name: 'Fiction' }).className).not.toContain('active');
  });

  it('renders a Settings nav item', () => {
    render(Sidebar, { active: 'operations' });
    expect(screen.getByRole('button', { name: 'Settings' })).toBeInTheDocument();
  });

  it('emits navigate with "settings" when Settings is clicked', async () => {
    const { component } = render(Sidebar, { active: 'library' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Settings' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('settings');
  });
});
