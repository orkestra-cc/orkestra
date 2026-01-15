import { ReactNode } from 'react';
import { Card, Tab, Row, Col, Nav, Button } from 'react-bootstrap';
import FalconCardBody from './FalconCardBody';
import classNames from 'classnames';
import { HashLink } from 'react-router-hash-link';
import Flex from './Flex';
import { useLocation } from 'react-router';
import { camelize } from '../../helpers/utils';
import { useAppContext } from 'providers/AppProvider';

const PreviewCode = () => {
  return (
    <Row className="d-inline-block">
      <Col>
        <Nav variant="pills" className="nav-pills-falcon m-0">
          <Nav.Item>
            <Nav.Link as={Button} size="sm" eventKey="preview">
              Preview
            </Nav.Link>
          </Nav.Item>
          <Nav.Item>
            <Nav.Link as={Button} size="sm" eventKey="code">
              Code
            </Nav.Link>
          </Nav.Item>
          {/* <Button variant="" type="button" size="sm">
            Preview
          </Button>
          <Button variant="" type="button" size="sm">
            Code
          </Button> */}
        </Nav>
      </Col>
    </Row>
  );
};

interface FalconComponentCardHeaderProps {
  light?: boolean;
  className?: string;
  title?: string;
  children?: ReactNode;
  noPreview?: boolean;
}

const FalconComponentCardHeader = ({
  light,
  className,
  title,
  children,
  noPreview
}: FalconComponentCardHeaderProps) => {
  const location = useLocation();
  const {
    config: { isRTL }
  } = useAppContext();
  return (
    <Card.Header
      className={classNames({ 'bg-body-tertiary': light }, className)}
    >
      <Row
        className={classNames('g-2', {
          'align-items-center': !children,
          'align-items-end ': children
        })}
      >
        <Col>
          {title && (
            <Flex>
              <h5
                className="mb-0 hover-actions-trigger text-nowrap"
                id={camelize(title)}
              >
                {isRTL ? (
                  <>
                    <HashLink
                      to={`${location.pathname}#${camelize(title)}`}
                      className="hover-actions ps-2"
                      style={{ top: 0, left: '-25px' }}
                    >
                      #
                    </HashLink>
                    {title}
                  </>
                ) : (
                  <>
                    {title}
                    <HashLink
                      to={`${location.pathname}#${camelize(title)}`}
                      className="hover-actions ps-2"
                      style={{ top: 0, right: '-25px' }}
                    >
                      #
                    </HashLink>
                  </>
                )}
              </h5>
            </Flex>
          )}
          {children}
        </Col>
        {!noPreview && (
          <Col
            className={classNames({
              'col-auto': !children,
              'col-md-auto col-12': children
            })}
          >
            <PreviewCode />
          </Col>
        )}
      </Row>
    </Card.Header>
  );
};

interface FalconComponentCardProps {
  children: ReactNode;
  multiSections?: boolean;
  noGuttersBottom?: boolean;
  [key: string]: any;
}

const FalconComponentCard = ({
  children,
  multiSections,
  noGuttersBottom,
  ...rest
}: FalconComponentCardProps) => {
  return (
    <Card className={classNames({ 'mb-3': !noGuttersBottom })} {...rest}>
      {multiSections ? (
        <>{children}</>
      ) : (
        <Tab.Container defaultActiveKey="preview">{children}</Tab.Container>
      )}
    </Card>
  );
};

FalconComponentCard.Header = FalconComponentCardHeader;
FalconComponentCard.Body = FalconCardBody;

export default FalconComponentCard;
