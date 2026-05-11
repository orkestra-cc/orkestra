
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Row, Col, Image } from 'react-bootstrap';
import { useAppContext } from 'providers/AppProvider';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface ProcessProps {
  icon: IconProp;
  iconText: string;
  isFirst: boolean;
  color: string;
  title: string;
  description: string;
  image: string;
  imageDark: string;
  inverse: boolean;
  children?: React.ReactNode;
}

const Process: React.FC<ProcessProps> = ({
  icon,
  iconText,
  isFirst,
  color,
  title,
  description,
  image,
  imageDark,
  inverse,
  children
}) => {
  const {
    config: { isDark }
  } = useAppContext();
  return (
    <Row
      className={classNames('flex-center', {
        'mt-7': !isFirst,
        'mt-8': isFirst
      })}
    >
      <Col
        md={{ order: inverse ? 2 : 0, span: 6 }}
        lg={5}
        xl={4}
        className={classNames({ 'pe-lg-6': inverse, 'ps-lg-6': !inverse })}
      >
        <Image
          fluid
          className="px-6 px-md-0"
          src={isDark ? imageDark : image}
          alt=""
        />
      </Col>
      <Col md lg={5} xl={4} className="mt-4 mt-md-0">
        <h5 className={`text-${color}`}>
          <FontAwesomeIcon icon={icon} className="me-2" />
          {iconText}
        </h5>
        <h3>{title}</h3>
        <p>{description}</p>
        {children}
      </Col>
    </Row>
  );
};

export default Process;
