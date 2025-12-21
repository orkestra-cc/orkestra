import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import IconButton from 'components/common/IconButton';
import rawPeople from 'data/people';
import { Card } from 'react-bootstrap';
import { Link } from 'react-router';
import paths from 'routes/paths';

const AddToFeed = () => {
  const people = rawPeople.slice(0, 8);

  return (
    <Card className="mb-3">
      <Card.Header
        as={Flex}
        alignItems="center"
        justifyContent="between"
        className="bg-body-tertiary"
      >
        <h5 className="mb-0">Add to your feed</h5>
        <Link className="fs-10" to="#!">
          See all
        </Link>
      </Card.Header>
      <Card.Body>
        {people.map((person: any, index: number) => (
          <SingleItem
            {...person}
            key={person.id}
            divider={people.length === index + 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

interface SingleItemProps {
  avatarSrc: string;
  name: string;
  mutual?: number;
  divider: boolean;
}

const SingleItem = ({ avatarSrc, name, mutual, divider }: SingleItemProps) => (
  <Flex>
    <Link to={paths.userProfile}>
      <Avatar size="3xl" src={avatarSrc} />
    </Link>
    <div className="flex-1 ms-2">
      <h6 className="mb-0">
        <Link to={paths.userProfile}>{name}</Link>
      </h6>
      {!!mutual && <p className="fs-10 mb-0">{mutual} mutual connections</p>}
      <IconButton
        className="mt-1 py-0 border"
        variant="tertiary"
        size="sm"
        icon="user-plus"
        transform="shrink-5"
      >
        <span className="fs-10">Follow</span>
      </IconButton>

      {!divider && <div className="border-dashed border-bottom my-3" />}
    </div>
  </Flex>
);

export default AddToFeed;
