import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import team13 from 'assets/img/team/13.jpg';
import team2 from 'assets/img/team/2.jpg';
import team3 from 'assets/img/team/3.jpg';
import team4 from 'assets/img/team/4.jpg';
import ActionButton from 'components/common/ActionButton';
import Avatar from 'components/common/Avatar';
import CardDropdown from 'components/common/CardDropdown';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import PageHeader from 'components/common/PageHeader';
import SubtleBadge from 'components/common/SubtleBadge';
import IconButton from 'components/common/IconButton';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import AdvanceTableSearchBox from 'components/common/advance-table/AdvanceTableSearchBox';
import AdvanceTablePagination from 'components/common/advance-table/AdvanceTablePagination';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider, {
  useAdvanceTableContext
} from 'providers/AdvanceTableProvider';

import { Button, Col, Row, Badge, Dropdown, Form, Table } from 'react-bootstrap';

/* =============================================================================
   SECTION 1: BASIC BOOTSTRAP TABLES
   ============================================================================= */

const basicTableCode = `
<Table responsive>
  <thead>
    <tr>
      <th scope="col">Name</th>
      <th scope="col">Email</th>
      <th className="text-end" scope="col">Actions</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>Ricky Antony</td>
      <td>ricky@example.com</td>
      <td className="text-end">
        <ActionButton icon="edit" title="Edit" variant="action" className="p-0 me-2" />
        <ActionButton icon="trash-alt" title="Delete" variant="action" className="p-0" />
      </td>
    </tr>
    <tr>
      <td>Emma Watson</td>
      <td>emma@example.com</td>
      <td className="text-end">
        <ActionButton icon="edit" title="Edit" variant="action" className="p-0 me-2" />
        <ActionButton icon="trash-alt" title="Delete" variant="action" className="p-0" />
      </td>
    </tr>
  </tbody>
</Table>
`;

const stripedTableCode = `
<Table striped responsive>
  <thead>
    <tr>
      <th scope="col">Name</th>
      <th scope="col">Email</th>
      <th className="text-end" scope="col">Actions</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>Ricky Antony</td>
      <td>ricky@example.com</td>
      <td className="text-end">
        <CardDropdown>
          <div className="py-2">
            <Dropdown.Item>Edit</Dropdown.Item>
            <Dropdown.Item className='text-danger'>Delete</Dropdown.Item>
          </div>
        </CardDropdown>
      </td>
    </tr>
    <tr>
      <td>Emma Watson</td>
      <td>emma@example.com</td>
      <td className="text-end">
        <CardDropdown>
          <div className="py-2">
            <Dropdown.Item>Edit</Dropdown.Item>
            <Dropdown.Item className='text-danger'>Delete</Dropdown.Item>
          </div>
        </CardDropdown>
      </td>
    </tr>
  </tbody>
</Table>
`;

const hoverableCode = `const Actions = () => (
  <div className="end-0 top-50 pe-3 translate-middle-y hover-actions">
    <Button variant="light" size="sm" className="border-300 me-1 text-600">
      <FontAwesomeIcon icon="edit" />
    </Button>
    <Button variant="light" size="sm" className="border-300 text-600">
      <FontAwesomeIcon icon="trash-alt" />
    </Button>
  </div>
);

const HoverableActionsExample = () => {
  return (
    <Table hover responsive>
      <thead>
        <tr>
          <th scope="col">Name</th>
          <th scope="col">Email</th>
          <th scope="col"></th>
        </tr>
      </thead>
      <tbody>
        <tr className="hover-actions-trigger">
          <td>Ricky Antony</td>
          <td>ricky@example.com</td>
          <td className="w-auto">
            <Actions />
          </td>
        </tr>
        <tr className="hover-actions-trigger">
          <td>Emma Watson</td>
          <td>emma@example.com</td>
          <td className="w-auto">
            <Actions />
          </td>
        </tr>
      </tbody>
    </Table>
  );
};

render(<HoverableActionsExample />)
`;

