import { describe, it, expect } from 'vitest';
import { lazy } from 'react';
import { screen } from '@testing-library/react';
import { renderWithProviders } from 'test/render';
import type { ResolvedSlotContribution } from 'modules/slotContributions';
import ContributedProfileSections from './ContributedProfileSections';
import type { UserProfileSectionProps } from './profileSlots';

const Section = (p: UserProfileSectionProps) => (
  <div>
    section for {p.userId} as {p.viewer}
  </div>
);

const contribution = (key: string): ResolvedSlotContribution => ({
  key,
  moduleName: key.split('.')[0],
  labelKey: 'profile.sectionTitle',
  component: lazy(() => Promise.resolve({ default: Section }))
});

describe('ContributedProfileSections', () => {
  it('renders nothing without contributions', () => {
    const { container } = renderWithProviders(
      <ContributedProfileSections<UserProfileSectionProps>
        contributions={[]}
        sectionProps={{ userId: 'u-1', user: {} as never, viewer: 'self' }}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders one card per contribution, titled in the contributor namespace, with the slot props', async () => {
    renderWithProviders(
      <ContributedProfileSections<UserProfileSectionProps>
        contributions={[
          contribution('people.person'),
          contribution('crm.activity')
        ]}
        sectionProps={{ userId: 'u-1', user: {} as never, viewer: 'admin' }}
      />
    );
    expect(await screen.findAllByText('section for u-1 as admin')).toHaveLength(
      2
    );
    expect(
      screen.getByTestId('profile-section-people.person')
    ).toBeInTheDocument();
    expect(
      screen.getByTestId('profile-section-crm.activity')
    ).toBeInTheDocument();
    // An unresolved key in a module namespace renders the key itself.
    expect(screen.getAllByText('profile.sectionTitle')).toHaveLength(2);
  });
});
