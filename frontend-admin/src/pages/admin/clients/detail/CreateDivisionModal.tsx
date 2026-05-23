import { useEffect, useState } from 'react';
import { Button, Form, Modal, Spinner } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import { useCreateTenantDivisionAdminMutation } from 'store/api/tenantApi';

interface Props {
  parentId: string;
  parentName: string;
  show: boolean;
  onHide: () => void;
}

function slugify(input: string): string {
  return input
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s_-]/g, '')
    .replace(/[\s_]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

const CreateDivisionModal: React.FC<Props> = ({
  parentId,
  parentName,
  show,
  onHide
}) => {
  const { t } = useTranslation();
  const [createDivision, { isLoading }] =
    useCreateTenantDivisionAdminMutation();
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugEdited, setSlugEdited] = useState(false);

  useEffect(() => {
    if (!show) {
      setName('');
      setSlug('');
      setSlugEdited(false);
    }
  }, [show]);

  const onNameChange = (value: string) => {
    setName(value);
    if (!slugEdited) setSlug(slugify(value));
  };

  const canSave =
    name.trim().length > 0 && slug.trim().length > 0 && !isLoading;

  const unknownErr = t('adminClients.createDivision.errorUnknown');

  const onSave = async () => {
    try {
      await createDivision({
        tenantId: parentId,
        body: { name: name.trim(), slug: slug.trim() || undefined }
      }).unwrap();
      toast.success(
        t('adminClients.createDivision.toastCreated', {
          name: name.trim(),
          parent: parentName
        })
      );
      onHide();
    } catch (err: unknown) {
      toast.error(
        t('adminClients.createDivision.toastFailed', {
          error: extractError(err, unknownErr)
        })
      );
    }
  };

  return (
    <Modal show={show} onHide={onHide} backdrop="static" centered>
      <Modal.Header closeButton>
        <Modal.Title>
          <FontAwesomeIcon icon="plus" className="text-primary me-2" />
          {t('adminClients.createDivision.title', { parent: parentName })}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        <Form>
          <Form.Group className="mb-3">
            <Form.Label className="fw-semibold">
              {t('adminClients.createDivision.labelName')}
            </Form.Label>
            <Form.Control
              type="text"
              placeholder={t('adminClients.createDivision.placeholderName')}
              value={name}
              maxLength={120}
              onChange={e => onNameChange(e.target.value)}
              autoFocus
            />
          </Form.Group>
          <Form.Group className="mb-0">
            <Form.Label className="fw-semibold">
              {t('adminClients.createDivision.labelSlug')}
            </Form.Label>
            <Form.Control
              type="text"
              placeholder={t('adminClients.createDivision.placeholderSlug')}
              value={slug}
              maxLength={80}
              onChange={e => {
                setSlug(slugify(e.target.value));
                setSlugEdited(true);
              }}
            />
            <Form.Text muted>
              {t('adminClients.createDivision.slugHelp')}
            </Form.Text>
          </Form.Group>
        </Form>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          {t('adminClients.createDivision.cancel')}
        </Button>
        <Button variant="primary" onClick={onSave} disabled={!canSave}>
          {isLoading ? (
            <>
              <Spinner size="sm" animation="border" className="me-2" />{' '}
              {t('adminClients.createDivision.submitting')}
            </>
          ) : (
            <>{t('adminClients.createDivision.submit')}</>
          )}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

function extractError(err: unknown, unknownLabel: string): string {
  if (err && typeof err === 'object' && 'data' in err) {
    const data = (err as { data?: { detail?: string; title?: string } }).data;
    return data?.detail || data?.title || unknownLabel;
  }
  return String(err);
}

export default CreateDivisionModal;
