import { useState } from 'react';
import { Card, Row, Col } from 'react-bootstrap';
import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import CardDropdown from 'components/common/CardDropdown';
import { statsData } from 'data/dashboard/crm';
import classNames from 'classnames';
import IconItem from 'components/common/icon/IconItem';
import StatsChart from './StatsChart';
import { useAppContext } from 'providers/AppProvider';

// TypeScript interfaces
interface CrmStatData {
  id: number;
  title: string;
  amount: number;
  target: string;
  icon: IconProp;
  caret: IconProp;
  color: 'primary' | 'success' | 'info' | 'warning' | 'danger' | 'secondary';
  caretColor: 'primary' | 'success' | 'info' | 'warning' | 'danger' | 'secondary';
  data: number[];
}

interface StatsItemProps {
  stat: CrmStatData;
}

const StatsItem = ({ stat }: StatsItemProps) => {
  const { icon, color, title, amount, caret, caretColor, target, data } = stat;
  const { getThemeColor } = useAppContext();
  return (
    <>
      <Flex
        justifyContent="center"
        alignItems="center"
        className="mb-3 position-static"
      >
        <IconItem
          tag="div"
          icon={icon}
          bg={`${color}-subtle`}
          color={color}
          size="sm"
          iconClass="fs-11"
          className="me-2 shadow-none"
        />
        <h6 className="mb-0 flex-1">{title}</h6>
        <div>
          <CardDropdown />
        </div>
      </Flex>
      <Flex>
        <p className="font-sans-serif lh-1 mb-1 fs-5 pe-2">{amount}%</p>
        <div className="d-flex flex-column">
          <FontAwesomeIcon
            icon={caret}
            className={`me-1 mb-0 text-${caretColor}`}
          />
          <p className="fs-11 mb-0 mt-0 text-nowrap">{target}</p>
        </div>
        <div className="w-100 ms-2">
          <StatsChart color={getThemeColor(color)} data={data} />
        </div>
      </Flex>
    </>
  );
};

const CrmStats = () => {
  const [stats] = useState<CrmStatData[]>(statsData);
  return (
    <Card>
      <Card.Body>
        <Row>
          {stats.map((stat, index) => (
            <Col
              lg={4}
              key={stat.id}
              className={classNames({
                'border-bottom border-lg-0 border-lg-end':
                  index !== stats.length - 1,
                'pb-3 pb-lg-0': index === 0,
                'py-3 py-lg-0': index === 1,
                'pt-3 pt-lg-0': index === 2
              })}
            >
              <StatsItem stat={stat} />
            </Col>
          ))}
        </Row>
      </Card.Body>
    </Card>
  );
};

export default CrmStats;
