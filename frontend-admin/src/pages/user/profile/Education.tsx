import educationData from 'data/educations';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { Card } from 'react-bootstrap';
import EducationItem from '../EducationItem';

const Education: React.FC = () => {
  return (
    <Card className="mb-3">
      <OrkestraCardHeader title="Education" light />
      <Card.Body className="fs-10">
        {(educationData as any[]).map((item: any, index: number) => (
          <EducationItem
            key={item.id}
            details={item}
            isLast={index === educationData.length - 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

export default Education;
