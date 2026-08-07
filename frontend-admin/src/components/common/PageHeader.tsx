import React, { PropsWithChildren, ElementType } from 'react';
import classNames from 'classnames';
import { Card, Col, Row, ColProps, CardProps } from 'react-bootstrap';
import Background from './Background';
import createMarkup from 'helpers/createMarkup';

interface PageHeaderProps extends Omit<CardProps, 'title'> {
  title: React.ReactNode;
  preTitle?: React.ReactNode;
  titleTag?: ElementType;
  description?: string;
  // Plain card surface by default — "nothing decorates" (DESIGN.md). Pass an
  // image explicitly for the rare surface that genuinely earns one.
  image?: string | null;
  col?: ColProps;
}

const PageHeader = ({
  title,
  preTitle,
  titleTag: TitleTag = 'h3',
  description,
  image = null,
  col = { lg: 8 },
  children,
  ...rest
}: PropsWithChildren<PageHeaderProps>) => (
  <Card {...rest}>
    {image && (
      <Background
        image={image}
        className="bg-card d-none d-sm-block"
        style={{
          borderTopRightRadius: '0.375rem',
          borderBottomRightRadius: '0.375rem'
        }}
      />
    )}
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