const responsiveTableCode = `const TableRow = ({ data }) => (
  <tr className="align-middle">
    <td className="text-nowrap">
      <div className="d-flex align-items-center">
        <Avatar src={data.avatar} size="l" name={data.name} />
        <div className="ms-2">{data.name}</div>
      </div>
    </td>
    <td className="text-nowrap">{data.email}</td>
    <td className="text-nowrap">{data.phone}</td>
    <td>
      <SubtleBadge pill bg={data.status.type}>
        {data.status.title}
        <FontAwesomeIcon icon={data.status.icon} className="ms-2" />
      </SubtleBadge>
    </td>
    <td className="text-end">$199</td>
  </tr>
);

const ResponsiveTableExample = () => {
  const customers = [
    {
      name: 'Ricky Antony',
      avatar: team4,
      email: 'ricky@example.com',
      phone: '(201) 200-1851',
      status: { title: 'Completed', type: 'success', icon: 'check' }
    },
    {
      name: 'Emma Watson',
      avatar: team13,
      email: 'emma@example.com',
      phone: '(212) 228-8403',
      status: { title: 'Processing', type: 'primary', icon: 'redo' }
    },
    {
      name: 'Antony Hopkins',
      avatar: team2,
      email: 'antony@example.com',
      phone: '(901) 324-3127',
      status: { title: 'Pending', type: 'warning', icon: 'stream' }
    }
  ];

  return (
    <Table responsive striped hover>
      <thead>
        <tr>
          <th scope="col">Name</th>
          <th scope="col">Email</th>
          <th scope="col">Phone</th>
          <th scope="col">Status</th>
          <th className="text-end" scope="col">Amount</th>
        </tr>
      </thead>
      <tbody>
        {customers.map(customer => (
          <TableRow data={customer} key={customer.email}/>
        ))}
      </tbody>
    </Table>
  );
};

render(<ResponsiveTableExample />)
`;

/* =============================================================================
   SECTION 2: ADVANCE TABLE SETUP
   ============================================================================= */

const advanceTableData = `const columns = [
  {
    accessorKey: 'name',
    header: 'Name',
    meta: {
      headerProps: { className: 'text-900' }
    },
  },
  {
    accessorKey: 'email',
    header: 'Email',
    meta: {
      headerProps: { className: 'text-900' }
    },
  },
  {
    accessorKey: 'age',
    header: 'Age',
    meta: {
      headerProps: { className: 'text-900' }
    },
  }
];

const data = [
  { name: 'Anna', email: 'anna@example.com', age: 18 },
  { name: 'Homer', email: 'homer@example.com', age: 35 },
  { name: 'Oscar', email: 'oscar@example.com', age: 52 },
  { name: 'Emily', email: 'emily@example.com', age: 30 },
  { name: 'Jara', email: 'jara@example.com', age: 25 },
  { name: 'Clark', email: 'clark@example.com', age: 39 },
  { name: 'Jennifer', email: 'jennifer@example.com', age: 52 },
  { name: 'Tony', email: 'tony@example.com', age: 30 },
  { name: 'Tom', email: 'tom@example.com', age: 25 },
  { name: 'Michael', email: 'michael@example.com', age: 39 },
  { name: 'Antony', email: 'antony@example.com', age: 39 },
  { name: 'Raymond', email: 'raymond@example.com', age: 52 },
];`;

const advanceTableBasicCode = `${advanceTableData}

function AdvanceTableExample() {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: true,
    perPage: 5
  });

  return(
    <AdvanceTableProvider {...table}>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          bordered: true,
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="mt-3">
        <AdvanceTableFooter
          rowInfo
          navButtons
          rowsPerPageSelection
        />
      </div>
    </AdvanceTableProvider>
  )
}

render(<AdvanceTableExample />)
`;

/* =============================================================================
   SECTION 3: PAGINATION STYLES
   ============================================================================= */

const paginationNumberingCode = `${advanceTableData}

function AdvanceTableExample() {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: true,
    perPage: 5
  });

  return(
    <AdvanceTableProvider {...table}>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          bordered: true,
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="mt-3">
        <AdvanceTablePagination />
      </div>
    </AdvanceTableProvider>
  )
}

render(<AdvanceTableExample />)
`;

