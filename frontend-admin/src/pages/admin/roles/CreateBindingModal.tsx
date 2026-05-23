import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { toast } from 'react-toastify';
import { Trans, useTranslation } from 'react-i18next';
import {
  useCreateBindingMutation,
  useListRolesQuery
} from 'store/api/tenantApi';

interface Props {
  tenantId: string;
  show: boolean;
  onHide: () => void;
}

/**
 * CreateBindingModal grants a role to a user in the current org, with an
 * optional expiry timestamp for contractor/trial access. The TTL index on
 * authz_bindings auto-reaps expired entries.
 */
const CreateBindingModal: React.FC<Props> = ({ tenantId, show, onHide }) => {
  const { t } = useTranslation();
  const { data: rolesData, isLoading: rolesLoading } = useListRolesQuery(
    tenantId,
    {
      skip: !show
    }
  );
  const [createBinding, { isLoading: isSaving }] = useCreateBindingMutation();

  const [userUUID, setUserUUID] = useState('');
  const [roleId, setRoleId] = useState('');
  const [expiresAt, setExpiresAt] = useState('');

  useEffect(() => {
    if (!show) {
      setUserUUID('');
      setRoleId('');
      setExpiresAt('');
    }
  }, [show]);

  const roles = rolesData?.roles ?? [];
  const canSave = userUUID.trim().length > 0 && roleId.length > 0 && !isSaving;

  const onSave = async () => {
    try {
      await createBinding({
        tenantId,
        body: {
          userUUID: userUUID.trim(),
          roleId,
          expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined
        }
      }).unwrap();
      toast.success(t('adminRoles.bindingModal.successToast'));
      onHide();
    } catch (err: unknown) {
      toast.error(
        t('adminRoles.bindingModal.errorToast', {
          message: extractError(err, t('adminRoles.bindingModal.unknownError'))
        })
      );
    }
  };

  return (
    <Modal show={show} onHide={onHide} backdrop="static">
      <Modal.Header closeButton>
        <Modal.Title>{t('adminRoles.bindingModal.title')}</Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form>
          <Form.Group className="mb-3">
            <Form.Label>
              {t('adminRoles.bindingModal.userUuidLabel')}
            </Form.Label>
            <Form.Control
              type="text"
              placeholder={t('adminRoles.bindingModal.userUuidPlaceholder')}
              value={userUUID}
              onChange={e => setUserUUID(e.target.value)}
              autoFocus
            />
            <Form.Text muted>
              <Trans
                i18nKey="adminRoles.bindingModal.userUuidHelp"
                components={{ code: <code /> }}
              />
            </Form.Text>
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>{t('adminRoles.bindingModal.roleLabel')}</Form.Label>
            {rolesLoading ? (
              <div>
                <Spinner animation="border" size="sm" />{' '}
                {t('adminRoles.bindingModal.loadingRoles')}
              </div>
            ) : (
              <Form.Select
                value={roleId}
                onChange={e => setRoleId(e.target.value)}
              >
                <option value="">
                  {t('adminRoles.bindingModal.rolePlaceholder')}
                </option>
                <optgroup label={t('adminRoles.bindingModal.systemRolesGroup')}>
                  {roles
                    .filter(r => r.isSystem)
                    .map(r => (
                      <option key={r.id} value={r.id}>
                        {r.name} (
                        {t('adminRoles.bindingModal.rolePermsCount', {
                          count: r.permissions.length
                        })}
                        )
                      </option>
                    ))}
                </optgroup>
                {roles.some(r => !r.isSystem) && (
                  <optgroup
                    label={t('adminRoles.bindingModal.customRolesGroup')}
                  >
                    {roles
                      .filter(r => !r.isSystem)
                      .map(r => (
                        <option key={r.id} value={r.id}>
                          {r.name} (
                          {t('adminRoles.bindingModal.rolePermsCount', {
                            count: r.permissions.length
                          })}
                          )
                        </option>
                      ))}
                  </optgroup>
                )}
              </Form.Select>
            )}
          </Form.Group>

          <Form.Group className="mb-3">
            <Form.Label>{t('adminRoles.bindingModal.expiresLabel')}</Form.Label>
            <Form.Control
              type="datetime-local"
              value={expiresAt}
              onChange={e => setExpiresAt(e.target.value)}
            />
            <Form.Text muted>
              {t('adminRoles.bindingModal.expiresHelp')}
            </Form.Text>
          </Form.Group>
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isSaving}>
          {t('adminRoles.bindingModal.cancel')}
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isSaving ? (
            <Spinner size="sm" animation="border" className="me-2" />
          ) : null}
          {t('adminRoles.bindingModal.grant')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

function extractError(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || fallback;
  }
  return String(err);
}

export default CreateBindingModal;
