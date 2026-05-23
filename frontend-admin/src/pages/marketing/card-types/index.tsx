// Marketing Card Types admin — Phase 4.
//
// Two-pane layout mirroring /marketing/scoring: profile-style list on
// the left (key + active badge), create/edit form on the right. Form
// fields map 1:1 to backend models.CardType:
//   - key                     — operator-facing slug, unique per tenant
//   - displayName             — human label
//   - description             — optional markdown
//   - codeFormat              — template grammar w/ live preview
//   - tiers                   — comma-separated
//   - defaultBenefits         — comma-separated
//   - allowMultiplePerPerson  — checkbox
//   - active                  — checkbox
//
// Code-format preview is rendered client-side via a thin emulator —
// no backend round-trip per keystroke. The emulator omits `{seq:N}`
// (server-only because it walks marketing_card_sequences) and shows a
// "(server-generated)" placeholder; everything else is locally
// reproducible.

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
import { useTranslation } from 'react-i18next';
import {
  useCreateCardTypeMutation,
  useDeleteCardTypeMutation,
  useListCardTypesQuery,
  useUpdateCardTypeMutation
} from 'store/api/marketingApi';
import type { CardType, CardTypePayload } from 'types/marketing';

interface FormState {
  uuid: string | null;
  key: string;
  displayName: string;
  description: string;
  codeFormat: string;
  tiersCsv: string;
  defaultBenefitsCsv: string;
  allowMultiplePerPerson: boolean;
  active: boolean;
}

const BLANK_FORM: FormState = {
  uuid: null,
  key: '',
  displayName: '',
  description: '',
  codeFormat: 'CARD-{YYYY}-{seq:5}',
  tiersCsv: '',
  defaultBenefitsCsv: '',
  allowMultiplePerPerson: false,
  active: true
};

const typeToForm = (c: CardType): FormState => ({
  uuid: c.uuid,
  key: c.key,
  displayName: c.displayName,
  description: c.description ?? '',
  codeFormat: c.codeFormat,
  tiersCsv: (c.tiers ?? []).join(', '),
  defaultBenefitsCsv: (c.defaultBenefits ?? []).join(', '),
  allowMultiplePerPerson: c.allowMultiplePerPerson,
  active: c.active
});

// previewCodeFormat is a deliberately-loose client-side renderer of
// the backend grammar in services/card_code_format.go. It supports
// the date placeholders + `{rand:N}` exactly; `{seq:N}` is rendered
// as the literal "{seq:N}" wrapped in a parenthetical hint because
// only the server can advance the per-(tenant, cardType) counter.
const previewCodeFormat = (template: string): string => {
  const now = new Date();
  const yyyy = String(now.getUTCFullYear()).padStart(4, '0');
  const yy = yyyy.slice(2);
  const mm = String(now.getUTCMonth() + 1).padStart(2, '0');
  const dd = String(now.getUTCDate()).padStart(2, '0');
  const RAND_ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'; // Crockford-Base32 (no I/L/O/U)

  return template.replace(/\{([^}]+)\}/g, (_match, inner: string) => {
    if (inner === 'YYYY') return yyyy;
    if (inner === 'YY') return yy;
    if (inner === 'MM') return mm;
    if (inner === 'DD') return dd;
    const randMatch = inner.match(/^rand:(\d+)$/);
    if (randMatch) {
      const n = Math.min(Math.max(parseInt(randMatch[1], 10), 1), 12);
      let out = '';
      for (let i = 0; i < n; i++) {
        out += RAND_ALPHABET[Math.floor(Math.random() * RAND_ALPHABET.length)];
      }
      return out;
    }
    const seqMatch = inner.match(/^seq:(\d+)$/);
    if (seqMatch) {
      const n = Math.min(Math.max(parseInt(seqMatch[1], 10), 1), 12);
      return '0'.repeat(n - 1) + 'N'; // hint that the server fills this
    }
    return `{${inner}}`;
  });
};

const csvToList = (raw: string): string[] =>
  raw
    .split(',')
    .map(s => s.trim())
    .filter(Boolean);

