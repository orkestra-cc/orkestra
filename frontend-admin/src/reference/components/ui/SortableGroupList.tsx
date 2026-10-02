// src/reference/components/ui/SortableGroupList.tsx
// Reference showcase for the shared SortableGroupList primitive
// (components/common/SortableGroupList): the console's "collapsible group
// band + sortable rows" list: categories whose rows move across groups, or
// a two-level taxonomy whose rows stay fixed. Prefer it over a
// bespoke section/row pair so every grouped list shares one band, one indent,
// one grip and one mobile wrap.

import { useState } from 'react';
import { Button, Dropdown, Form } from 'react-bootstrap';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import SubtleBadge from 'components/common/SubtleBadge';
import CardDropdown from 'components/common/CardDropdown';
import {
  SortableGroupList,
  useCollapsedSet
} from 'components/common/SortableGroupList';

const seed = [
  {
    id: 'events',
    name: 'Events',
    color: '#2c7be5',
    rows: [
      { id: 'r1', title: 'Startup Lab 2026', status: 'published', count: 55 },
      { id: 'r2', title: 'Talent Award', status: 'draft', count: 0 }
    ]
  },
  {
    id: 'newsletters',
    name: 'Newsletters',
    color: '#00d27a',
    rows: [
      { id: 'r3', title: 'Monthly digest', status: 'published', count: 90 }
    ]
  },
  { id: 'archive', name: 'Archive', color: '', rows: [] }
];

const basicCode = `Demo = () => {
  const [groups, setGroups] = useState(seed);
  const { collapsed, toggle } = useCollapsedSet('reference.sgl.basic');

  const onReorderGroups = ids =>
    setGroups(prev => ids.map(id => prev.find(g => g.id === id)));

  const onMoveRow = (rowId, toGroupId, orderedRowIds) =>
    setGroups(prev => {
      const row = prev.flatMap(g => g.rows).find(r => r.id === rowId);
      return prev.map(g => {
        const rows = g.rows.filter(r => r.id !== rowId);
        if (g.id !== toGroupId) return { ...g, rows };
        return { ...g, rows: orderedRowIds.map(id => (id === rowId ? row : rows.find(r => r.id === id))) };
      });
    });

  return (
    <div className="border rounded overflow-hidden">
      <SortableGroupList
        groups={groups}
        getGroupId={g => g.id}
        rowsOf={g => g.rows}
        getRowId={r => r.id}
        collapsed={collapsed}
        onToggle={toggle}
        renderGroup={(g, { rowCount }) => ({
          marker: (
            <span
              aria-hidden
              className="d-inline-block rounded-circle border flex-shrink-0"
              style={{ width: 10, height: 10, backgroundColor: g.color || 'transparent' }}
            />
          ),
          title: g.name,
          meta: rowCount + ' forms',
          actions: (
            <CardDropdown btnRevealClass="text-700">
              <div className="py-2">
                <Dropdown.Item>Rename</Dropdown.Item>
                <Dropdown.Item className="text-danger">Delete</Dropdown.Item>
              </div>
            </CardDropdown>
          )
        })}
        renderRow={r => ({
          content: <a href="#!" className="fw-semibold text-truncate">{r.title}</a>,
          meta: (
            <>
              <SubtleBadge bg={r.status === 'published' ? 'success' : 'secondary'}>{r.status}</SubtleBadge>
              <span className="text-700">{r.count}</span>
            </>
          )
        })}
        emptyGroup={() => 'Nothing here yet — drop a row on this band.'}
        dnd={{
          enabled: true,
          onReorderGroups,
          onMoveRow,
          handleLabel: { group: g => 'Drag ' + g.name, row: r => 'Drag ' + r.title }
        }}
      />
    </div>
  );
};`;

const fixedRowsCode = `Demo = () => {
  const [groups, setGroups] = useState(seed.slice(0, 2));
  const { collapsed, toggle, expandAll, collapseAll } = useCollapsedSet('reference.sgl.fixed');

  return (
    <>
      <div className="d-flex gap-2 mb-2">
        <Button size="sm" variant="orkestra-default" onClick={expandAll}>Expand all</Button>
        <Button size="sm" variant="orkestra-default" onClick={() => collapseAll(groups.map(g => g.id))}>Collapse all</Button>
      </div>
      <div className="border rounded overflow-hidden">
        <SortableGroupList
          groups={groups}
          getGroupId={g => g.id}
          rowsOf={g => g.rows}
          getRowId={r => r.id}
          collapsed={collapsed}
          onToggle={toggle}
          renderGroup={(g, { rowCount }) => ({
            title: g.name,
            meta: (
              <>
                <SubtleBadge bg="secondary">locked</SubtleBadge>
                <span>{rowCount} tags</span>
              </>
            )
          })}
          renderRow={r => ({
            content: (
              <>
                <span className="fw-medium text-900">{r.title}</span>
                <code className="fs-11 text-muted d-none d-md-inline">{r.id}</code>
              </>
            ),
            meta: <span className="fs-11 text-muted">{r.count} contacts</span>
          })}
          dnd={{
            enabled: true,
            rowsDraggable: false,
            onReorderGroups: ids => setGroups(prev => ids.map(id => prev.find(g => g.id === id))),
            handleLabel: { group: g => 'Drag ' + g.name, row: () => '' }
          }}
        />
      </div>
    </>
  );
};`;