/* =============================================================================
   SECTION 4: SEARCH & FILTERING
   ============================================================================= */

const searchableTableCode = `${advanceTableData}

function AdvanceTableExample() {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    pagination: true,
    perPage: 5
  });

  return (
    <AdvanceTableProvider {...table}>
      <Row className="flex-end-center mb-3">
        <Col xs="auto" sm={6} lg={4}>
          <AdvanceTableSearchBox placeholder="Search..." />
        </Col>
      </Row>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          bordered: true,
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
      <div className="mt-3">
        <AdvanceTableFooter
          rowInfo
          navButtons
          rowsPerPageSelection
        />
      </div>
    </AdvanceTableProvider>
  );
}

render(<AdvanceTableExample />)`;

/* =============================================================================
   SECTION 5: ROW SELECTION & BULK ACTIONS
   ============================================================================= */

const selectionCode = `const columns = [
  {
    accessorKey: 'name',
    header: 'Name',
    meta: { headerProps: { className: 'text-900' } }
  },
  {
    accessorKey: 'email',
    header: 'Email',
    meta: { headerProps: { className: 'text-900' } },
    cell: ({ row: { original } }) => (
      <a href={'mailto:' + original.email}>{original.email}</a>
    )
  },
  {
    accessorKey: 'age',
    header: 'Age',
    meta: {
      headerProps: { className: 'text-900' },
      cellProps: { className: 'fw-medium' }
    }
  }
];

const data = [
  { name: 'Anna', email: 'anna@example.com', age: 18 },
  { name: 'Homer', email: 'homer@example.com', age: 35 },
  { name: 'Oscar', email: 'oscar@example.com', age: 52 },
  { name: 'Emily', email: 'emily@example.com', age: 30 },
  { name: 'Jara', email: 'jara@example.com', age: 25 }
];

function BulkActions() {
  const { getSelectedRowModel } = useAdvanceTableContext();
  const selectedCount = getSelectedRowModel().rows.length;

  return (
    <Row className="flex-between-center mb-3">
      <Col xs={4} sm="auto" className="d-flex align-items-center pe-0">
        <h5 className="fs-9 mb-0 text-nowrap py-2 py-xl-0">
          {selectedCount > 0
            ? 'You have selected ' + selectedCount + ' rows'
            : 'Selection Example'}
        </h5>
      </Col>
      <Col xs={8} sm="auto" className="ms-auto text-end ps-0">
        {selectedCount > 0 ? (
          <div className="d-flex">
            <Form.Select size="sm" aria-label="Bulk actions">
              <option>Bulk Actions</option>
              <option value="activate">Activate</option>
              <option value="deactivate">Deactivate</option>
              <option value="delete">Delete</option>
            </Form.Select>
            <Button variant="orkestra-default" size="sm" className="ms-2">
              Apply
            </Button>
          </div>
        ) : (
          <div id="table-actions">
            <IconButton variant="orkestra-default" size="sm" icon="plus" transform="shrink-3" className='me-2'>
              <span className="d-none d-sm-inline-block ms-1">New</span>
            </IconButton>
            <IconButton variant="orkestra-default" size="sm" icon="external-link-alt" transform="shrink-3">
              <span className="d-none d-sm-inline-block ms-1">Export</span>
            </IconButton>
          </div>
        )}
      </Col>
    </Row>
  );
}

function AdvanceTableExample() {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true,
    selection: true,
    pagination: true,
    perPage: 5,
    selectionColumnWidth: 30
  });

  return(
    <AdvanceTableProvider {...table}>
      <BulkActions />
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
    </AdvanceTableProvider>
  )
}

render(<AdvanceTableExample />)
`;

/* =============================================================================
   SECTION 6: CUSTOM CELL RENDERING
   ============================================================================= */

