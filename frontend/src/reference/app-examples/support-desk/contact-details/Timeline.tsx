
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { timeline } from 'data/support-desk/contactDetailsData';
import { Col, OverlayTrigger, Row, Tooltip } from 'react-bootstrap';
import { Link } from 'react-router';
import paths from 'routes/paths';

const Timeline = () => {
  return (
    <div className="timeline-vertical py-0">
      {timeline.map((item: any, index: number) => {
        const {
          subject,
          description,
          time,
          icon,
          year,
          day,
          priority,
          status
        } = item;
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
                index % 2 == 0 ? 'timeline-item-start' : 'timeline-item-end'
              }`}
            >
              <Col lg={6} className="timeline-item-time">
                <div>
                  <h6 className="mb-0 text-700"> {year}</h6>
                  <p className="fs-11 text-500 font-sans-serif">{day}</p>
                </div>
              </Col>
              <Col lg={6}>
                <div className="timeline-item-content arrow-bg-white">
                  <div className="timeline-item-card bg-white dark__bg-1100">
                    <Link to={paths.ticketsPreview}>
                      <h5 className="mb-2 hover-primary">{subject}</h5>
                    </Link>
                    <p className="border-bottom mb-3 pb-4 text-600 fs-10">
                      {description}
                    </p>
                    <Flex wrap="wrap" className="pt-2">
                      <h6 className="mb-0 text-600 lh-base">
                        <FontAwesomeIcon
                          icon={['far', 'clock']}
                          className="me-1"
                        />
                        {time}
                      </h6>
                      <Flex
                        alignItems="center"
                        className="ms-auto me-2 me-sm-x1 me-xl-2 me-xxl-x1"
                      >
                        <OverlayTrigger
                          placement="top"
                          overlay={
                            <Tooltip style={{ position: 'fixed' }}>
                              {priority.text}
                            </Tooltip>
                          }
                        >
                          <div
                            className={`dot me-0 me-sm-2 me-xl-0 me-xxl-2 bg-${priority.color}`}
                          ></div>
                        </OverlayTrigger>
                        <h6 className="mb-0 text-700 d-none d-sm-block d-xl-none d-xxl-block">
                          {priority.text}
                        </h6>
                      </Flex>
                      <SubtleBadge bg={status.type}>
                        {status.content}
                      </SubtleBadge>
                    </Flex>
                  </div>
                </div>
              </Col>
            </Row>
          </div>
        );
      })}
    </div>
  );
};

export default Timeline;
