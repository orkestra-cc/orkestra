import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import coverSrc from 'assets/img/generic/4.jpg';
import apple from 'assets/img/logos/apple.png';
import google from 'assets/img/logos/g.png';
import hp from 'assets/img/logos/hp.png';
import avatar from 'assets/img/team/2.jpg';
import Flex from 'components/common/Flex';
import VerifiedBadge from 'components/common/VerifiedBadge';
import React, { useState } from 'react';
import { Button, Col, Row } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import ProfileBanner from '../ProfileBanner';

const Banner: React.FC = () => {
  const { t } = useTranslation();
  const [rightSidedItems] = useState([
    {
      title: 'Google',
      image: google
    },
    {
      title: 'Apple',
      image: apple
    },
    {
      title: ' Hewlett Packard',
      image: hp
    }
  ]);
  return (
    <ProfileBanner>
      <ProfileBanner.Header avatar={avatar} coverSrc={coverSrc} />
      <ProfileBanner.Body>
        <Row>
          <Col lg={8}>
            <h4 className="mb-1">
              {t('userProfileScaffold.banner.userName')} <VerifiedBadge />
            </h4>
            <h5 className="fs-9 fw-normal">
              {t('userProfileScaffold.banner.userTitle')}
            </h5>
            <p className="text-500">
              {t('userProfileScaffold.banner.userLocation')}
            </p>
            <Button variant="orkestra-primary" size="sm" className="px-3">
              {t('userProfileScaffold.banner.following')}
            </Button>
            <Button variant="orkestra-default" size="sm" className="px-3 ms-2">
              {t('userProfileScaffold.banner.message')}
            </Button>
            <div className="border-dashed border-bottom my-4 d-lg-none" />
          </Col>
          <Col className="ps-2 ps-lg-3">
            <Link to="#!" className="link-900">
              <Flex alignItems="center" className="mb-2">
                <FontAwesomeIcon
                  icon="user-circle"
                  className="me-2 text-700"
                  style={{ height: '30px', width: '30px' }}
                />
                <div className="flex-1">
                  <h6 className="mb-0">
                    {t('userProfileScaffold.banner.seeFollowersCount')}
                  </h6>
                </div>
              </Flex>
            </Link>
            {rightSidedItems.map(item => (
              <Link to="#!" key={item.title} className="link-900">
                <Flex alignItems="center" className="mb-2">
                  <img
                    src={item.image}
                    alt={item.title}
                    width={30}
                    className="me-2"
                  />
                  <div className="flex-1">
                    <h6 className="mb-0">{item.title}</h6>
                  </div>
                </Flex>
              </Link>
            ))}
          </Col>
        </Row>
      </ProfileBanner.Body>
    </ProfileBanner>
  );
};

export default Banner;