const customCellCode = `const columns = [
  {
    accessorKey: 'name',
    header: 'User',
    meta: { headerProps: { className: 'text-900' } },
    // Custom cell with Avatar
    cell: ({ row: { original } }) => (
      <div className="d-flex align-items-center">
        <Avatar src={original.avatar} size="l" name={original.name} />
        <div className="ms-2">
          <h6 className="mb-0">{original.name}</h6>
          <small className="text-muted">{original.email}</small>
        </div>
      </div>
    )
  },
  {
    accessorKey: 'role',
    header: 'Role',
    meta: { headerProps: { className: 'text-900' } },
    // Custom cell with Badge
    cell: ({ row: { original } }) => {
      const roleColors = {
        admin: 'danger',
        manager: 'warning',
        user: 'info',
        guest: 'secondary'
      };
      return (
        <Badge bg={roleColors[original.role] || 'secondary'}>
          {original.role.charAt(0).toUpperCase() + original.role.slice(1)}
        </Badge>
      );
    }
  },
  {
    accessorKey: 'status',
    header: 'Status',
    meta: { headerProps: { className: 'text-900' } },
    // Custom cell with SubtleBadge
    cell: ({ row: { original } }) => (
      <SubtleBadge bg={original.isActive ? 'success' : 'secondary'}>
        {original.isActive ? 'Active' : 'Inactive'}
      </SubtleBadge>
    )
  },
  {
    accessorKey: 'actions',
    header: 'Actions',
    meta: { headerProps: { className: 'text-end text-900' } },
    // Custom cell with Dropdown
    cell: ({ row: { original } }) => (
      <Dropdown align="end" className="btn-reveal-trigger">
        <Dropdown.Toggle variant="link" size="sm" className="text-600 btn-reveal">
          <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
        </Dropdown.Toggle>
        <Dropdown.Menu className="border py-0">
          <div className="py-2">
            <Dropdown.Item>View Details</Dropdown.Item>
            <Dropdown.Item>Edit</Dropdown.Item>
            <Dropdown.Divider />
            <Dropdown.Item className="text-danger">Delete</Dropdown.Item>
          </div>
        </Dropdown.Menu>
      </Dropdown>
    )
  }
];

const data = [
  { name: 'Anna', email: 'anna@example.com', avatar: team4, role: 'admin', isActive: true },
  { name: 'Homer', email: 'homer@example.com', avatar: team13, role: 'manager', isActive: true },
  { name: 'Oscar', email: 'oscar@example.com', avatar: null, role: 'user', isActive: false },
  { name: 'Emily', email: 'emily@example.com', avatar: team2, role: 'guest', isActive: true }
];

function AdvanceTableExample() {
  const table = useAdvanceTable({
    data,
    columns,
    sortable: true
  });

  return(
    <AdvanceTableProvider {...table}>
      <AdvanceTable
        headerClassName="bg-200 text-nowrap align-middle"
        rowClassName="align-middle white-space-nowrap"
        tableProps={{
          striped: true,
          className: 'fs-10 mb-0 overflow-hidden'
        }}
      />
    </AdvanceTableProvider>
  )
}

render(<AdvanceTableExample />)
`;

/* =============================================================================
   SECTION 7: PRODUCTION PATTERNS - TABLE HEADER WITH FILTERS
   ============================================================================= */

