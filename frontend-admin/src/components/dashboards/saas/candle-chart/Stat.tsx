
import Flex from 'components/common/Flex';
import { Col } from 'react-bootstrap';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import classNames from 'classnames';
import { IconProp } from '@fortawesome/fontawesome-svg-core';

interface GrowInfo {
  color: BadgeColor;
  isGrow: boolean;
  growAmount: number;
}

interface StatInfo {
  id: string | number;
  title: string;
  grow: GrowInfo;
  amount: string;
}

interface StatProps {
  statInfo: StatInfo;
  isLast: boolean;
}

const Stat = ({ statInfo: { title, grow, amount }, isLast }: StatProps) => {
  return (
    <Col xs={12} sm="auto">
      <div
        className={classNames('mb-3', {
          'pe-4 border-sm-end border-200': !isLast,
          'pe-0': isLast
        })}
      >
        <h6 className="fs-11 text-600 mb-1">{title}</h6>
        <Flex alignItems="center">
          <h5 className="fs-9 text-900 mb-0 me-2">{amount}</h5>
          <SubtleBadge bg={grow.color} pill>
            <FontAwesomeIcon
              icon={(grow.isGrow ? 'caret-up' : 'caret-down') as IconProp}
            />{' '}
            {grow.growAmount}%
          </SubtleBadge>
        </Flex>
      </div>
    </Col>
  );
};

export default Stat;
