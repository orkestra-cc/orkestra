
import { Link } from 'react-router';
import Avatar from 'components/common/Avatar';
import Flex from 'components/common/Flex';
import paths from 'routes/paths';

interface User {
  id?: number;
  name: string;
  avatarSrc: string | string[];
  status?: string;
}

interface ChatContentBodyIntroProps {
  user: User;
  isGroup: boolean;
}

const ChatContentBodyIntro = ({ user, isGroup }: ChatContentBodyIntroProps) => (
  <Flex
    alignItems="center"
    className="position-relative p-3 border-bottom mb-3"
  >
    <Avatar className={`${user.status} me-3`} size="2xl" src={user.avatarSrc} />
    <div className="flex-1">
      <h6 className="mb-0">
        <Link
          to={paths.userProfile}
          className=" text-decoration-none stretched-link text-700"
        >
          {user.name}
        </Link>
      </h6>
      <p className="mb-0">
        {isGroup
          ? `You are a member of ${user.name}. Say hi to start conversation to the group.`
          : `You friends with ${user.name}. Say hi to start the conversation`}
      </p>
    </div>
  </Flex>
);

export default ChatContentBodyIntro;
