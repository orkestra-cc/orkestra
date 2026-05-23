import { useState } from 'react';
import { Badge, Button, Card, Modal, Form, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import PageHeader from 'components/common/PageHeader';
import IconButton from 'components/common/IconButton';
import Flex from 'components/common/Flex';
import {
  useListSubscriptionServicesQuery,
  useCreateSubscriptionServiceMutation,
  useUpdateSubscriptionServiceMutation,
  useDeleteSubscriptionServiceMutation
} from 'store/api/subscriptionsApi';
import type {
  CreateServiceInput,
  PricingTier,
  SubscriptionService
} from 'types/subscriptions';

const emptyForm: CreateServiceInput = {
  code: '',
  name: '',
  category: 'workflow',
  description: '',
  active: true,
  setupFeeCents: 0,
  pricingTiers: [
    { code: 'monthly', cycle: 'monthly', amountCents: 0, currency: 'EUR' }
  ]
};

const formatMoney = (cents: number, currency = 'EUR') =>
  new Intl.NumberFormat('it-IT', { style: 'currency', currency }).format(
    cents / 100
  );

const ServicesListPage: React.FC = () => {
  const { t } = useTranslation();
  const { data, isLoading, refetch } =
    useListSubscriptionServicesQuery(undefined);
  const [create] = useCreateSubscriptionServiceMutation();
  const [update] = useUpdateSubscriptionServiceMutation();
  const [del] = useDeleteSubscriptionServiceMutation();

  const [editing, setEditing] = useState<SubscriptionService | null>(null);
  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState<CreateServiceInput>(emptyForm);

  const openNew = () => {
    setForm(emptyForm);
    setEditing(null);
    setShowModal(true);
  };
  const openEdit = (s: SubscriptionService) => {
    setForm({
      code: s.code,
      name: s.name,
      category: s.category,
      description: s.description,
      active: s.active,
      setupFeeCents: s.setupFeeCents,
      pricingTiers: s.pricingTiers
    });
    setEditing(s);
    setShowModal(true);
  };

  const submit = async () => {
    if (editing) {
      await update({ id: editing.uuid, patch: form }).unwrap();
    } else {
      await create(form).unwrap();
    }
    setShowModal(false);
  };

  const updateTier = (i: number, patch: Partial<PricingTier>) => {
    const next = [...form.pricingTiers];
    next[i] = { ...next[i], ...patch };
    setForm({ ...form, pricingTiers: next });
  };
  const addTier = () =>
    setForm({
      ...form,
      pricingTiers: [
        ...form.pricingTiers,
        { code: 'annual', cycle: 'annual', amountCents: 0, currency: 'EUR' }
      ]
    });
  const removeTier = (i: number) =>
    setForm({
      ...form,
      pricingTiers: form.pricingTiers.filter((_, idx) => idx !== i)
    });

  return (
    <>
      <PageHeader
        title={t('subscriptions.services.title')}
        description={t('subscriptions.services.description')}
        className="mb-3"
      >
        <Flex className="gap-2 mt-3">
          <IconButton icon="plus" variant="primary" onClick={openNew}>
            {t('subscriptions.services.new')}
          </IconButton>
          <IconButton
            icon="sync-alt"
            variant="orkestra-default"
            onClick={() => refetch()}
          >
            {t('subscriptions.services.refresh')}
          </IconButton>
        </Flex>
      </PageHeader>

      <Card>
        <Card.Body className="p-0">
          {isLoading ? (
            <div className="p-4">{t('subscriptions.services.loading')}</div>
          ) : !data?.items.length ? (
            <div className="p-4 text-muted text-center">
              {t('subscriptions.services.empty')}
            </div>
          ) : (
            <Table responsive hover className="mb-0">
              <thead className="bg-200">
                <tr>
                  <th>{t('subscriptions.services.columns.code')}</th>
                  <th>{t('subscriptions.services.columns.name')}</th>
                  <th>{t('subscriptions.services.columns.category')}</th>
                  <th>{t('subscriptions.services.columns.pricing')}</th>
                  <th>{t('subscriptions.services.columns.status')}</th>
                  <th className="text-end">
                    {t('subscriptions.services.columns.actions')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(s => (
                  <tr key={s.uuid}>
                    <td>
                      <code>{s.code}</code>
                    </td>
                    <td>{s.name}</td>
                    <td>{s.category}</td>
                    <td>
                      {s.pricingTiers.map(pt => (
                        <div key={pt.code}>
                          <small>
                            <strong>{pt.cycle}</strong>:{' '}
                            {formatMoney(pt.amountCents, pt.currency)}
                          </small>
                        </div>
                      ))}
                    </td>
                    <td>
                      <Badge bg={s.active ? 'success' : 'secondary'}>
                        {s.active
                          ? t('subscriptions.services.statusActive')
                          : t('subscriptions.services.statusInactive')}
                      </Badge>
                    </td>
                    <td className="text-end">
                      <Button
                        size="sm"
                        variant="link"
                        onClick={() => openEdit(s)}
                      >
                        {t('subscriptions.services.edit')}
                      </Button>
                      <Button
                        size="sm"
                        variant="link"
                        className="text-danger"
                        onClick={async () => {
                          if (
                            confirm(
                              t('subscriptions.services.deleteConfirm', {
                                name: s.name
                              })
                            )
                          ) {
                            await del(s.uuid).unwrap();
                          }
                        }}
                      >
                        {t('subscriptions.services.delete')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card.Body>
      </Card>

      <Modal show={showModal} onHide={() => setShowModal(false)} size="lg">
        <Modal.Header closeButton>
          <Modal.Title>
            {editing
              ? t('subscriptions.services.modalEditTitle')
              : t('subscriptions.services.modalNewTitle')}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Form>
            <div className="row g-3">
              <div className="col-md-4">
                <Form.Group>
                  <Form.Label>
                    {t('subscriptions.services.modal.codeLabel')}
                  </Form.Label>
                  <Form.Control
                    value={form.code}
                    disabled={!!editing}
                    onChange={e => setForm({ ...form, code: e.target.value })}
                    placeholder={t(
                      'subscriptions.services.modal.codePlaceholder'
                    )}
                  />
                </Form.Group>
              </div>
              <div className="col-md-8">
                <Form.Group>
                  <Form.Label>
                    {t('subscriptions.services.modal.nameLabel')}
                  </Form.Label>
                  <Form.Control
                    value={form.name}
                    onChange={e => setForm({ ...form, name: e.target.value })}
                  />
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>
                    {t('subscriptions.services.modal.categoryLabel')}
                  </Form.Label>
                  <Form.Select
                    value={form.category}
                    onChange={e =>
                      setForm({ ...form, category: e.target.value })
                    }
                  >
                    <option value="workflow">
                      {t('subscriptions.services.modal.categoryWorkflow')}
                    </option>
                    <option value="database">
                      {t('subscriptions.services.modal.categoryDatabase')}
                    </option>
                    <option value="agent">
                      {t('subscriptions.services.modal.categoryAgent')}
                    </option>
                    <option value="hosting">
                      {t('subscriptions.services.modal.categoryHosting')}
                    </option>
                    <option value="custom">
                      {t('subscriptions.services.modal.categoryCustom')}
                    </option>
                  </Form.Select>
                </Form.Group>
              </div>
              <div className="col-md-6">
                <Form.Group>
                  <Form.Label>
                    {t('subscriptions.services.modal.setupFeeLabel')}
                  </Form.Label>
                  <Form.Control
                    type="number"
                    value={form.setupFeeCents ?? 0}
                    onChange={e =>
                      setForm({
                        ...form,
                        setupFeeCents: Number(e.target.value)
                      })
                    }
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Group>
                  <Form.Label>
                    {t('subscriptions.services.modal.descriptionLabel')}
                  </Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={2}
                    value={form.description ?? ''}
                    onChange={e =>
                      setForm({ ...form, description: e.target.value })
                    }
                  />
                </Form.Group>
              </div>
              <div className="col-12">
                <Form.Check
                  type="switch"
                  label={t('subscriptions.services.modal.activeLabel')}
                  checked={form.active}
                  onChange={e => setForm({ ...form, active: e.target.checked })}
                />
              </div>
            </div>

            <hr />
            <Flex justifyContent="between" alignItems="center" className="mb-2">
              <strong>
                {t('subscriptions.services.modal.pricingTiersHeading')}
              </strong>
              <Button size="sm" variant="orkestra-default" onClick={addTier}>
                {t('subscriptions.services.modal.addTier')}
              </Button>
            </Flex>
            {form.pricingTiers.map((tier, i) => (
              <div className="row g-2 mb-2" key={i}>
                <div className="col-md-3">
                  <Form.Control
                    placeholder={t(
                      'subscriptions.services.modal.tierCodePlaceholder'
                    )}
                    value={tier.code}
                    onChange={e => updateTier(i, { code: e.target.value })}
                  />
                </div>
                <div className="col-md-3">
                  <Form.Select
                    value={tier.cycle}
                    onChange={e =>
                      updateTier(i, {
                        cycle: e.target.value as PricingTier['cycle']
                      })
                    }
                  >
                    <option value="monthly">
                      {t('subscriptions.services.modal.cycleMonthly')}
                    </option>
                    <option value="quarterly">
                      {t('subscriptions.services.modal.cycleQuarterly')}
                    </option>
                    <option value="annual">
                      {t('subscriptions.services.modal.cycleAnnual')}
                    </option>
                  </Form.Select>
                </div>
                <div className="col-md-3">
                  <Form.Control
                    type="number"
                    placeholder={t(
                      'subscriptions.services.modal.tierAmountPlaceholder'
                    )}
                    value={tier.amountCents}
                    onChange={e =>
                      updateTier(i, { amountCents: Number(e.target.value) })
                    }
                  />
                </div>
                <div className="col-md-2">
                  <Form.Control
                    placeholder={t(
                      'subscriptions.services.modal.tierCurrencyPlaceholder'
                    )}
                    value={tier.currency}
                    onChange={e => updateTier(i, { currency: e.target.value })}
                  />
                </div>
                <div className="col-md-1">
                  <Button
                    variant="link"
                    className="text-danger p-0"
                    onClick={() => removeTier(i)}
                    disabled={form.pricingTiers.length === 1}
                  >
                    ✕
                  </Button>
                </div>
              </div>
            ))}
          </Form>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowModal(false)}>
            {t('subscriptions.services.modal.cancel')}
          </Button>
          <Button variant="primary" onClick={submit}>
            {t('subscriptions.services.modal.save')}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default ServicesListPage;
