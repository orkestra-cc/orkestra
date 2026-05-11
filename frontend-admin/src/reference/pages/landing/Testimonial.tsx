
import { Row, Col } from 'react-bootstrap';
import Section from 'components/common/Section';
import Slider from 'react-slick';
import { isIterableArray } from 'helpers/utils';
import 'slick-carousel/slick/slick.css';
import 'slick-carousel/slick/slick-theme.css';
import testimonials from 'data/testimonial/testimonialList';

interface TestimonialItemProps {
  description: string;
  author: string;
  designation: string;
  companyImage: string;
  size: number;
}

const TestimonialItem: React.FC<TestimonialItemProps> = ({
  description,
  author,
  designation,
  companyImage,
  size
}) => {
  return (
    <div className="px-5 px-sm-6">
      <p className="fs-sm-8 fs-md-7 fst-italic text-1100">{description}</p>
      <p className="fs-9 text-600">
        - {author}, {designation}
      </p>
      <img className="w-auto mx-auto" src={companyImage} alt="" height={size} />
    </div>
  );
};

const settings = {
  infinite: true,
  speed: 500,
  slidesToShow: 1,
  slidesToScroll: 1,
  autoplay: true
};

const Testimonial: React.FC = () => (
  <Section className="text-center bg-200">
    <Row className="justify-content-center">
      <Col xs={10} lg={9} xl={8}>
        <Slider {...settings}>
          {isIterableArray(testimonials) &&
            (testimonials as TestimonialItemProps[]).map((testimonial, index) => (
              <TestimonialItem {...testimonial} key={index} />
            ))}
        </Slider>
      </Col>
    </Row>
  </Section>
);

export default Testimonial;
