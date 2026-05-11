import { Dropdown } from 'react-bootstrap';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  selectMemberships,
  selectCurrentMembership,
  setCurrentOrg
} from 'store/slices/tenantSlice';

/**
 * OrgSwitcher renders a dropdown of the current user's tenant memberships and
 * switches the active tenant in Redux. Switching is instant — the baseApi
 * interceptor will pick up the new tenantId on the next request, so no token
 * refresh is needed.
 *
 * Drop this into the top navbar next to the user avatar.
 */
export default function OrgSwitcher() {
  const dispatch = useAppDispatch();
  const memberships = useAppSelector(selectMemberships);
  const current = useAppSelector(selectCurrentMembership);

  if (memberships.length === 0) {
    return null;
  }

  if (memberships.length === 1 && current) {
    return (
      <span className="text-muted small me-2" title={`Plan: ${current.plan}`}>
        {current.name}
      </span>
    );
  }

  return (
    <Dropdown align="end" className="me-2">
      <Dropdown.Toggle variant="outline-secondary" size="sm" id="org-switcher">
        {current ? current.name : 'Select organization'}
      </Dropdown.Toggle>
      <Dropdown.Menu>
        <Dropdown.Header>Your organizations</Dropdown.Header>
        {memberships.map((m) => (
          <Dropdown.Item
            key={m.tenantId}
            active={m.tenantId === current?.tenantId}
            onClick={() => dispatch(setCurrentOrg(m.tenantId))}
          >
            <div className="d-flex justify-content-between align-items-center">
              <span>{m.name}</span>
              <small className="text-muted ms-3">{m.plan}</small>
            </div>
            {m.roles.length > 0 && (
              <small className="text-muted d-block">{m.roles.join(', ')}</small>
            )}
          </Dropdown.Item>
        ))}
      </Dropdown.Menu>
    </Dropdown>
  );
}
