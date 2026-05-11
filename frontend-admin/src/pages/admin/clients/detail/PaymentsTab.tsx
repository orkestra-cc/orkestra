import { Alert, Spinner, Table } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { Org } from 'store/api/tenantApi';
import { useListTenantPaymentsAdminQuery } from 'store/api/tenantApi';

interface Props {
  org: Org;
}

const statusColors: Record<string, BadgeColor> = {
  succeeded: 'success',
  pending: 'info',
  requires_action: 'warning',
  failed: 'danger',
  refunded: 'secondary',
  partially_refunded: 'secondary',
};

function formatAmount(cents: number, currency: string): string {
  const amount = (cents / 100).toFixed(2);
  return `${amount} ${currency.toUpperCase()}`;
}

const PaymentsTab: React.FC<Props> = ({ org }) => {
  const { data, isLoading, error } = useListTenantPaymentsAdminQuery(org.id);

  if (isLoading) {
    return (
      <div className="text-center py-4">
        <Spinner animation="border" size="sm" />
      </div>
    );
  }
  if (error) {
    return (
      <Alert variant="danger" className="fs-10">
        Failed to load payments. This may mean the payments module is disabled.
      </Alert>
    );
  }

  const rows = data?.payments ?? [];

  if (rows.length === 0) {
    return (
      <Alert variant="light" className="fs-10 py-3 border text-center text-muted">
        No payment transactions for this client yet.
      </Alert>
    );
  }

  return (
    <Table size="sm" className="fs-10 mb-0">
      <thead className="bg-body-tertiary">
        <tr>
          <th>Provider</th>
          <th>Reference</th>
          <th>Amount</th>
          <th>Status</th>
          <th>Charged</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((p) => (
          <tr key={p.uuid} className="align-middle">
            <td>
              <SubtleBadge bg="primary" pill>
                {p.provider}
              </SubtleBadge>
            </td>
            <td>
              <code className="fs-11">{p.providerTxID || '—'}</code>
            </td>
            <td>{formatAmount(p.amountCents, p.currency)}</td>
            <td>
              <SubtleBadge bg={statusColors[p.status] || 'secondary'} pill>
                {p.status}
              </SubtleBadge>
              {p.refundedCents > 0 && (
                <div className="text-muted fs-11">
                  refunded {formatAmount(p.refundedCents, p.currency)}
                </div>
              )}
            </td>
            <td className="text-muted">
              {p.chargedAt ? new Date(p.chargedAt).toLocaleDateString() : '—'}
            </td>
            <td className="text-muted">
              {new Date(p.createdAt).toLocaleDateString()}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
};

export default PaymentsTab;
