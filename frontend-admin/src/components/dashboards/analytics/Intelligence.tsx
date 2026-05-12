import { Card } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import signalImg from 'assets/img/icons/signal.png';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { Link } from 'react-router';
import OrkestraLink from 'components/common/OrkestraLink';
import SimpleBar from 'simplebar-react';

interface IntelligenceItem {
  icon: IconProp;
  title: string;
  description: string;
}

interface SingleItemProps {
  icon: IconProp;
  title: string;
  description: string;
}

interface IntelligenceProps {
  data: IntelligenceItem[];
}

const SingleItem = ({ icon, title, description }: SingleItemProps) => {
  return (
    <div className="border border-1 border-300 rounded-2 p-3 ask-analytics-item position-relative mb-3">
      <Flex alignItems="center" className="mb-3">
        <FontAwesomeIcon icon={icon} className="text-primary" />
        <Link to="#!" className="stretched-link text-decoration-none">
          <h5 className="fs-10 text-600 mb-0 ps-3">{title}</h5>
        </Link>
      </Flex>
      <h5 className="fs-10 text-800">{description}</h5>
    </div>
  );
};

const Intelligence = ({ data }: IntelligenceProps) => {
  return (
    <Card className="h-100">
      <Card.Header as={Flex} alignItems="center">
        <img src={signalImg} alt="intelligence" height={35} className="me-2" />
        <h5 className="fs-9 fw-normal text-800 mb-0">
          Ask Orkestra Intelligence
        </h5>
      </Card.Header>
      <Card.Body className="p-0">
        <SimpleBar className="ask-analytics">
          <div className="pt-0 px-x1">
            {data.map(item => (
              <SingleItem
                key={item.title}
                icon={item.icon}
                title={item.title}
                description={item.description}
              />
            ))}
          </div>
        </SimpleBar>
      </Card.Body>
      <Card.Footer className="bg-body-tertiary text-end py-2">
        <OrkestraLink title="More Insights" className="px-0 fw-medium" />
      </Card.Footer>
    </Card>
  );
};

export default Intelligence;
