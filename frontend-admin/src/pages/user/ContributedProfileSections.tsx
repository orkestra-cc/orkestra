// Renders the sections other modules contribute to a user page (profileSlots.ts):
// one card per contribution, titled in the contributor's own i18n namespace,
// body lazily loaded. The host owns the layout and passes the slot props; this
// component owns the card chrome so every contributed section looks the same.
import { Suspense, type ComponentType, type LazyExoticComponent } from 'react';
import { Card } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import OrkestraLoader from 'components/common/OrkestraLoader';
import type { ResolvedSlotContribution } from 'modules/slotContributions';

interface Props<P> {
  contributions: ResolvedSlotContribution[];
  sectionProps: P;
}

function ContributedProfileSections<P extends object>({
  contributions,
  sectionProps
}: Props<P>) {
  const { t } = useTranslation();
  if (contributions.length === 0) return null;
  return (
    <>
      {contributions.map(c => {
        const Component = c.component as LazyExoticComponent<ComponentType<P>>;
        return (
          <Card
            className="mb-3"
            key={c.key}
            data-testid={`profile-section-${c.key}`}
          >
            <Card.Header className="bg-body-tertiary">
              <h5 className="mb-0">{t(c.labelKey, { ns: c.moduleName })}</h5>
            </Card.Header>
            <Card.Body>
              <Suspense fallback={<OrkestraLoader />}>
                <Component {...sectionProps} />
              </Suspense>
            </Card.Body>
          </Card>
        );
      })}
    </>
  );
}

export default ContributedProfileSections;
