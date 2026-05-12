import { Card, Col, Image, Row } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import images from 'data/gallery';
import OrkestraLightBoxGallery from 'components/common/OrkestraLightBoxGallery';

const Photos: React.FC = () => {
  return (
    <Card className="mt-3">
      <OrkestraCardHeader title="Photos" light />
      <Card.Body>
        <OrkestraLightBoxGallery images={images}>
          {setImgIndex => (
            <Row className="g-2">
              <Col xs={6}>
                <Image
                  className="cursor-pointer"
                  src={images[0]}
                  fluid
                  rounded
                  onClick={() => setImgIndex(0)}
                />
              </Col>
              <Col xs={6}>
                <Image
                  className="cursor-pointer"
                  src={images[1]}
                  fluid
                  rounded
                  onClick={() => setImgIndex(1)}
                />
              </Col>
              <Col xs={4}>
                <Image
                  className="cursor-pointer"
                  src={images[2]}
                  fluid
                  rounded
                  onClick={() => setImgIndex(2)}
                />
              </Col>
              <Col xs={4}>
                <Image
                  className="cursor-pointer"
                  src={images[3]}
                  fluid
                  rounded
                  onClick={() => setImgIndex(3)}
                />
              </Col>
              <Col xs={4}>
                <Image
                  className="cursor-pointer"
                  src={images[4]}
                  fluid
                  rounded
                  onClick={() => setImgIndex(4)}
                />
              </Col>
            </Row>
          )}
        </OrkestraLightBoxGallery>
      </Card.Body>
    </Card>
  );
};

export default Photos;
