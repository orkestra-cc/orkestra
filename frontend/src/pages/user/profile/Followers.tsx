
import { Card, Col, Row } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { Link } from 'react-router';
import Follower from 'reference/app-examples/social/followers/Follower';
import paths from 'routes/paths';

interface FollowersProps {
  totalFollowers: number;
  followers: any[];
  colBreakpoints?: { xs?: number; md?: number; lg?: number; xxl?: number };
}

const Followers = ({
  totalFollowers,
  followers,
  colBreakpoints = { xs: 6, md: 4, lg: 3, xxl: 2 }
}: FollowersProps) => {
  return (
    <Card className="p-0">
      <Card.Header className="bg-body-tertiary">
        <Flex justifyContent="between">
          <h5 className="mb-0">Followers ({totalFollowers}) </h5>
          <Link to={paths.followers} className="font-sans-serif">
            All Members
          </Link>
        </Flex>
      </Card.Header>
      <Card.Body className="bg-body-tertiary px-1 pb-1 pt-0 fs-10">
        <Row className="gx-0 gy-1 text-center">
          {followers.map((follower: any) => (
            <Col key={follower.id} {...colBreakpoints}>
              <Follower follower={follower} />
            </Col>
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};


export default Followers;
