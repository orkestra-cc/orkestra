
import { Row, Col } from 'react-bootstrap';

interface SectionHeaderProps {
  title: string;
  subtitle: string;
  [key: string]: any;
}

const SectionHeader: React.FC<SectionHeaderProps> = ({ title, subtitle, ...rest }) => {
  return (
    <Row className="justify-content-center text-center" {...rest}>
      <Col lg={8} xl={7} xxl={6} className="col-xxl-6">
        <h1 className="fs-7 fs-sm-5 fs-md-4">{title}</h1>
        <p className="lead">{subtitle}</p>
      </Col>
    </Row>
  );
};

export default SectionHeader;
