// src/components/common/SortableGroupList/SortableGroupList.test.tsx
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import SortableGroupList from './SortableGroupList';

interface G {
  id: string;
  name: string;
  rows: R[];
  fixed?: boolean;
}
interface R {
  id: string;
  label: string;
}

const groups: G[] = [
  { id: 'g1', name: 'Alpha', rows: [{ id: 'r1', label: 'One' }] },
  { id: 'g2', name: 'Beta', rows: [] },
  { id: 'g3', name: 'Fixed', rows: [{ id: 'r2', label: 'Two' }], fixed: true }
];

const renderList = (
  over: Partial<React.ComponentProps<typeof SortableGroupList<G, R>>> = {}
) => {
  const onToggle = vi.fn();
  const utils = render(
    <SortableGroupList<G, R>
      groups={groups}
      getGroupId={g => g.id}
      rowsOf={g => g.rows}
      getRowId={r => r.id}
      renderGroup={(g, { rowCount }) => ({
        title: g.name,
        meta: `${rowCount} rows`,
        actions: <button type="button">menu {g.name}</button>
      })}
      renderRow={r => ({ content: r.label, meta: <span>meta {r.label}</span> })}
      collapsed={new Set<string>()}
      onToggle={onToggle}
      emptyGroup={g => `nothing in ${g.name}`}
      {...over}
    />
  );
  return { ...utils, onToggle };
};

describe('SortableGroupList', () => {
  it('renders bands with data-group-id and rows with data-row-id, slots in place', () => {
    const { container } = renderList();
    expect(container.querySelectorAll('[data-group-id]')).toHaveLength(3);
    expect(container.querySelectorAll('[data-row-id="r1"]')).toHaveLength(1);
    expect(screen.getByText('Alpha')).toBeInTheDocument();
    // g1 and g3 both have exactly one row, so "1 rows" is not unique
    // document-wide — scope the meta check to Alpha's own band.
    const alphaBand = screen.getByText('Alpha').closest('[role="button"]');
    expect(
      within(alphaBand as HTMLElement).getByText('1 rows')
    ).toBeInTheDocument();
    expect(screen.getByText('One')).toBeInTheDocument();
    expect(screen.getByText('meta One')).toBeInTheDocument();
    expect(screen.getByText('nothing in Beta')).toBeInTheDocument();
  });

  it('hides rows and the empty placeholder when a group is collapsed, with aria-expanded=false', () => {
    renderList({ collapsed: new Set(['g1', 'g2']) });
    expect(screen.queryByText('One')).not.toBeInTheDocument();
    expect(screen.queryByText('nothing in Beta')).not.toBeInTheDocument();
    expect(screen.getByText('Two')).toBeInTheDocument();
    const band = screen.getByText('Alpha').closest('[role="button"]');
    expect(band).toHaveAttribute('aria-expanded', 'false');
  });

  it('renders no placeholder when emptyGroup returns a falsy value', () => {
    const { container } = renderList({
      emptyGroup: g => (g.id === 'g2' ? null : `nothing in ${g.name}`)
    });
    expect(screen.queryByText('nothing in Beta')).not.toBeInTheDocument();
    expect(
      container.querySelector('[data-group-id="g2"] .text-muted')
    ).toBeNull();
  });

  it('toggles on band click and on Enter/Space, but not on action clicks', () => {
    const { onToggle } = renderList();
    fireEvent.click(screen.getByText('Alpha'));
    expect(onToggle).toHaveBeenLastCalledWith('g1');

    const band = screen
      .getByText('Beta')
      .closest('[role="button"]') as HTMLElement;
    fireEvent.keyDown(band, { key: 'Enter' });
    expect(onToggle).toHaveBeenLastCalledWith('g2');
    fireEvent.keyDown(band, { key: ' ' });
    expect(onToggle).toHaveBeenCalledTimes(3);

    fireEvent.click(screen.getByText('menu Alpha'));
    expect(onToggle).toHaveBeenCalledTimes(3);
  });

  it('renders no handles when dnd is off', () => {
    renderList();
    expect(
      screen.queryByRole('button', { name: /drag/i })
    ).not.toBeInTheDocument();
  });

  it('renders labelled handles when dnd is on, honouring groupDraggable and rowsDraggable', () => {
    renderList({
      dnd: {
        enabled: true,
        groupDraggable: g => !g.fixed,
        rowsDraggable: true,
        handleLabel: {
          group: g => `drag group ${g.name}`,
          row: r => `drag row ${r.label}`
        }
      }
    });
    expect(
      screen.getByRole('button', { name: 'drag group Alpha' })
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: 'drag group Fixed' })
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'drag row One' })
    ).toBeInTheDocument();
  });

  it('renders no row handles when rowsDraggable is false', () => {
    renderList({
      dnd: {
        enabled: true,
        rowsDraggable: false,
        handleLabel: {
          group: g => `drag group ${g.name}`,
          row: r => `drag row ${r.label}`
        }
      }
    });
    expect(
      screen.getByRole('button', { name: 'drag group Alpha' })
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /drag row/ })
    ).not.toBeInTheDocument();
  });

  it('a handle click does not toggle the band', () => {
    const { onToggle } = renderList({
      dnd: {
        enabled: true,
        handleLabel: {
          group: g => `drag group ${g.name}`,
          row: r => `drag row ${r.label}`
        }
      }
    });
    fireEvent.click(screen.getByRole('button', { name: 'drag group Alpha' }));
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('a keypress on the grip or on the kebab does not toggle the band', () => {
    const { onToggle } = renderList({
      dnd: {
        enabled: true,
        handleLabel: {
          group: g => `drag group ${g.name}`,
          row: r => `drag row ${r.label}`
        }
      }
    });
    fireEvent.keyDown(
      screen.getByRole('button', { name: 'drag group Alpha' }),
      { key: ' ' }
    );
    fireEvent.keyDown(screen.getByText('menu Alpha'), { key: 'Enter' });
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('a keydown inside the actions slot still reaches the document (dropdown keyboard handling)', () => {
    renderList();
    const spy = vi.fn();
    document.addEventListener('keydown', spy);
    try {
      fireEvent.keyDown(screen.getByText('menu Alpha'), { key: 'Escape' });
      expect(spy).toHaveBeenCalledTimes(1);
    } finally {
      document.removeEventListener('keydown', spy);
    }
  });

  it('toggleLabel becomes the band accessible name', () => {
    renderList({
      renderGroup: (g, { collapsed }) => ({
        title: g.name,
        toggleLabel: `${collapsed ? 'Expand' : 'Collapse'} ${g.name}`
      }),
      collapsed: new Set(['g2'])
    });
    expect(
      screen.getByRole('button', { name: 'Collapse Alpha' })
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Expand Beta' })
    ).toBeInTheDocument();
  });
});
