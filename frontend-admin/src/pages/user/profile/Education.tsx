
import educationData from 'data/educations';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Card } from 'react-bootstrap';
import EducationItem from '../EducationItem';

const Education: React.FC = () => {
  return (
    <Card className="mb-3">
      <FalconCardHeader title="Education" light />
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
