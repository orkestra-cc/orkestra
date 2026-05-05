import { Alert, Spinner, Table } from 'react-bootstrap';
import SubtleBadge from 'components/common/SubtleBadge';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { Org } from 'store/api/tenantApi';
import { useListTenantSubscriptionsAdminQuery } from 'store/api/tenantApi';

interface Props {
  org: Org;
}

const statusColors: Record<string, BadgeColor> = {
  active: 'success',
  past_due: 'warning',
  suspended: 'danger',
  cancelled: 'secondary',
  expired: 'dark',
};

const SubscriptionsTab: React.FC<Props> = ({ org }) => {
  const { data, isLoading, error } = useListTenantSubscriptionsAdminQuery(
    org.id,
  );

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
        Failed to load subscriptions. This may mean the subscriptions module is
        disabled.
      </Alert>
    );
  }

  const subs = data?.subscriptions ?? [];

  if (subs.length === 0) {
    return (
      <Alert variant="light" className="fs-10 py-3 border text-center text-muted">
        No subscriptions for this client yet. When one lands, it will appear
        here — the entitlement syncer grants capabilities on activation.
      </Alert>
    );
  }

  return (
    <Table size="sm" className="fs-10 mb-0">
      <thead className="bg-body-tertiary">
        <tr>
          <th>Tier</th>
          <th>Status</th>
          <th>Period</th>
          <th>Next billing</th>
          <th>Created</th>
        </tr>
      </thead>
      <tbody>
        {subs.map((s) => (
          <tr key={s.uuid} className="align-middle">
            <td>
              <code className="fs-11">{s.tierCode}</code>
              <div className="text-muted fs-11">{s.serviceUUID}</div>
            </td>
            <td>
              <SubtleBadge bg={statusColors[s.status] || 'secondary'} pill>
                {s.status}
              </SubtleBadge>
            </td>
            <td className="text-muted">
              {new Date(s.currentPeriodStart).toLocaleDateString()} →{' '}
              {new Date(s.currentPeriodEnd).toLocaleDateString()}
            </td>
            <td className="text-muted">
              {new Date(s.nextBillingAt).toLocaleDateString()}
            </td>
            <td className="text-muted">
              {new Date(s.createdAt).toLocaleDateString()}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
};

export default SubscriptionsTab;
