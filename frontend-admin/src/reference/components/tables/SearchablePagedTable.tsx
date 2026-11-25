import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import EmptyState from 'components/common/EmptyState';
import SubtleBadge from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { faKey, faRobot } from '@fortawesome/free-solid-svg-icons';

// SearchablePagedTableShowcase is the Orkestra reference showcase for the
// two list primitives a tabbed admin page renders its lists through:
// SearchablePagedTable (components/common/advance-table) — the AdvanceTable
// provider + search box + footer wired once, so a tab contributes only its
// columns — and EmptyState (components/common) — the zero-row placeholder,
// optionally with a call-to-action. Live consumers: /admin/compliance and
// /admin/llm. Modeled on the primitive-in-common + showcase-in-reference
// pattern of StatCards.tsx.

const tableCode = `
function TableExample() {
  const rows = [
    { name: 'OpenAI prod', provider: 'OpenAI', status: 'active' },
    { name: 'Local ollama', provider: 'Ollama', status: 'active' },
    { name: 'Claude staging', provider: 'Anthropic', status: 'disabled' },
    { name: 'Gemini batch', provider: 'Google Gemini', status: 'active' }
  ];
  const columns = [
    { accessorKey: 'name', header: 'Name', meta: { headerProps: { className: 'text-900' } } },
    { accessorKey: 'provider', header: 'Provider', meta: { headerProps: { className: 'text-900' } } },
    {
      accessorKey: 'status',
      header: 'Status',
      meta: { headerProps: { className: 'text-900' } },
      cell: ({ row: { original } }) => (
        <SubtleBadge pill bg={original.status === 'active' ? 'success' : 'secondary'}>
          {original.status}
        </SubtleBadge>
      )
    }
  ];
  return (
    <SearchablePagedTable
      data={rows}
      columns={columns}
      searchPlaceholder="Search credentials"
      perPage={3}
    />
  );
}
`;

const emptyCode = `
<EmptyState
  icon={faRobot}
  message="No models configured"
  hint="Define a model, its capabilities and who may use it."
/>
`;

const emptyCtaCode = `
function EmptyWithCtaExample() {
  const [clicks, setClicks] = useState(0);
  return (
    <>
      <EmptyState
        icon={faKey}
        message="No credentials yet"
        hint="Add a provider API key, then configure a model that uses it."
        ctaLabel="Add credential"
        onCta={() => setClicks(c => c + 1)}
      />
      <EmptyState
        icon={faKey}
        message="No credentials yet"
        hint="You need the permission to manage LLM credentials."
        ctaLabel="Add credential"
        ctaDisabled
        onCta={() => {}}
      />
      <p className="fs-10 text-600 text-center mb-0">CTA clicked {clicks} times</p>
    </>
  );
}
`;

const scope = { SearchablePagedTable, EmptyState, SubtleBadge, faKey, faRobot };

const SearchablePagedTableShowcase = () => (
  <>
    <PageHeader
      title="Searchable Paged Table and Empty State"
      description="The list shell of a tabbed admin page. <strong>SearchablePagedTable</strong> wires <code>useAdvanceTable</code> + <code>AdvanceTableProvider</code> + search box + footer once (sortable, paginated, <code>fs-10</code> density), so a tab passes only <code>data</code> and <code>columns</code>. <strong>EmptyState</strong> replaces the table when there are no rows, with an optional call-to-action that can be disabled when the caller lacks the permission."
      className="mb-3"
    />

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="SearchablePagedTable" light={false}>
        <p className="mb-0">
          Client-side search, sort and pagination over the rows you pass.
          Memoise <code>columns</code> when cells render interactive controls:
          AdvanceTable renders each <code>cell</code> as a component, so a fresh
          array per render remounts them.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={tableCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header title="EmptyState" light={false}>
        <p className="mb-0">
          Icon, message and an optional hint. Hints use <code>text-600</code> so
          they stay AA-readable.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={emptyCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>

    <OrkestraComponentCard>
      <OrkestraComponentCard.Header
        title="EmptyState with a call-to-action"
        light={false}
      >
        <p className="mb-0">
          Pass <code>ctaLabel</code> + <code>onCta</code> for the first action
          on an empty list; <code>ctaDisabled</code> keeps it visible but inert
          for a caller without the permission.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={emptyCtaCode}
        scope={scope}
        language="jsx"
      />
    </OrkestraComponentCard>
  </>
);

export default SearchablePagedTableShowcase;
