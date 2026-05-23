// Marketing Scoring admin — Phase 2.
//
// Two-pane layout: left side lists the tenant's score profiles with
// active/inactive toggles + version badges; right side shows the
// selected profile's form + a top-20 leaderboard preview.
//
// The rule editor is intentionally a JSON textarea for Phase 2 —
// the schema is documented inline and matches the example in
// docs/plans/marketing-addon/schemas/marketing_score_profiles.md.
// A structured rule-builder UI is a Phase 2 follow-up; operators
// editing rules today are internal Tier-1 staff comfortable with
// JSON.

import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Form,
  FormControl,
  InputGroup
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import type { ColumnDef } from '@tanstack/react-table';
import { useTranslation } from 'react-i18next';
import useAdvanceTable from 'hooks/ui/useAdvanceTable';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import ExportCsvButton from 'components/common/advance-table/ExportCsvButton';
import { formatDateForCSV } from 'utils/csvExport';
import {
  useCreateScoreProfileMutation,
  useDeleteScoreProfileMutation,
  useGetProfileLeaderboardQuery,
  useListScoreProfilesQuery,
  useReplaceScoreProfileMutation
} from 'store/api/marketingApi';
import type {
  DecayFn,
  LeaderboardEntry,
  ProfileFilter,
  ScoreProfile,
  ScoreProfilePayload,
  ScoreRule
} from 'types/marketing';

// Boilerplate JSON loaded into a blank "Create" form so operators
// have something to edit rather than starting from scratch. Mirrors
// the example profile in the schema doc.
const BLANK_RULES_JSON = JSON.stringify(
  [
    {
      activityKind: 'meeting_held',
      points: 50
    },
    {
      activityKind: 'form_submitted',
      points: 30
    },
    {
      activityKind: 'email_opened',
      points: 1,
      cap: 10,
      decay: { fn: 'linear', windowDays: 90 }
    }
  ],
  null,
  2
);

const BLANK_FILTERS_JSON = JSON.stringify(
  {
    tagsInclude: [],
    tagsExclude: [],
    customFieldFilters: {}
  },
  null,
  2
);

const BLANK_DEFAULT_DECAY_JSON = JSON.stringify({ fn: 'none' }, null, 2);

interface FormState {
  uuid: string | null; // null for new profile
  name: string;
  description: string;
  active: boolean;
  rulesJson: string;
  filtersJson: string;
  defaultDecayJson: string;
  parseError: string | null;
}

const BLANK_FORM: FormState = {
  uuid: null,
  name: '',
  description: '',
  active: true,
  rulesJson: BLANK_RULES_JSON,
  filtersJson: BLANK_FILTERS_JSON,
  defaultDecayJson: BLANK_DEFAULT_DECAY_JSON,
  parseError: null
};

const profileToForm = (p: ScoreProfile): FormState => ({
  uuid: p.uuid,
  name: p.name,
  description: p.description ?? '',
  active: p.active,
  rulesJson: JSON.stringify(p.rules, null, 2),
  filtersJson: JSON.stringify(p.filters ?? null, null, 2),
  defaultDecayJson: JSON.stringify(p.defaultDecay ?? { fn: 'none' }, null, 2),
  parseError: null
});

