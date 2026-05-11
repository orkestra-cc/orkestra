import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';

import { Link } from 'react-router';

// Types for Education Item component
interface EducationDetails {
  logo: string;
  institution: string;
  degree: string;
  duration: string;
  location: string;
  verified?: boolean;
}

interface EducationItemProps {
  details: EducationDetails;
  isLast?: boolean;
}

export const EducationItem: React.FC<EducationItemProps> = ({ details, isLast }) => {
  const { logo, institution, degree, duration, location, verified } = details;
  return (
    <Flex>
      <Link to="#!">
        <Avatar size="3xl" src={logo} />
      </Link>
      <div className="flex-1 position-relative ps-3">
        <h6 className="fs-9 mb-0">
          <Link to="#!">{institution}</Link>
          {verified && <VerifiedBadge />}
        </h6>
        <p className="mb-1">{degree}</p>
        <p className="text-1000 mb-0">{duration}</p>
        <p className="text-1000 mb-0">{location}</p>
        {!isLast && <div className="border-dashed border-bottom my-3"></div>}
      </div>
    </Flex>
  );
};

export default EducationItem;
