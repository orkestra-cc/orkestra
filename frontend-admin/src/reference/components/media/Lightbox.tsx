
import { Button } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import OrkestraComponentCard from 'components/common/OrkestraComponentCard';
import img1 from 'assets/img/generic/4.jpg';
import img2 from 'assets/img/generic/5.jpg';
import img11 from 'assets/img/generic/11.jpg';
import img3 from 'assets/img/gallery/4.jpg';
import img4 from 'assets/img/gallery/5.jpg';
import img5 from 'assets/img/gallery/3.jpg';
import OrkestraLightBoxGallery from 'components/common/OrkestraLightBoxGallery';
import OrkestraLightBox from 'components/common/OrkestraLightBox';

const galleryCode = `<OrkestraLightBoxGallery images={images}>
  {setImgIndex => (
    <Row className="g-2">
      <Col xs={6}>
        <Image
          src={images[0]}
          fluid
          rounded
          className="cursor-pointer"
          onClick={() => setImgIndex(0)}
        />
      </Col>
      <Col xs={6}>
        <Image
          src={images[1]}
          fluid
          rounded
          className="cursor-pointer"
          onClick={() => setImgIndex(1)}
        />
      </Col>
      <Col xs={4}>
        <Image
          src={images[2]}
          fluid
          rounded
          className="cursor-pointer"
          onClick={() => setImgIndex(2)}
        />
      </Col>
      <Col xs={4}>
        <Image
          src={images[3]}
          fluid
          rounded
          className="cursor-pointer"
          onClick={() => setImgIndex(3)}
        />
      </Col>
      <Col xs={4}>
        <Image
          src={images[4]}
          fluid
          rounded
          className="cursor-pointer"
          onClick={() => setImgIndex(4)}
        />
      </Col>
    </Row>
  )}
</OrkestraLightBoxGallery>`;

const simpleImageCode = ` <OrkestraLightBox image={image}>
  <Image src={image} fluid rounded width={300} />
</OrkestraLightBox>`;

const Lightbox = () => {
  const images = [img1, img2, img3, img4, img5];
  const image = img11;

  return (
    <>
      <PageHeader
        title="Lightbox"
        description="React-Orkestra uses <strong>yet-another-react-lightbox</strong> for lightbox. Yet Another React Lightbox is a modern React lightbox component. Performant, easy to use, customizable and extendable."
        className="mb-3"
      >
        <Button
          href="https://yet-another-react-lightbox.com/"
          target="_blank"
          variant="link"
          size="sm"
          className="ps-0"
        >
          Yet Another React Lightbox Documentation
          <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
        </Button>
      </PageHeader>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Gallery" />
        <OrkestraComponentCard.Body
          code={galleryCode}
          scope={{ OrkestraLightBoxGallery, images }}
          language="jsx"
        />
      </OrkestraComponentCard>

      <OrkestraComponentCard>
        <OrkestraComponentCard.Header title="Simple Image" />
        <OrkestraComponentCard.Body
          code={simpleImageCode}
          scope={{ OrkestraLightBox, image }}
          language="jsx"
        />
      </OrkestraComponentCard>
    </>
  );
};

export default Lightbox;
