import { useMemo, useState } from 'react';
import {
  Alert,
  Button,
  ButtonGroup,
  Card,
  Col,
  Form,
  InputGroup,
  Row
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import { useGetAdminNavigationQuery } from 'store/api/navigationAdminApi';
import type { AdminNavItem, TenantKind } from 'types/navigation';
import NavigationTree from './NavigationTree';
import NavigationDetailPanel from './NavigationDetailPanel';

// NavigationAdminPage — the operator's navigation audit + reorder surface.
// Left pane: the full unfiltered tree of every nav item every module declared,
// drag-to-reorder within each parent. The "Show role matrix" toggle overlays a
// truthful per-row visibility strip; the tenant-kind switch and "View as"
// dropdown let an operator see exactly who sees what, and preview the sidebar
// as any (role × tenant-kind) persona — all driven by the same server-computed
// visibility the live sidebar uses, so the audit never lies.

const MATRIX_KEY = 'orkestra.navadmin.matrix';
const TENANT_KEY = 'orkestra.navadmin.tenantkind';
const VIEWAS_KEY = 'orkestra.navadmin.viewas';

const readLS = (key: string, fallback: string): string => {
  if (typeof window === 'undefined') return fallback;
  return window.localStorage.getItem(key) ?? fallback;
};
const writeLS = (key: string, value: string) => {
  if (typeof window !== 'undefined') window.localStorage.setItem(key, value);
};

const NavigationAdminPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, error } = useGetAdminNavigationQuery();
  const [selected, setSelected] = useState<AdminNavItem | null>(null);
  const [showMatrix, setShowMatrix] = useState<boolean>(
    () => readLS(MATRIX_KEY, '0') === '1'
  );
  const [tenantKind, setTenantKind] = useState<TenantKind>(() =>
    readLS(TENANT_KEY, 'internal') === 'external' ? 'external' : 'internal'
  );
  const [viewAs, setViewAs] = useState<string>(() => readLS(VIEWAS_KEY, ''));
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
    writeLS(MATRIX_KEY, next ? '1' : '0');
  };
  const pickTenant = (next: TenantKind) => {
    setTenantKind(next);
    writeLS(TENANT_KEY, next);
  };
  const pickViewAs = (next: string) => {
    setViewAs(next);
    writeLS(VIEWAS_KEY, next);
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

  // "View as" only enables simulation when the picked role is one the server
  // actually knows about; an empty selection (or a stale localStorage value)
  // means preview is off.
  const simulateRole = viewAs && data.roles.includes(viewAs) ? viewAs : null;
  const tenantLabel = t(
    `adminNavigation.filters.tenant${
      tenantKind === 'external' ? 'External' : 'Internal'
    }`
  );

  return (
    <>
      <PageHeader
        title={t('adminNavigation.title')}
        description={t('adminNavigation.description')}
        className="mb-3"
      />

      <Card className="shadow-none border mb-3">
        <Card.Body className="d-flex flex-wrap align-items-center gap-3">
          <InputGroup style={{ maxWidth: 240 }}>
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
            style={{ maxWidth: 200 }}
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

          {/* Tenant-kind switch — drives both the matrix chips and the preview. */}
          <div className="d-flex align-items-center gap-2">
            <span className="text-muted small">
              {t('adminNavigation.filters.tenantKind')}
            </span>
            <ButtonGroup size="sm">
              <Button
                variant={
                  tenantKind === 'internal' ? 'primary' : 'outline-secondary'
                }
                onClick={() => pickTenant('internal')}
              >
                {t('adminNavigation.filters.tenantInternal')}
              </Button>
              <Button
                variant={
                  tenantKind === 'external' ? 'primary' : 'outline-secondary'
                }
                onClick={() => pickTenant('external')}
              >
                {t('adminNavigation.filters.tenantExternal')}
              </Button>
            </ButtonGroup>
          </div>

          {/* View as — preview the sidebar as a specific role. */}
          <InputGroup size="sm" style={{ maxWidth: 220 }}>
            <InputGroup.Text>
              <span className="text-muted small">
                {t('adminNavigation.viewAs.label')}
              </span>
            </InputGroup.Text>
            <Form.Select
              value={viewAs}
              onChange={e => pickViewAs(e.target.value)}
            >
              <option value="">{t('adminNavigation.viewAs.off')}</option>
              {data.roles.map(r => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </Form.Select>
          </InputGroup>

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

      {simulateRole && (
        <Alert
          variant="info"
          className="d-flex align-items-center justify-content-between py-2"
        >
          <span className="small mb-0">
            {t('adminNavigation.viewAs.banner', {
              role: simulateRole,
              tenant: tenantLabel
            })}
          </span>
          <Button
            size="sm"
            variant="outline-secondary"
            onClick={() => pickViewAs('')}
          >
            {t('adminNavigation.viewAs.clear')}
          </Button>
        </Alert>
      )}

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
                tenantKind={tenantKind}
                simulateRole={simulateRole}
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
