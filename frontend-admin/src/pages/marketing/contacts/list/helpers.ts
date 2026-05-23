// Small projection helpers shared by PersonsTable and OrganizationsTable.
// Kept as plain functions (no React) so column accessorFns and cell
// renderers can call them without re-running on every render.

import type { Person, Organization } from 'types/marketing';

export const primaryEmail = (
  p: Pick<Person, 'emails'> | Pick<Organization, 'emails'>
): string =>
  p.emails?.find(e => e.primary)?.address ?? p.emails?.[0]?.address ?? '';

export const fullName = (p: Pick<Person, 'firstName' | 'lastName'>): string =>
  [p.firstName, p.lastName].filter(Boolean).join(' ');
