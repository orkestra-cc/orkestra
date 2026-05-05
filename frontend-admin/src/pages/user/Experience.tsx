import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';

import { Image } from 'react-bootstrap';
import { Link } from 'react-router';

// Types for Experience component
interface ExperienceData {
  logo: string;
  title: string;
  company: string;
  startDate: string;
  endDate: string;
  duration: string;
  location: string;
  verified?: boolean;
}

interface ExperienceProps {
  experience: ExperienceData;
  isLast?: boolean;
}

const Experience: React.FC<ExperienceProps> = ({ experience, isLast }) => {
  const {
    logo,
    title,
    company,
    startDate,
    endDate,
    duration,
    location,
    verified
  } = experience;

  return (
    <Flex>
      <Link to="#!">
        <Image fluid src={logo} width={56} />
      </Link>
      <div className="flex-1 position-relative ps-3">
        <h6 className="fs-9 mb-0">
          {title}
          {verified && <VerifiedBadge />}
        </h6>
        <p className="mb-1">
          <Link to="#!">{company}</Link>
        </p>
        <p className="text-1000 mb-0">
          {`${startDate} - ${endDate} • ${duration}`}
        </p>
        <p className="text-1000 mb-0">{location}</p>
        {!isLast && <div className="border-dashed border-bottom my-3" />}
      </div>
    </Flex>
  );
};

export default Experience;