const ScoringPage: React.FC = () => {
  const { t } = useTranslation();
  const [selectedUuid, setSelectedUuid] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<FormState>(BLANK_FORM);
  const [sidebarFilter, setSidebarFilter] = useState('');

  const { data: profiles, isLoading } = useListScoreProfilesQuery(undefined);
  const [createProfile, createState] = useCreateScoreProfileMutation();
  const [replaceProfile, replaceState] = useReplaceScoreProfileMutation();
  const [deleteProfile] = useDeleteScoreProfileMutation();

  const selected = useMemo(
    () => profiles?.items?.find(p => p.uuid === selectedUuid) ?? null,
    [profiles, selectedUuid]
  );

  const visibleProfiles = useMemo(() => {
    const all = profiles?.items ?? [];
    const needle = sidebarFilter.trim().toLowerCase();
    if (!needle) return all;
    return all.filter(p =>
      `${p.name} ${p.description ?? ''}`.toLowerCase().includes(needle)
    );
  }, [profiles, sidebarFilter]);

  // Sync form when the user picks a different profile or enters
  // create mode. Operators editing without saving still see their
  // edits because the state lives on `form`, not on the query.
  useEffect(() => {
    if (creating) {
      setForm(BLANK_FORM);
    } else if (selected) {
      setForm(profileToForm(selected));
    }
  }, [selected, creating]);

  const onSelect = (uuid: string) => {
    setCreating(false);
    setSelectedUuid(uuid);
  };

  const onStartCreate = () => {
    setCreating(true);
    setSelectedUuid(null);
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    let rules: ScoreRule[];
    let filters: ProfileFilter | undefined;
    let defaultDecay: DecayFn | undefined;
    try {
      rules = JSON.parse(form.rulesJson) as ScoreRule[];
      const parsedFilters = JSON.parse(form.filtersJson);
      filters =
        parsedFilters && Object.keys(parsedFilters).length > 0
          ? (parsedFilters as ProfileFilter)
          : undefined;
      const parsedDecay = JSON.parse(form.defaultDecayJson);
      defaultDecay =
        parsedDecay && parsedDecay.fn !== 'none'
          ? (parsedDecay as DecayFn)
          : undefined;
    } catch (err) {
      setForm(f => ({
        ...f,
        parseError: t('marketing.scoring.form.parseError', {
          msg: (err as Error).message
        })
      }));
      return;
    }

    const payload: ScoreProfilePayload = {
      name: form.name,
      description: form.description || undefined,
      active: form.active,
      rules,
      filters,
      defaultDecay
    };

    try {
      if (creating) {
        const created = await createProfile(payload).unwrap();
        setCreating(false);
        setSelectedUuid(created.uuid);
      } else if (form.uuid) {
        await replaceProfile({ id: form.uuid, body: payload }).unwrap();
      }
    } catch {
      // RTK Query surfaces error via createState/replaceState
    }
  };

  const onDelete = async () => {
    if (!form.uuid) return;
    if (!confirm(t('marketing.scoring.confirmDelete', { name: form.name }))) {
      return;
    }
    try {
      await deleteProfile(form.uuid).unwrap();
      setSelectedUuid(null);
      setForm(BLANK_FORM);
    } catch {
      // ignore
    }
  };

  return (
    <>
      <div className="mb-3 d-flex align-items-baseline gap-3">
        <h3 className="fw-normal mb-0">{t('marketing.scoring.title')}</h3>
        <small className="text-muted">{t('marketing.scoring.subtitle')}</small>
      </div>

      <div className="row g-3">
        {/* Profile list */}
        <div className="col-md-4">
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <strong>{t('marketing.scoring.profilesHeader')}</strong>
              <Button
                size="sm"
                variant="outline-primary"
                onClick={onStartCreate}
              >
                {t('marketing.scoring.newProfile')}
              </Button>
            </Card.Header>
            <div className="px-3 py-2 border-bottom border-200">
              <InputGroup className="position-relative">
                <FormControl
                  size="sm"
                  type="search"
                  value={sidebarFilter}
                  onChange={e => setSidebarFilter(e.target.value)}
                  placeholder={t('marketing.scoring.searchPlaceholder')}
                  aria-label={t('marketing.scoring.searchPlaceholder')}
                  className="shadow-none"
                />
                <Button
                  size="sm"
                  variant="outline-secondary"
                  className="border-300 hover-border-secondary"
                  tabIndex={-1}
                >
                  <FontAwesomeIcon icon="search" className="fs-10" />
                </Button>
              </InputGroup>
            </div>
            <Card.Body className="p-0">
              {isLoading && (
                <p className="text-muted p-3 mb-0">
                  {t('marketing.scoring.loading')}
                </p>
              )}
              {!isLoading && !profiles?.items?.length && (
                <p className="text-muted p-3 mb-0">
                  {t('marketing.scoring.empty')}
                </p>
              )}
              {!isLoading &&
                !!profiles?.items?.length &&
                !visibleProfiles.length && (
                  <p className="text-muted p-3 mb-0">
                    {t('marketing.scoring.noMatches')}
                  </p>
                )}
              {visibleProfiles.map(p => (
                <button
                  key={p.uuid}
                  type="button"
                  className={`list-group-item list-group-item-action border-0 border-bottom ${
                    p.uuid === selectedUuid ? 'active' : ''
                  }`}
                  onClick={() => onSelect(p.uuid)}
                >
                  <div className="d-flex justify-content-between align-items-center">
                    <span>{p.name}</span>
                    <span className="text-muted small">v{p.version}</span>
                  </div>
                  <div>
                    {p.active ? (
                      <Badge bg="success" pill>
                        {t('marketing.scoring.badgeActive')}
                      </Badge>
                    ) : (
                      <Badge bg="secondary" pill>
                        {t('marketing.scoring.badgeInactive')}
                      </Badge>
                    )}{' '}
                    <small className="text-muted">
                      {p.rules.length} {t('marketing.scoring.rulesSuffix')}
                    </small>
                  </div>
                </button>
              ))}
            </Card.Body>
          </Card>
        </div>

        {/* Editor + leaderboard */}
        <div className="col-md-8">
          {!creating && !selected ? (
            <Card>
              <Card.Body>
                <p className="text-muted mb-0">
                  {t('marketing.scoring.selectPrompt')}
                </p>
              </Card.Body>
            </Card>
          ) : (
            <Form onSubmit={onSubmit}>
              <Card>
                <Card.Header>
                  <strong>
                    {creating
                      ? t('marketing.scoring.form.newTitle')
                      : t('marketing.scoring.form.editTitle', {
                          name: form.name
                        })}
                  </strong>
                </Card.Header>
                <Card.Body>
                  {(createState.error || replaceState.error) && (
                    <Alert variant="danger">
                      {t('marketing.scoring.form.saveError')}
                    </Alert>
                  )}
                  {form.parseError && (
                    <Alert variant="warning">{form.parseError}</Alert>
                  )}
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.scoring.form.nameLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      value={form.name}
                      onChange={e =>
                        setForm(f => ({ ...f, name: e.target.value }))
                      }
                      placeholder="hot_lead"
                      required
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.scoring.form.nameHelp')}
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.scoring.form.descriptionLabel')}
                    </Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={2}
                      value={form.description}
                      onChange={e =>
                        setForm(f => ({ ...f, description: e.target.value }))
                      }
                    />
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Check
                      type="checkbox"
                      label={t('marketing.scoring.form.activeLabel')}
                      checked={form.active}
                      onChange={e =>
                        setForm(f => ({ ...f, active: e.target.checked }))
                      }
                    />
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.scoring.form.rulesLabel')}
                    </Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={12}
                      className="font-monospace"
                      value={form.rulesJson}
                      onChange={e =>
                        setForm(f => ({ ...f, rulesJson: e.target.value }))
                      }
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.scoring.form.rulesHelp')}
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.scoring.form.filtersLabel')}
                    </Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={5}
                      className="font-monospace"
                      value={form.filtersJson}
                      onChange={e =>
                        setForm(f => ({ ...f, filtersJson: e.target.value }))
                      }
                    />
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.scoring.form.defaultDecayLabel')}
                    </Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={3}
                      className="font-monospace"
                      value={form.defaultDecayJson}
                      onChange={e =>
                        setForm(f => ({
                          ...f,
                          defaultDecayJson: e.target.value
                        }))
                      }
                    />
                  </Form.Group>
                </Card.Body>
                <Card.Footer className="d-flex justify-content-between">
                  <div>
                    {!creating && form.uuid && (
                      <Button
                        variant="outline-danger"
                        size="sm"
                        type="button"
                        onClick={onDelete}
                      >
                        {t('marketing.scoring.form.delete')}
                      </Button>
                    )}
                  </div>
                  <Button
                    type="submit"
                    variant="primary"
                    disabled={createState.isLoading || replaceState.isLoading}
                  >
                    {createState.isLoading || replaceState.isLoading
                      ? t('marketing.scoring.form.saving')
                      : t('marketing.scoring.form.save')}
                  </Button>
                </Card.Footer>
              </Card>

              {selected && <LeaderboardPreview profileUuid={selected.uuid} />}
            </Form>
          )}
        </div>
      </div>
    </>
  );
};

