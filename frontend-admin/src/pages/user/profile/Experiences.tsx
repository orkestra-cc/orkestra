
import { Card } from 'react-bootstrap';
import experiences from 'data/experiences';
import FalconCardHeader from 'components/common/FalconCardHeader';
import Experience from '../Experience';

const Experiences: React.FC = () => {
  return (
    <Card className="mb-3">
      <FalconCardHeader title="Experience" light />
      <Card.Body className="fs-10">
        {(experiences as any[]).map((experience: any, index: number) => (
          <Experience
            key={experience.id}
            experience={experience}
            isLast={index === experiences.length - 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

export default Experiences;