const tableHeaderPatternCode = `// Production-ready TableHeader pattern
// This shows the recommended structure for table headers

const TableHeader = () => {
  const { getSelectedRowModel, setColumnFilters, getFilteredRowModel } = useAdvanceTableContext();
  const [selectedFilter, setSelectedFilter] = useState('All');

  const filters = ['All', 'Active', 'Inactive', 'Pending'];

  const handleFilter = (filter) => {
    setSelectedFilter(filter);
    if (filter === 'All') {
      setColumnFilters([]);
    } else {
      setColumnFilters([{ id: 'status', value: filter.toLowerCase() }]);
    }
  };

  const handleExportCSV = () => {
    const rows = getFilteredRowModel().rows;
    // Transform and export data...
    console.log('Exporting', rows.length, 'rows');
  };

  return (
    <div className="d-lg-flex justify-content-between">
      {/* Left side: Title + Search */}
      <Row className="flex-between-center gy-2 px-x1">
        <Col xs="auto" className="pe-0">
          <h6 className="mb-0">Data Management</h6>
        </Col>
        <Col xs="auto">
          <AdvanceTableSearchBox
            className="input-search-width"
            placeholder="Search..."
          />
        </Col>
      </Row>

      <div className="border-bottom border-200 my-3"></div>

      {/* Right side: Filters + Actions */}
      <div className="d-flex align-items-center justify-content-between justify-content-lg-end px-x1">
        {/* Filter Dropdown */}
        <Dropdown className="font-sans-serif">
          <Dropdown.Toggle variant="orkestra-default" size="sm" className="text-600">
            <FontAwesomeIcon icon="filter" transform="shrink-4" className="me-2" />
            <span className="d-none d-sm-inline-block">{selectedFilter}</span>
          </Dropdown.Toggle>
          <Dropdown.Menu className="border py-2">
            {filters.map((filter) => (
              <Dropdown.Item
                key={filter}
                onClick={() => handleFilter(filter)}
                className={selectedFilter === filter ? 'active' : ''}
              >
                {filter}
                {selectedFilter === filter && (
                  <FontAwesomeIcon icon="check" className="ms-2" />
                )}
              </Dropdown.Item>
            ))}
          </Dropdown.Menu>
        </Dropdown>

        <div className="bg-300 mx-3 d-none d-lg-block" style={{ width: '1px', height: '29px' }} />

        {/* Conditional: Bulk Actions or Regular Actions */}
        {getSelectedRowModel().rows.length > 0 ? (
          <div className="d-flex">
            <Form.Select size="sm" aria-label="Bulk actions">
              <option>Bulk actions</option>
              <option value="activate">Activate</option>
              <option value="delete">Delete</option>
            </Form.Select>
            <Button variant="orkestra-default" size="sm" className="ms-2">
              Apply
            </Button>
          </div>
        ) : (
          <div id="table-actions">
            <IconButton variant="orkestra-default" size="sm" icon="plus" transform="shrink-3" iconAlign="middle">
              <span className="d-none d-sm-inline-block ms-1">New</span>
            </IconButton>
            <IconButton
              variant="orkestra-default"
              size="sm"
              icon="external-link-alt"
              transform="shrink-3"
              className="mx-2"
              iconAlign="middle"
              onClick={handleExportCSV}
            >
              <span className="d-none d-sm-inline-block ms-1">Export</span>
            </IconButton>
          </div>
        )}
      </div>
    </div>
  );
};
`;

/* =============================================================================
   SECTION 8: HOOK OPTIONS REFERENCE
   ============================================================================= */

const hookOptionsCode = `// useAdvanceTable Hook - All Configuration Options

interface UseAdvanceTableOptions<T> {
  // Required
  columns: ColumnDef<T>[];        // TanStack column definitions
  data: T[];                       // Raw data array

  // Optional - Sorting
  sortable?: boolean;              // Enable column sorting (default: false)

  // Optional - Selection
  selection?: boolean;             // Enable row selection with checkboxes
  selectionColumnWidth?: string | number; // Width of selection column
  selectionHeaderClassname?: string;      // Class for selection header

  // Optional - Pagination
  pagination?: boolean;            // Enable pagination (default: false)
  perPage?: number;                // Rows per page (default: 10)

  // Optional - Initial State
  initialState?: Partial<TableState>; // Initial table state
}

// Example usage:
const table = useAdvanceTable({
  columns,
  data,
  sortable: true,
  selection: true,
  selectionColumnWidth: 30,
  pagination: true,
  perPage: 10,
  initialState: {
    sorting: [{ id: 'name', desc: false }]
  }
});
`;

/* =============================================================================
   MAIN COMPONENT
   ============================================================================= */

