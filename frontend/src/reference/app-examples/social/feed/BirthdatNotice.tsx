import { Card } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Link } from 'react-router';
import Flex from 'components/common/Flex';

interface BirthdayNoticeProps {
  name: string;
  profileLink: string;
  className?: string;
}

const BirthdayNotice = ({ name, profileLink, className }: BirthdayNoticeProps) => {
  return (
    <Card className={className}>
      <Card.Body className="fs-10">
        <Flex>
          <FontAwesomeIcon icon="gift" className="fs-9 text-warning" />
          <div className="ms-2 flex-1">
            <Link className="fw-semibold" to={profileLink}>
              {name}
            </Link>
            's Birthday is today
          </div>
        </Flex>
      </Card.Body>
    </Card>
  );
};

export default BirthdayNotice;
