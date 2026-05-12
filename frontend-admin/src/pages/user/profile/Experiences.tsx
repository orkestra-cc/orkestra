import { Card } from 'react-bootstrap';
import experiences from 'data/experiences';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import Experience from '../Experience';

const Experiences: React.FC = () => {
  return (
    <Card className="mb-3">
      <OrkestraCardHeader title="Experience" light />
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
