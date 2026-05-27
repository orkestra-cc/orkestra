import { useMemo, useState } from 'react';
import { Alert, Card, Col, Form, InputGroup, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import { useGetAdminNavigationQuery } from 'store/api/navigationAdminApi';
import type { AdminNavItem } from 'types/navigation';
import NavigationTree from './NavigationTree';
import NavigationDetailPanel from './NavigationDetailPanel';

// NavigationAdminPage — Phase 1 + 2 of the navigation admin epic. Two
// panes: a tree of every nav item every module declared (left) and a
// detail panel for the selected node (right). The tree supports
// drag-to-reorder within each parent via @dnd-kit; the role-matrix
// toggle overlays a 6-chip strip per row so operators can audit
// visibility without leaving the page.

const ROLE_MATRIX_LOCALSTORAGE_KEY = 'orkestra.navadmin.matrix';

const readMatrixDefault = () => {
  if (typeof window === 'undefined') return false;
  return window.localStorage.getItem(ROLE_MATRIX_LOCALSTORAGE_KEY) === '1';
};

const NavigationAdminPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetAdminNavigationQuery();
  const [selected, setSelected] = useState<AdminNavItem | null>(null);
  const [showMatrix, setShowMatrix] = useState<boolean>(readMatrixDefault);
  const [moduleFilter, setModuleFilter] = useState<string>('');
  const [search, setSearch] = useState<string>('');

  const moduleNames = useMemo(() => {
    if (!data) return [] as string[];
    const set = new Set<string>();
    const walk = (items: AdminNavItem[]) => {
      items.forEach(it => {
        if (it.moduleName) set.add(it.moduleName);
        if (it.children) walk(it.children);
      });
    };
    data.realms.forEach(r => r.sections.forEach(s => walk(s.items)));
    return Array.from(set).sort();
  }, [data]);

  const toggleMatrix = (next: boolean) => {
    setShowMatrix(next);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(
        ROLE_MATRIX_LOCALSTORAGE_KEY,
        next ? '1' : '0'
      );
    }
  };

  if (isLoading) {
    return (
      <Card>
        <Card.Body>{t('adminNavigation.loading')}</Card.Body>
      </Card>
    );
  }
  if (error || !data) {
    return <Alert variant="danger">{t('adminNavigation.loadFailed')}</Alert>;
  }

  return (
    <>
      <PageHeader
        title={t('adminNavigation.title')}
        description={t('adminNavigation.description')}
        className="mb-3"
      />

      <Card className="shadow-none border mb-3">
        <Card.Body className="d-flex flex-wrap align-items-center gap-3">
          <InputGroup style={{ maxWidth: 260 }}>
            <InputGroup.Text>
              <span className="text-muted small">
                {t('adminNavigation.filters.search')}
              </span>
            </InputGroup.Text>
            <Form.Control
              size="sm"
              type="search"
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder={t('adminNavigation.filters.searchPlaceholder')}
            />
          </InputGroup>

          <Form.Select
            size="sm"
            style={{ maxWidth: 220 }}
            value={moduleFilter}
            onChange={e => setModuleFilter(e.target.value)}
          >
            <option value="">{t('adminNavigation.filters.allModules')}</option>
            {moduleNames.map(m => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </Form.Select>

          <Form.Check
            type="switch"
            id="navadmin-matrix-toggle"
            label={t('adminNavigation.filters.showRoleMatrix')}
            checked={showMatrix}
            onChange={e => toggleMatrix(e.target.checked)}
            className="ms-auto"
          />
        </Card.Body>
      </Card>

      <Row className="g-3">
        <Col lg={8}>
          <Card className="shadow-none border">
            <Card.Body>
              <NavigationTree
                realms={data.realms}
                realmsParentKey={data.realmsParentKey}
                realmsOverridden={data.realmsOverridden}
                roles={data.roles}
                showRoleMatrix={showMatrix}
                moduleFilter={moduleFilter}
                search={search}
                selectedKey={selected?.itemKey ?? null}
                onSelect={setSelected}
              />
            </Card.Body>
          </Card>
        </Col>
        <Col lg={4}>
          <NavigationDetailPanel item={selected} roles={data.roles} />
        </Col>
      </Row>
    </>
  );
};

export default NavigationAdminPage;