const filteredCode = `Demo = () => {
  const [q, setQ] = useState('');
  const [all, setAll] = useState(seed);
  const { collapsed, toggle } = useCollapsedSet('reference.sgl.filtered');
  const needle = q.trim().toLowerCase();

  // Drag is only on while the filter is empty, so these always see full groups.
  const onReorderGroups = ids =>
    setAll(prev => ids.map(id => prev.find(g => g.id === id)));

  const onMoveRow = (rowId, toGroupId, orderedRowIds) =>
    setAll(prev => {
      const row = prev.flatMap(g => g.rows).find(r => r.id === rowId);
      return prev.map(g => {
        const rows = g.rows.filter(r => r.id !== rowId);
        if (g.id !== toGroupId) return { ...g, rows };
        return { ...g, rows: orderedRowIds.map(id => (id === rowId ? row : rows.find(r => r.id === id))) };
      });
    });

  const groups = all
    .map(g => ({ ...g, rows: g.rows.filter(r => !needle || r.title.toLowerCase().includes(needle)) }))
    .filter(g => !needle || g.rows.length > 0);

  return (
    <>
      <Form.Control
        size="sm"
        type="search"
        placeholder="Filter rows… (drag switches off while filtering)"
        value={q}
        onChange={e => setQ(e.target.value)}
        className="mb-2"
        style={{ maxWidth: 320 }}
      />
      <div className="border rounded overflow-hidden">
        <SortableGroupList
          groups={groups}
          getGroupId={g => g.id}
          rowsOf={g => g.rows}
          getRowId={r => r.id}
          collapsed={collapsed}
          onToggle={toggle}
          renderGroup={(g, { rowCount }) => ({ title: g.name, meta: '(' + rowCount + ')' })}
          renderRow={r => ({ content: r.title, meta: <SubtleBadge bg="secondary">{r.status}</SubtleBadge> })}
          emptyGroup={() => 'No rows in this group.'}
          dnd={{
            enabled: needle === '',
            groupDraggable: g => g.id !== 'archive',
            onReorderGroups,
            onMoveRow,
            handleLabel: { group: g => 'Drag ' + g.name, row: r => 'Drag ' + r.title }
          }}
        />
      </div>
    </>
  );
};`;

const scope = {
  useState,
  seed,
  SortableGroupList,
  useCollapsedSet,
  SubtleBadge,
  CardDropdown,
  Dropdown,
  Button,
  Form
};

const SortableGroupListShowcase = () => (
  <>
    <PageHeader
      title="Sortable Group List"
      description="The console's collapsible, drag-and-drop grouped list (components/common/SortableGroupList): a fs-10 band on the tertiary surface with grip, chevron, marker, title, meta and actions; rows indented one level under it with a grip, content and a right-pinned meta cluster that wraps under the content on narrow screens. Pages pass data and render-props; the primitive owns dnd-kit, collapse and the visual contract."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Groups + rows, both sortable"
        light={false}
      >
        <p className="mb-0">
          Drag a band to reorder groups; drag a row to reorder it or drop it on
          another band (even an empty or collapsed one). The page receives
          <code>onReorderGroups(ids)</code> and{' '}
          <code>onMoveRow(rowId, toGroupId, orderedRowIds)</code> — the latter
          also fires for a same-group reorder.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={basicCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Fixed rows + expand/collapse all"
        light={false}
      >
        <p className="mb-0">
          <code>rowsDraggable: false</code> keeps the rows put (a taxonomy's
          children are not siblings). <code>useCollapsedSet</code> exposes{' '}
          <code>expandAll</code> / <code>collapseAll</code> for a view menu, and
          persists the set per <code>storageKey</code>.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={fixedRowsCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="Filtered view + fixed trailing bucket"
        light={false}
      >
        <p className="mb-0">
          Pass <code>enabled: false</code> while a filter is active — a reorder
          must see the full set — and the list renders without handles or a
          DndContext. <code>groupDraggable</code> pins a bucket (an
          "uncategorized" tail) while still accepting dropped rows.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={filteredCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default SortableGroupListShowcase;
