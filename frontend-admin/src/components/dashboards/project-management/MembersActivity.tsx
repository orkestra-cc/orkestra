import { Card, Row, Col, Image } from 'react-bootstrap';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import CardDropdown from 'components/common/CardDropdown';
import SimpleBar from 'simplebar-react';
import OrkestraLink from 'components/common/OrkestraLink';
import Avatar from 'components/common/Avatar';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';

interface ActivityItem {
  id: string | number;
  img: string;
  color: BadgeColor;
  amount: string | number;
}

interface ActivityData {
  id: string | number;
  name: string;
  avatar: string;
  activity: ActivityItem[];
}

interface ActivityProps {
  activity: ActivityData;
}

const Activity = ({ activity: { name, avatar, activity } }: ActivityProps) => {
  return (
    <Row className="g-2 mb-4">
      <Col xs={12} className="d-flex align-items-center">
        <Avatar size="xl" rounded="circle" src={avatar} />
        <h6 className="mb-0 ps-2">{name}</h6>
      </Col>
      {activity.map((item: ActivityItem) => (
        <Col key={item.id} xs={4} className="position-relative">
          <Image src={item.img} alt={name} className="w-100" />
          <SubtleBadge
            bg={item.color}
            pill
            className="position-absolute top-100 start-50 translate-middle"
          >
            {item.amount}
          </SubtleBadge>
        </Col>
      ))}
    </Row>
  );
};

interface MembersActivityProps {
  data: ActivityData[];
}

const MembersActivity = ({ data }: MembersActivityProps) => {
  return (
    <Card className="h-100 members-activity">
      <OrkestraCardHeader
        className="py-2"
        light
        title="Members Activity"
        titleTag="h6"
        endEl={<CardDropdown />}
      />
      <SimpleBar>
        <Card.Body>
          {data.map((activity: ActivityData) => (
            <Activity key={activity.id} activity={activity} />
          ))}
        </Card.Body>
      </SimpleBar>
      <Card.Footer className="bg-body-tertiary p-0">
        <OrkestraLink title="See all projects" className="d-block py-2" />
      </Card.Footer>
    </Card>
  );
};

export default MembersActivity;
