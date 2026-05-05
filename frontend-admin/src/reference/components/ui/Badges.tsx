import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import PageHeader from 'components/common/PageHeader';
import SubtleBadge from 'components/common/SubtleBadge';
import { reactBootstrapDocsUrl } from 'helpers/utils';

import { Button } from 'react-bootstrap';

const SubtleBadgesCode = `<div>
  <SubtleBadge bg='primary' className='me-2'>Primary</SubtleBadge> 
  <SubtleBadge bg='secondary' className='me-2'>Secondary</SubtleBadge> 
  <SubtleBadge bg='success' className='me-2'>Success</SubtleBadge> 
  <SubtleBadge bg='info' className='me-2'>Info</SubtleBadge> 
  <SubtleBadge bg='warning' className='me-2'>Warning</SubtleBadge> 
  <SubtleBadge bg='danger' className='me-2'>Danger</SubtleBadge> 
  <SubtleBadge bg='light' className='me-2'>Light</SubtleBadge> 
  <SubtleBadge bg='dark' className='me-2'>Dark</SubtleBadge> 
</div>`;

const subtlePillBadgesCode = `<div>
  <SubtleBadge pill bg='primary' className='me-2'>Primary</SubtleBadge> 
  <SubtleBadge pill bg='secondary' className='me-2'>Secondary</SubtleBadge> 
  <SubtleBadge pill bg='success' className='me-2'>Success</SubtleBadge> 
  <SubtleBadge pill bg='info' className='me-2'>Info</SubtleBadge> 
  <SubtleBadge pill bg='warning' className='me-2'>Warning</SubtleBadge> 
  <SubtleBadge pill bg='danger' className='me-2'>Danger</SubtleBadge> 
  <SubtleBadge pill bg='light' className='me-2'>Light</SubtleBadge> 
  <SubtleBadge pill bg='dark' className='me-2'>Dark</SubtleBadge> 
</div>`;

const solidPillBagesCode = `<div>
  <Badge pill bg="primary" className="me-2">
    Primary
  </Badge>
  <Badge pill bg="secondary" className="me-2">
    Secondary
  </Badge>
  <Badge pill bg="success" className="me-2">
    Success
  </Badge>
  <Badge pill bg="danger" className="me-2">
    Danger
  </Badge>
  <Badge pill bg="warning" text="dark" className="me-2">
    Warning
  </Badge>
  <Badge pill bg="info" className="me-2">
    Info
  </Badge>
  <Badge pill bg="light" text="dark">
    Light
  </Badge>
  <Badge pill bg="dark" className="me-2 dark__bg-dark">
    Dark
  </Badge>
</div>`;

const solidBagesCode = `<div>
  <Badge bg="primary" className="me-2">Primary</Badge> 
  <Badge bg="secondary" className="me-2">Secondary</Badge>
  <Badge bg="success" className="me-2">Success</Badge> 
  <Badge bg="danger" className="me-2">Danger</Badge>
  <Badge bg="warning" text="dark" className="me-2">
    Warning
  </Badge>
  <Badge bg="info" className="me-2">Info</Badge>
  <Badge bg="light" text="dark" className="me-2">
    Light
  </Badge>
  <Badge bg="dark" className="me-2 dark__bg-dark">Dark</Badge>
</div>`;

const Badges = () => (
  <>
    <PageHeader
      title="Badges"
      description="Documentation and examples for badges, our small count and labeling component."
      className="mb-3"
    >
      <Button
        href={`${reactBootstrapDocsUrl}/docs/components/badge`}
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        Badges on React Bootstrap
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Subtle badges" light={false} />
      <FalconComponentCard.Body
        code={SubtleBadgesCode}
        scope={{ SubtleBadge }}
        language="jsx"
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Subtle pill badges" light={false} />
      <FalconComponentCard.Body
        code={subtlePillBadgesCode}
        scope={{ SubtleBadge }}
        language="jsx"
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Solid badges" light={false} />
      <FalconComponentCard.Body code={solidBagesCode} language="jsx" />
    </FalconComponentCard>
    <FalconComponentCard noGuttersBottom>
      <FalconComponentCard.Header title="Solid pill badges" light={false} />
      <FalconComponentCard.Body code={solidPillBagesCode} language="jsx" />
    </FalconComponentCard>
  </>
);

export default Badges;
