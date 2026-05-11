import React, { PropsWithChildren, ElementType } from 'react';
import classNames from 'classnames';
import { Card, Col, Row, ColProps, CardProps } from 'react-bootstrap';
import Background from './Background';
import corner4 from 'assets/img/illustrations/corner-4.png';
import createMarkup from 'helpers/createMarkup';

interface PageHeaderProps extends Omit<CardProps, 'title'> {
  title: React.ReactNode;
  preTitle?: React.ReactNode;
  titleTag?: ElementType;
  description?: string;
  image?: string;
  col?: ColProps;
}

const PageHeader = ({
  title,
  preTitle,
  titleTag: TitleTag = 'h3',
  description,
  image = corner4,
  col = { lg: 8 },
  children,
  ...rest
}: PropsWithChildren<PageHeaderProps>) => (
  <Card {...rest}>
    <Background
      image={image}
      className="bg-card d-none d-sm-block"
      style={{
        borderTopRightRadius: '0.375rem',
        borderBottomRightRadius: '0.375rem'
      }}
    />
    <Card.Body className="position-relative">
      <Row>
        <Col {...col}>
          {preTitle && <h6 className="text-600">{preTitle}</h6>}
          <TitleTag className="mb-0">{title}</TitleTag>
          {description && (
            <p
              className={classNames('mt-2', { 'mb-0': !children })}
              dangerouslySetInnerHTML={createMarkup(description)}
            />
          )}
          {children}
        </Col>
      </Row>
    </Card.Body>
  </Card>
);

export default PageHeader;
