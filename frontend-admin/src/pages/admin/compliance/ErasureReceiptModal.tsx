import { Alert, Button, Modal, Spinner, Table } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useGetErasureReceiptQuery } from 'store/api/complianceApi';
import { erasureErrorMessage } from './erasureErrors';
import { formatDateTime } from './complianceFormat';

interface Props {
  requestId: string | null;
  onClose: () => void;
}

// downloadJson saves the receipt exactly as the backend produced it.
const downloadJson = (name: string, value: unknown) => {
  const blob = new Blob([JSON.stringify(value, null, 2)], {
    type: 'application/json'
  });
  const href = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = href;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(href);
};

// ErasureReceiptModal shows the receipt of a finished erasure and lets the
// operator download it as JSON.
const ErasureReceiptModal = ({ requestId, onClose }: Props) => {
  const { t } = useTranslation();
  const { data, isFetching, error } = useGetErasureReceiptQuery(
    requestId ?? '',
    { skip: !requestId }
  );
  return (
    <Modal show={requestId !== null} onHide={onClose} centered size="lg">
      <Modal.Header closeButton>
        <Modal.Title as="h5">
          {t('adminCompliance.erasure.receipt.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {isFetching && <Spinner animation="border" size="sm" />}
        {error && (
          <Alert variant="warning" className="mb-0">
            {erasureErrorMessage(
              error,
              t,
              t('adminCompliance.erasure.receipt.error')
            )}
          </Alert>
        )}
        {data && !isFetching && (
          <>
            <dl className="row fs-10 mb-3">
              <dt className="col-sm-4">
                {t('adminCompliance.erasure.columns.subject')}
              </dt>
              <dd className="col-sm-8 font-monospace">
                {data.subjectUserUuid}
              </dd>
              <dt className="col-sm-4">
                {t('adminCompliance.erasure.columns.status')}
              </dt>
              <dd className="col-sm-8">
                {t(`adminCompliance.status.${data.status}`)}
              </dd>
              <dt className="col-sm-4">
                {t('adminCompliance.erasure.receipt.completedAt')}
              </dt>
              <dd className="col-sm-8">
                {data.completedAt ? formatDateTime(data.completedAt) : '—'}
              </dd>
              <dt className="col-sm-4">
                {t('adminCompliance.erasure.receipt.totals')}
              </dt>
              <dd className="col-sm-8">
                {t('adminCompliance.erasure.receipt.totalsValue', {
                  deleted: data.totalRowsDeleted,
                  anonymized: data.totalRowsAnonymized
                })}
              </dd>
            </dl>
            <Table size="sm" className="fs-10">
              <thead>
                <tr>
                  <th>{t('adminCompliance.erasure.receipt.producer')}</th>
                  <th>{t('adminCompliance.erasure.columns.status')}</th>
                </tr>
              </thead>
              <tbody>
                {(data.producers ?? []).map(p => (
                  <tr key={p.key}>
                    <td>{p.key}</td>
                    <td>
                      {t(`adminCompliance.erasure.outcome.${p.outcome}`, {
                        defaultValue: p.outcome
                      })}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="orkestra-default" onClick={onClose}>
          {t('adminCompliance.erasure.receipt.close')}
        </Button>
        <Button
          variant="orkestra-primary"
          disabled={!data}
          onClick={() =>
            data &&
            downloadJson(`erasure-receipt-${data.requestUuid}.json`, data)
          }
        >
          {t('adminCompliance.erasure.receipt.download')}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

export default ErasureReceiptModal;
