
import { Button, Row, Col } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import StarRating from 'components/common/StarRating';
import { Rating } from 'react-simple-star-rating';

const defaultRatingCode = `function DefaultRatingExample() {
  return (
    <Rating
      initialValue={2}
    />
  );
}`;
const customIconCode = `function DefaultRatingExample() {
  return (
    <Rating
      initialValue={2}
      fillIcon={
        <FontAwesomeIcon icon="star" className="text-warning fs-7 me-1" />
      }
      emptyIcon={
        <FontAwesomeIcon icon="star" className="text-300 fs-7 me-1" />
      }
    />
  );
}`;

const readOnlyCode = `function ReadOnlyExample() {
  return (
    <Rating
      readonly
      initialValue={2}
      fillIcon={
        <FontAwesomeIcon icon="heart" className="text-warning fs-7 me-1" />
      }
      emptyIcon={
        <FontAwesomeIcon icon="heart" className="text-300 fs-7 me-1" />
      }
    />
  );
}`;

const fractionalRatingCode = `function FractionalRatingExample() {
  return (
    <Rating
      initialValue={2.5}
      allowFraction={true}
      fillIcon={
        <FontAwesomeIcon icon="heart" className="text-warning fs-7 me-1" />
      }
      emptyIcon={
        <FontAwesomeIcon icon={['far','heart']} className="text-300 fs-7 me-1" />
      }
    />
  );
}`;

const oneToTenCode = `function Example() {
  return (
    <Rating
      iconsCount={10}
      initialValue={2.5}
      allowFraction={true}
      fillIcon={
        <FontAwesomeIcon icon="circle" className="text-warning fs-7 me-1" />
      }
      emptyIcon={
        <FontAwesomeIcon icon={['far','circle']} className="text-300 fs-7 me-1" />
      }
    />
  );
}`;

const rtlSupportCode = `function PlaceholderExample() {
  return (
    <Rating
      initialValue={2.5}
      allowFraction={true}
      rtl={true}
      emptyIcon={<FontAwesomeIcon icon={['far','star']} className="text-warning fs-7 me-1" />}
      fillIcon={<FontAwesomeIcon icon="star" className="text-warning fs-7 me-1" />}
    />
  );
}`;

const starRatingCode = `function StarRatingExample() {
  return (
    <StarRating
      className="fs-7"
      initialValue={3}
    />
  );
}`;

const RatingExample = () => (
  <>
    <PageHeader
      title="Rating"
      description="React-Falcon uses <strong>React Simple Rating</strong> as rating component. It's a simple react component for adding a star rating to your project."
      className="mb-3"
    >
      <Button
        href="https://github.com/awran5/react-simple-star-rating"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React Simple Rating Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Basic Example" />
          <FalconComponentCard.Body
            code={defaultRatingCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Custom Icon" />
          <FalconComponentCard.Body
            code={customIconCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Readonly rating" />
          <FalconComponentCard.Body
            code={readOnlyCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="Fractional rating" />
          <FalconComponentCard.Body
            code={fractionalRatingCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <Row className="mb-3 g-3">
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="1 to 10 rating" />
          <FalconComponentCard.Body
            code={oneToTenCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
      <Col lg={6}>
        <FalconComponentCard noGuttersBottom>
          <FalconComponentCard.Header title="RTL Support" />
          <FalconComponentCard.Body
            code={rtlSupportCode}
            scope={{ Rating, FontAwesomeIcon }}
            language="jsx"
          />
        </FalconComponentCard>
      </Col>
    </Row>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Star Rating" light={false}>
        <p className="mb-0">
          <strong>StarRating</strong> is a custom component for star rating. Use
          this component for star rating only.
        </p>
      </FalconComponentCard.Header>
      <FalconComponentCard.Body
        code={starRatingCode}
        language="jsx"
        scope={{ StarRating }}
      />
    </FalconComponentCard>
  </>
);

export default RatingExample;
