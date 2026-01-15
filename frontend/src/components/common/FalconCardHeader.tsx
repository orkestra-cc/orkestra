
import { Col, Card, Row } from 'react-bootstrap';
import classNames from 'classnames';

type TitleTag = 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6';
type Breakpoint = 'sm' | 'md' | 'lg' | 'xl' | 'xxl';

interface TitleProps {
  titleTag?: TitleTag;
  className?: string;
  breakPoint?: Breakpoint;
  children?: React.ReactNode;
}

const Title: React.FC<TitleProps> = ({
  titleTag: TitleTag = 'h5',
  className,
  breakPoint,
  children
}) => (
  <TitleTag
    className={classNames(
      {
        'mb-0': !breakPoint,
        [`mb-${breakPoint}-0`]: !!breakPoint
      },
      className
    )}
  >
    {children}
  </TitleTag>
);

interface FalconCardHeaderProps {
  title?: React.ReactNode;
  light?: boolean;
  titleTag?: TitleTag;
  titleClass?: string;
  className?: string;
  breakPoint?: Breakpoint;
  endEl?: React.ReactNode;
  children?: React.ReactNode;
}

const FalconCardHeader: React.FC<FalconCardHeaderProps> = ({
  title,
  light,
  titleTag,
  titleClass,
  className,
  breakPoint,
  endEl,
  children
}) => (
  <Card.Header className={classNames(className, { 'bg-body-tertiary': light })}>
    {endEl ? (
      <Row className="align-items-center g-2">
        <Col>
          <Title
            breakPoint={breakPoint}
            titleTag={titleTag}
            className={titleClass}
          >
            {title}
          </Title>
          {children}
        </Col>
        <Col
          {...{ [breakPoint ? breakPoint : 'xs']: 'auto' }}
          className={`text${breakPoint ? `-${breakPoint}` : ''}-right`}
        >
          {endEl}
        </Col>
      </Row>
    ) : (
      <Title breakPoint={breakPoint} titleTag={titleTag} className={titleClass}>
        {title}
      </Title>
    )}
  </Card.Header>
);

export default FalconCardHeader;