const Tables = () => (
  <>
    <PageHeader
      title="Tables"
      description="Complete guide to tables - from basic Bootstrap tables to advanced data grids with sorting, filtering, pagination, and production patterns."
      className="mb-3"
    >
      <Button
        href="https://tanstack.com/table/v8/docs/introduction"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        TanStack Table Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    {/* ========== BASIC TABLES ========== */}
    <h5 className="mb-3 mt-4 fw-bold text-primary">
      <FontAwesomeIcon icon="table" className="me-2" />
      Basic Bootstrap Tables
    </h5>
    <p className="text-muted mb-3">
      Use React Bootstrap's <code>Table</code> component for simple, static tables.
      Best for read-only data without sorting, filtering, or pagination needs.
    </p>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom className="h-100">
          <OrkestraComponentCard.Header
            title="Basic Table"
            className="border-bottom"
          >
            <p className="mt-2 mb-0">
              Use <code>Table</code> component for basic bootstrap table with action buttons.
            </p>
          </OrkestraComponentCard.Header>
          <OrkestraComponentCard.Body
            code={basicTableCode}
            language="jsx"
            scope={{ ActionButton, Table }}
            noLight
            className="p-0"
          />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom className="h-100">
          <OrkestraComponentCard.Header
            title="Striped Table with Dropdown"
            className="border-bottom"
          >
            <p className="mt-2 mb-0">
              Use <code>striped</code> prop with <code>CardDropdown</code> for action menus.
            </p>
          </OrkestraComponentCard.Header>
          <OrkestraComponentCard.Body
            code={stripedTableCode}
            language="jsx"
            scope={{ ActionButton, CardDropdown, Table, Dropdown }}
            noLight
            className="p-0"
          />
        </OrkestraComponentCard>
      </Col>
    </Row>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom className="h-100">
          <OrkestraComponentCard.Header
            title="Hoverable Rows with Actions"
            className="border-bottom"
          >
            <p className="mt-2 mb-0">
              Use <code>hover-actions-trigger</code> class to show actions on row hover.
            </p>
          </OrkestraComponentCard.Header>
          <OrkestraComponentCard.Body
            code={hoverableCode}
            language="jsx"
            scope={{ FontAwesomeIcon, Button, Table }}
            noLight
            className="p-0"
            noInline
          />
        </OrkestraComponentCard>
      </Col>
      <Col lg={6}>
        <OrkestraComponentCard noGuttersBottom className="h-100">
          <OrkestraComponentCard.Header
            title="Responsive Table with Avatars"
            className="border-bottom"
          >
            <p className="mt-2 mb-0">
              Use <code>responsive</code> prop with Avatar and SubtleBadge components.
            </p>
          </OrkestraComponentCard.Header>
          <OrkestraComponentCard.Body
            code={responsiveTableCode}
            language="jsx"
            scope={{
              team3, team4, team2, team13, Avatar, FontAwesomeIcon, SubtleBadge, Table
            }}
            noLight
            className="p-0"
            noInline
          />
        </OrkestraComponentCard>
      </Col>
    </Row>

    {/* ========== ADVANCE TABLE SETUP ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="cogs" className="me-2" />
      AdvanceTable Components
    </h5>
    <p className="text-muted mb-3">
      Use <code>useAdvanceTable</code> hook with <code>AdvanceTableProvider</code> for
      sortable, filterable, and paginated data tables. Built on TanStack React Table v8.
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header title="Basic AdvanceTable Setup" light={false}>
        <p className="mt-2 mb-0">
          Core pattern: <code>useAdvanceTable</code> → <code>AdvanceTableProvider</code> → <code>AdvanceTable</code>
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={advanceTableBasicCode}
        scope={{
          useAdvanceTable, AdvanceTableProvider, AdvanceTable, AdvanceTableFooter
        }}
        language="jsx"
        noInline
        noLight
      />
    </OrkestraComponentCard>

    {/* ========== PAGINATION ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="pager" className="me-2" />
      Pagination
    </h5>
    <p className="text-muted mb-3">
      Two pagination styles: <code>AdvanceTableFooter</code> (prev/next with row info)
      or <code>AdvanceTablePagination</code> (numbered pages).
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header title="Numbered Pagination" light={false}>
        <p className="mt-2 mb-0">
          Use <code>AdvanceTablePagination</code> for numbered page navigation.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={paginationNumberingCode}
        scope={{
          useAdvanceTable, AdvanceTableProvider, AdvanceTable, AdvanceTablePagination
        }}
        language="jsx"
        noInline
        noLight
      />
    </OrkestraComponentCard>

    {/* ========== SEARCH & FILTERING ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="search" className="me-2" />
      Search & Filtering
    </h5>
    <p className="text-muted mb-3">
      Use <code>AdvanceTableSearchBox</code> for global search across all columns.
      For column-specific filters, use <code>setColumnFilters</code> from context.
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header
        title="Searchable Table"
        light={false}
        className="border-bottom border-200"
      >
        <p className="mt-2 mb-0">
          Global search filters all rows based on any matching cell value.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={searchableTableCode}
        scope={{
          useAdvanceTable, AdvanceTableProvider, AdvanceTable, AdvanceTableFooter,
          AdvanceTableSearchBox, Row, Col
        }}
        language="jsx"
        noInline
        noLight
      />
    </OrkestraComponentCard>

    {/* ========== ROW SELECTION ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="check-square" className="me-2" />
      Row Selection & Bulk Actions
    </h5>
    <p className="text-muted mb-3">
      Enable <code>selection: true</code> to add checkbox column.
      Use <code>getSelectedRowModel()</code> to access selected rows.
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header
        title="Selection with Bulk Actions"
        light={false}
        className="border-bottom border-200"
      >
        <p className="mt-2 mb-0">
          Checkbox selection with conditional bulk action dropdown.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={selectionCode}
        scope={{
          useAdvanceTable, AdvanceTableProvider, AdvanceTable, AdvanceTableFooter,
          useAdvanceTableContext, IconButton, Row, Col, Form, Button
        }}
        language="jsx"
        noInline
        noLight
      />
    </OrkestraComponentCard>

    {/* ========== CUSTOM CELLS ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="palette" className="me-2" />
      Custom Cell Rendering
    </h5>
    <p className="text-muted mb-3">
      Use the <code>cell</code> property in column definitions to render custom components
      like Avatars, Badges, Dropdowns, and formatted dates.
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header
        title="Custom Cell Examples"
        light={false}
        className="border-bottom border-200"
      >
        <p className="mt-2 mb-0">
          Avatar cells, Badge roles, Status indicators, and Action dropdowns.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={customCellCode}
        scope={{
          useAdvanceTable, AdvanceTableProvider, AdvanceTable,
          Avatar, Badge, SubtleBadge, FontAwesomeIcon, Dropdown,
          team2, team4, team13
        }}
        language="jsx"
        noInline
        noLight
      />
    </OrkestraComponentCard>

    {/* ========== PRODUCTION PATTERNS ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="industry" className="me-2" />
      Production Patterns
    </h5>
    <p className="text-muted mb-3">
      Complete table header pattern with search, filters, conditional bulk actions,
      and CSV export - as used in production tables like UserTable and VehicleTable.
    </p>

    <OrkestraComponentCard className="mb-3">
      <OrkestraComponentCard.Header
        title="Table Header Pattern"
        light={false}
        className="border-bottom border-200"
      >
        <p className="mt-2 mb-0">
          Production-ready header with search, filter dropdown, and conditional actions.
          See <code>src/pages/admin/users/UserTableHeader.tsx</code> for complete implementation.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={tableHeaderPatternCode}
        language="jsx"
        hidePreview
      />
    </OrkestraComponentCard>

    {/* ========== HOOK OPTIONS ========== */}
    <h5 className="mb-3 mt-5 fw-bold text-primary">
      <FontAwesomeIcon icon="sliders-h" className="me-2" />
      Hook Configuration Reference
    </h5>
    <p className="text-muted mb-3">
      Complete reference for <code>useAdvanceTable</code> hook options.
    </p>

    <OrkestraComponentCard className="mb-0">
      <OrkestraComponentCard.Header
        title="useAdvanceTable Options"
        light={false}
        className="border-bottom border-200"
      >
        <p className="mt-2 mb-0">
          All available configuration options for the useAdvanceTable hook.
        </p>
      </OrkestraComponentCard.Header>
      <OrkestraComponentCard.Body
        code={hookOptionsCode}
        language="tsx"
        hidePreview
      />
    </OrkestraComponentCard>
  </>
);

export default Tables;