interface LeaderboardPreviewProps {
  profileUuid: string;
}

const LeaderboardPreview: React.FC<LeaderboardPreviewProps> = ({
  profileUuid
}) => {
  const { t } = useTranslation();
  const { data, isLoading } = useGetProfileLeaderboardQuery({
    id: profileUuid,
    limit: 20,
    applicableOnly: true
  });

  const items = data?.items ?? [];

  // The leaderboard is capped at 20 server-side, so sortable headers
  // are enough — no global search or pagination needed.
  const columns = useMemo<ColumnDef<LeaderboardEntry>[]>(
    () => [
      {
        id: 'value',
        accessorKey: 'value',
        header: t('marketing.scoring.leaderboard.colValue'),
        cell: ({ getValue }) => (
          <strong>{(getValue() as number).toFixed(2)}</strong>
        ),
        meta: {
          headerProps: {
            className: 'text-900',
            style: { width: 100 }
          }
        }
      },
      {
        id: 'person',
        accessorKey: 'personUuid',
        header: t('marketing.scoring.leaderboard.colPerson'),
        enableSorting: false,
        cell: ({ getValue }) => {
          const uuid = String(getValue());
          return (
            <a href={`/marketing/contacts/${uuid}?tab=scores`}>
              <code>{uuid.slice(0, 8)}</code>
            </a>
          );
        },
        meta: { headerProps: { className: 'text-900' } }
      },
      {
        id: 'activityCount',
        accessorKey: 'activityCount',
        header: t('marketing.scoring.leaderboard.colActivities'),
        cell: ({ getValue }) => (
          <small className="text-muted">{getValue() as number}</small>
        ),
        meta: {
          headerProps: {
            className: 'text-900',
            style: { width: 120 }
          }
        }
      }
    ],
    [t]
  );

  const table = useAdvanceTable<LeaderboardEntry>({
    data: items,
    columns,
    sortable: true,
    pagination: false,
    initialState: { sorting: [{ id: 'value', desc: true }] }
  });

  return (
    <Card className="mt-3">
      <Card.Header>
        <strong>{t('marketing.scoring.leaderboard.title')}</strong>
        <small className="text-muted ms-2">
          {t('marketing.scoring.leaderboard.subtitle')}
        </small>
      </Card.Header>
      <Card.Body className="p-0">
        {isLoading ? (
          <p className="text-muted p-3 mb-0">
            {t('marketing.scoring.leaderboard.loading')}
          </p>
        ) : !items.length ? (
          <p className="text-muted p-3 mb-0">
            {t('marketing.scoring.leaderboard.empty')}
          </p>
        ) : (
          <AdvanceTableProvider {...table}>
            <div className="d-flex justify-content-end px-x1 py-2 border-bottom border-200">
              <ExportCsvButton<LeaderboardEntry>
                filename={`marketing_leaderboard_${profileUuid.slice(0, 8)}`}
                buildRow={e => ({
                  PersonUUID: e.personUuid,
                  Value: e.value.toFixed(2),
                  Applicable: e.applicable ? 'yes' : 'no',
                  Stale: e.stale ? 'yes' : 'no',
                  ActivityCount: e.activityCount,
                  LastActivityAt: formatDateForCSV(e.lastActivityAt),
                  AsOf: formatDateForCSV(e.asOf),
                  ComputedAt: formatDateForCSV(e.computedAt),
                  ProfileVersion: e.profileVersion
                })}
              />
            </div>
            <AdvanceTable
              headerClassName="bg-body-tertiary align-middle"
              rowClassName="align-middle"
              tableProps={{
                size: 'sm',
                className: 'fs-10 mb-0 overflow-hidden'
              }}
            />
          </AdvanceTableProvider>
        )}
      </Card.Body>
    </Card>
  );
};

export default ScoringPage;
