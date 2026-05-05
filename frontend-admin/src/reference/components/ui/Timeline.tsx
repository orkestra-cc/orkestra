
import PageHeader from 'components/common/PageHeader';
import { Card, Col, Image, Row } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  VerticalTimelineData,
  ZigzagTimelineData
} from 'data/timeline/timeline';
import classNames from 'classnames';
import FalconComponentCard from 'components/common/FalconComponentCard';

const Timeline = () => {
  return (
    <>
      <PageHeader
        title="Timeline"
        description="Create timeline, stories, roadmap, history with Falcon’s exclusive timeline component."
        className="mb-3"
      />
      <Card className="mb-3">
        <FalconComponentCard.Header
          className="bg-body-tertiary"
          title="Example"
          noPreview
        />
        <Card.Body className="px-sm-4 px-md-8 px-lg-6 px-xxl-8">
          <div className="timeline-vertical">
            {VerticalTimelineData.map((item: { year: string; date: string; title: string; description: string; icon: any }, index: number) => {
              const { year, date, title, description, icon } = item;
              return (
                <div
                  key={index}
                  className={classNames('timeline-item', {
                    'timeline-item-start': index % 2 === 0,
                    'timeline-item-end': index % 2 !== 0
                  })}
                >
                  <div className="timeline-icon icon-item icon-item-lg text-primary border-300">
                    <FontAwesomeIcon icon={icon} className="fs-8" />
                  </div>
                  <Row
                    className={` ${
                      index % 2 == 0
                        ? 'timeline-item-start'
                        : 'timeline-item-end'
                    }`}
                  >
                    <Col lg={6} className="timeline-item-time">
                      <div>
                        <p className="fs-10 mb-0 fw-semibold"> {year}</p>
                        <p className="fs-11 text-600">{date}</p>
                      </div>
                    </Col>
                    <Col lg={6}>
                      <div className="timeline-item-content">
                        <div className="timeline-item-card">
                          <h5 className="mb-2">{title}</h5>
                          <p className="fs-10 mb-0">{description}</p>
                        </div>
                      </div>
                    </Col>
                  </Row>
                </div>
              );
            })}
          </div>
        </Card.Body>
      </Card>

      <Card>
        <FalconComponentCard.Header
          className="bg-body-tertiary"
          title="Timeline with media"
          noPreview
        />

        <Card.Body className="px-xxl-8 px-md-8 px-lg-6">
          <div className="timeline-zigzag">
            {ZigzagTimelineData.map((item: { title: string; description: string; year: string; img: { img: string; width: number; height: number } }, index: number) => {
              const { title, description, year } = item;
              const { img, width, height } = item.img;
              return (
                <Row
                  key={index}
                  className={classNames('timeline-item', {
                    'timeline-item-end': index % 2 === 0,
                    'timeline-item-start': index % 2 !== 0
                  })}
                >
                  <Col lg={6} className="timeline-item-content">
                    <span className="bullet" />
                    <Row className="g-0 mt-n4">
                      <Col className="timeline-item-text">
                        <h3 className="text-primary mt-4 mt-sm-0">{year}</h3>
                        <h6 className="mt-2 mb-1 mt-sm-3">{title}</h6>
                        <p className="fs-10">{description}</p>
                      </Col>
                      <Col sm="auto">
                        <Image
                          src={img}
                          alt=""
                          rounded
                          className="timeline-item-media ms-3 ms-sm-0"
                          height={height || '188px'}
                          width={width || '250px'}
                        />
                      </Col>
                    </Row>
                  </Col>
                </Row>
              );
            })}
          </div>
        </Card.Body>
      </Card>
    </>
  );
};

export default Timeline;