const CardTypesPage: React.FC = () => {
  const { t } = useTranslation();
  const [selectedUuid, setSelectedUuid] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<FormState>(BLANK_FORM);
  const [sidebarFilter, setSidebarFilter] = useState('');

  const { data: cardTypes, isLoading } = useListCardTypesQuery(undefined);
  const [createCardType, createState] = useCreateCardTypeMutation();
  const [updateCardType, updateState] = useUpdateCardTypeMutation();
  const [deleteCardType] = useDeleteCardTypeMutation();

  const selected = useMemo(
    () => cardTypes?.items?.find(c => c.uuid === selectedUuid) ?? null,
    [cardTypes, selectedUuid]
  );

  // Local-only filter — substring match across the three operator-facing
  // fields. Stays in component state so deep links to a specific card type
  // (when those land) won't have to round-trip through the URL.
  const visibleCardTypes = useMemo(() => {
    const all = cardTypes?.items ?? [];
    const needle = sidebarFilter.trim().toLowerCase();
    if (!needle) return all;
    return all.filter(c =>
      `${c.displayName} ${c.key} ${c.codeFormat}`.toLowerCase().includes(needle)
    );
  }, [cardTypes, sidebarFilter]);

  useEffect(() => {
    if (creating) setForm(BLANK_FORM);
    else if (selected) setForm(typeToForm(selected));
  }, [selected, creating]);

  const codePreview = useMemo(
    () => previewCodeFormat(form.codeFormat),
    [form.codeFormat]
  );

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
    const payload: CardTypePayload = {
      key: form.key,
      displayName: form.displayName,
      description: form.description || undefined,
      codeFormat: form.codeFormat,
      tiers: csvToList(form.tiersCsv),
      defaultBenefits: csvToList(form.defaultBenefitsCsv),
      allowMultiplePerPerson: form.allowMultiplePerPerson,
      active: form.active
    };
    try {
      if (creating) {
        const created = await createCardType(payload).unwrap();
        setCreating(false);
        setSelectedUuid(created.uuid);
      } else if (form.uuid) {
        await updateCardType({ id: form.uuid, patch: payload }).unwrap();
      }
    } catch {
      /* error surfaced via createState / updateState */
    }
  };

  const onDelete = async () => {
    if (!form.uuid) return;
    if (
      !confirm(
        t('marketing.cardTypes.confirmDelete', { name: form.displayName })
      )
    ) {
      return;
    }
    try {
      await deleteCardType(form.uuid).unwrap();
      setSelectedUuid(null);
      setForm(BLANK_FORM);
    } catch {
      /* error surfaced via mutation */
    }
  };

  return (
    <>
      <div className="mb-3 d-flex align-items-baseline gap-3">
        <h3 className="fw-normal mb-0">{t('marketing.cardTypes.title')}</h3>
        <small className="text-muted">
          {t('marketing.cardTypes.subtitle')}
        </small>
      </div>

      <div className="row g-3">
        <div className="col-md-4">
          <Card>
            <Card.Header className="d-flex justify-content-between align-items-center">
              <strong>{t('marketing.cardTypes.listHeader')}</strong>
              <Button
                size="sm"
                variant="outline-primary"
                onClick={onStartCreate}
              >
                {t('marketing.cardTypes.new')}
              </Button>
            </Card.Header>
            <div className="px-3 py-2 border-bottom border-200">
              <InputGroup className="position-relative">
                <FormControl
                  size="sm"
                  type="search"
                  value={sidebarFilter}
                  onChange={e => setSidebarFilter(e.target.value)}
                  placeholder={t('marketing.cardTypes.searchPlaceholder')}
                  aria-label={t('marketing.cardTypes.searchPlaceholder')}
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
                  {t('marketing.cardTypes.loading')}
                </p>
              )}
              {!isLoading && !cardTypes?.items?.length && (
                <p className="text-muted p-3 mb-0">
                  {t('marketing.cardTypes.empty')}
                </p>
              )}
              {!isLoading &&
                !!cardTypes?.items?.length &&
                !visibleCardTypes.length && (
                  <p className="text-muted p-3 mb-0">
                    {t('marketing.cardTypes.noMatches')}
                  </p>
                )}
              {visibleCardTypes.map(c => (
                <button
                  key={c.uuid}
                  type="button"
                  className={`list-group-item list-group-item-action border-0 border-bottom ${
                    c.uuid === selectedUuid ? 'active' : ''
                  }`}
                  onClick={() => onSelect(c.uuid)}
                >
                  <div className="d-flex justify-content-between align-items-center">
                    <span>
                      <strong>{c.displayName}</strong>
                      <code className="ms-2 text-muted small">{c.key}</code>
                    </span>
                    {c.active ? (
                      <Badge bg="success" pill>
                        {t('marketing.cardTypes.badgeActive')}
                      </Badge>
                    ) : (
                      <Badge bg="secondary" pill>
                        {t('marketing.cardTypes.badgeInactive')}
                      </Badge>
                    )}
                  </div>
                  <div className="small text-muted">
                    <code>{c.codeFormat}</code>
                    {c.allowMultiplePerPerson && (
                      <span className="ms-2">
                        · {t('marketing.cardTypes.allowMultipleShort')}
                      </span>
                    )}
                  </div>
                </button>
              ))}
            </Card.Body>
          </Card>
        </div>

        <div className="col-md-8">
          {!creating && !selected ? (
            <Card>
              <Card.Body>
                <p className="text-muted mb-0">
                  {t('marketing.cardTypes.selectPrompt')}
                </p>
              </Card.Body>
            </Card>
          ) : (
            <Form onSubmit={onSubmit}>
              <Card>
                <Card.Header>
                  <strong>
                    {creating
                      ? t('marketing.cardTypes.form.newTitle')
                      : t('marketing.cardTypes.form.editTitle', {
                          name: form.displayName
                        })}
                  </strong>
                </Card.Header>
                <Card.Body>
                  {(createState.error || updateState.error) && (
                    <Alert variant="danger">
                      {t('marketing.cardTypes.form.saveError')}
                    </Alert>
                  )}
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.cardTypes.form.keyLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      value={form.key}
                      onChange={e =>
                        setForm(f => ({ ...f, key: e.target.value }))
                      }
                      placeholder="premium_member"
                      required
                      disabled={!creating}
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.cardTypes.form.keyHelp')}
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.cardTypes.form.displayNameLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      value={form.displayName}
                      onChange={e =>
                        setForm(f => ({ ...f, displayName: e.target.value }))
                      }
                      placeholder="Premium Member"
                      required
                    />
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.cardTypes.form.descriptionLabel')}
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
                    <Form.Label>
                      {t('marketing.cardTypes.form.codeFormatLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      className="font-monospace"
                      value={form.codeFormat}
                      onChange={e =>
                        setForm(f => ({ ...f, codeFormat: e.target.value }))
                      }
                      required
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.cardTypes.form.codeFormatHelp')}{' '}
                      <code>{codePreview}</code>
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.cardTypes.form.tiersLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      value={form.tiersCsv}
                      onChange={e =>
                        setForm(f => ({ ...f, tiersCsv: e.target.value }))
                      }
                      placeholder="silver, gold, platinum"
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.cardTypes.form.tiersHelp')}
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Label>
                      {t('marketing.cardTypes.form.benefitsLabel')}
                    </Form.Label>
                    <Form.Control
                      type="text"
                      value={form.defaultBenefitsCsv}
                      onChange={e =>
                        setForm(f => ({
                          ...f,
                          defaultBenefitsCsv: e.target.value
                        }))
                      }
                      placeholder="lounge_access, priority_support"
                    />
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Check
                      type="checkbox"
                      label={t('marketing.cardTypes.form.allowMultipleLabel')}
                      checked={form.allowMultiplePerPerson}
                      onChange={e =>
                        setForm(f => ({
                          ...f,
                          allowMultiplePerPerson: e.target.checked
                        }))
                      }
                    />
                    <Form.Text className="text-muted">
                      {t('marketing.cardTypes.form.allowMultipleHelp')}
                    </Form.Text>
                  </Form.Group>
                  <Form.Group className="mb-3">
                    <Form.Check
                      type="checkbox"
                      label={t('marketing.cardTypes.form.activeLabel')}
                      checked={form.active}
                      onChange={e =>
                        setForm(f => ({ ...f, active: e.target.checked }))
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
                        {t('marketing.cardTypes.form.delete')}
                      </Button>
                    )}
                  </div>
                  <Button
                    type="submit"
                    variant="primary"
                    disabled={createState.isLoading || updateState.isLoading}
                  >
                    {createState.isLoading || updateState.isLoading
                      ? t('marketing.cardTypes.form.saving')
                      : t('marketing.cardTypes.form.save')}
                  </Button>
                </Card.Footer>
              </Card>
            </Form>
          )}
        </div>
      </div>
    </>
  );
};

export default CardTypesPage;
