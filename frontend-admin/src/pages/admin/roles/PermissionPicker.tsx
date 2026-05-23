import { useMemo, useState } from 'react';
import {
  Accordion,
  Badge,
  Button,
  Form,
  InputGroup,
  Spinner
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Trans, useTranslation } from 'react-i18next';
import { useListPermissionsQuery, type Permission } from 'store/api/tenantApi';

interface Props {
  /** Currently-selected permission keys. Controlled from the parent. */
  selected: Set<string>;
  /** Called with the next set whenever a checkbox (or "select all") fires. */
  onChange: (next: Set<string>) => void;
  /** When true every input is rendered read-only (system-role edit view). */
  readOnly?: boolean;
}

/**
 * Permission-picker surface used by both the create and edit role modals.
 * The catalog is grouped by its module prefix and filtered live by a search
 * box that matches against the permission key and its description. Groups
 * with no visible permissions are hidden while a filter is active so the
 * accordion stays short.
 *
 * This component owns the catalog fetch + search state; the parent just
 * hands it a selected set and receives updates via onChange. That keeps the
 * form logic (name/description/isActive/diffing) in the modal that uses it.
 */
const PermissionPicker: React.FC<Props> = ({
  selected,
  onChange,
  readOnly
}) => {
  const { t } = useTranslation();
  const { data, isLoading } = useListPermissionsQuery();
  const [query, setQuery] = useState('');

  const grouped = useMemo(() => {
    const groups: Record<string, Permission[]> = {};
    for (const p of data?.permissions ?? []) {
      if (!groups[p.module]) groups[p.module] = [];
      groups[p.module].push(p);
    }
    for (const mod of Object.keys(groups)) {
      groups[mod].sort((a, b) => a.key.localeCompare(b.key));
    }
    return groups;
  }, [data]);

  const q = query.trim().toLowerCase();
  const filteredGroups = useMemo(() => {
    if (!q) return grouped;
    const out: Record<string, Permission[]> = {};
    for (const [mod, perms] of Object.entries(grouped)) {
      const hits = perms.filter(
        p =>
          p.key.toLowerCase().includes(q) ||
          p.description.toLowerCase().includes(q) ||
          mod.toLowerCase().includes(q)
      );
      if (hits.length > 0) out[mod] = hits;
    }
    return out;
  }, [grouped, q]);

  const sortedMods = useMemo(
    () => Object.keys(filteredGroups).sort(),
    [filteredGroups]
  );

  const toggle = (key: string) => {
    if (readOnly) return;
    const next = new Set(selected);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    onChange(next);
  };

  const toggleModule = (perms: Permission[]) => {
    if (readOnly) return;
    const next = new Set(selected);
    const allSelected = perms.every(p => next.has(p.key));
    if (allSelected) perms.forEach(p => next.delete(p.key));
    else perms.forEach(p => next.add(p.key));
    onChange(next);
  };

  const selectedCount = selected.size;
  const visibleCount = sortedMods.reduce(
    (sum, m) => sum + filteredGroups[m].length,
    0
  );
  const totalCount = data?.permissions.length ?? 0;

  // Open every matching group automatically while a filter is active, so a
  // single keystroke reveals its hits without another click.
  const openKeys = q ? sortedMods.map((_, i) => String(i)) : [];

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-2">
        <div className="fw-semibold">
          {t('adminRoles.permissionPicker.heading')}{' '}
          <Badge bg="primary" className="ms-1">
            {t('adminRoles.permissionPicker.selectedBadge', {
              count: selectedCount
            })}
          </Badge>
          {q && (
            <span className="text-muted small ms-2">
              {t('adminRoles.permissionPicker.filterSummary', {
                visible: visibleCount,
                total: totalCount
              })}
            </span>
          )}
        </div>
        {selectedCount > 0 && !readOnly && (
          <Button
            variant="link"
            size="sm"
            className="text-danger p-0"
            onClick={() => onChange(new Set())}
          >
            {t('adminRoles.permissionPicker.clearSelection')}
          </Button>
        )}
      </div>

      <InputGroup className="mb-2">
        <InputGroup.Text>
          <FontAwesomeIcon icon="search" className="text-muted" />
        </InputGroup.Text>
        <Form.Control
          type="search"
          placeholder={t('adminRoles.permissionPicker.searchPlaceholder')}
          value={query}
          onChange={e => setQuery(e.target.value)}
          aria-label={t('adminRoles.permissionPicker.searchAriaLabel')}
        />
        {query && (
          <Button variant="outline-secondary" onClick={() => setQuery('')}>
            {t('adminRoles.permissionPicker.clearSearch')}
          </Button>
        )}
      </InputGroup>

      {isLoading ? (
        <div className="text-center py-4">
          <Spinner animation="border" size="sm" />{' '}
          {t('adminRoles.permissionPicker.loading')}
        </div>
      ) : sortedMods.length === 0 ? (
        <div className="text-center text-muted py-4 fs-10">
          <FontAwesomeIcon icon="filter" className="me-1" />
          <Trans
            i18nKey="adminRoles.permissionPicker.noMatch"
            values={{ query }}
            components={{ code: <code /> }}
          />
        </div>
      ) : (
        <Accordion alwaysOpen activeKey={q ? openKeys : undefined}>
          {sortedMods.map((mod, idx) => {
            const perms = filteredGroups[mod];
            const selectedInMod = perms.filter(p => selected.has(p.key)).length;
            const allSelected = perms.every(p => selected.has(p.key));
            return (
              <Accordion.Item eventKey={String(idx)} key={mod}>
                <Accordion.Header>
                  <div className="d-flex justify-content-between align-items-center w-100 me-3">
                    <span>
                      <strong>{mod}</strong>{' '}
                      <span className="text-muted small">
                        {t(
                          perms.length === 1
                            ? 'adminRoles.permissionPicker.permsCountOne'
                            : 'adminRoles.permissionPicker.permsCountOther',
                          { count: perms.length }
                        )}
                      </span>
                    </span>
                    {selectedInMod > 0 && (
                      <Badge
                        bg={allSelected ? 'success' : 'primary'}
                        className="ms-2"
                      >
                        {selectedInMod}/{perms.length}
                      </Badge>
                    )}
                  </div>
                </Accordion.Header>
                <Accordion.Body className="py-2">
                  <Form.Check
                    type="checkbox"
                    id={`perm-picker-all-${mod}`}
                    className="mb-2 fw-semibold"
                    label={
                      <span className="text-muted">
                        {t(
                          allSelected
                            ? 'adminRoles.permissionPicker.deselectAll'
                            : 'adminRoles.permissionPicker.selectAll',
                          { module: mod }
                        )}
                      </span>
                    }
                    checked={allSelected}
                    onChange={() => toggleModule(perms)}
                    disabled={readOnly}
                  />
                  <hr className="my-2" />
                  {perms.map(p => (
                    <Form.Check
                      key={p.key}
                      type="checkbox"
                      id={`perm-picker-${p.key}`}
                      className="mb-1"
                      checked={selected.has(p.key)}
                      onChange={() => toggle(p.key)}
                      disabled={readOnly}
                      label={
                        <span>
                          <code className="me-2">{p.key}</code>
                          {p.system && (
                            <Badge bg="warning" text="dark" className="me-2">
                              {t('adminRoles.permissionPicker.systemBadge')}
                            </Badge>
                          )}
                          <span className="text-muted small">
                            {p.description}
                          </span>
                        </span>
                      }
                    />
                  ))}
                </Accordion.Body>
              </Accordion.Item>
            );
          })}
        </Accordion>
      )}
    </>
  );
};

export default PermissionPicker;
