
import { Card, CardProps } from 'react-bootstrap';
import classNames from 'classnames';
import Background from 'components/common/Background';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import CountUp from 'react-countup';

interface StatData {
  title: string;
  value: number;
  decimal?: boolean;
  suffix?: string;
  prefix?: string;
  valueClassName?: string;
  linkText: string;
  link: string;
  badgeText?: string;
  badgeBg?: BadgeColor;
  image?: string;
  className?: string;
}

interface StatisticsCardProps extends CardProps {
  stat: StatData;
}

const StatisticsCard = ({ stat, ...rest }: StatisticsCardProps) => {
  const {
    title,
    value,
    decimal,
    suffix,
    prefix,
    valueClassName,
    linkText,
    link,
    badgeText,
    badgeBg,
    image,
    className
  } = stat;
  return (
    <Card className={classNames(className, 'overflow-hidden')} {...rest}>
      <Background image={image} className="bg-card" />
      <Card.Body className="position-relative">
        <h6>
          {title}
          {badgeText && (
            <SubtleBadge bg={badgeBg} pill className="ms-2">
              {badgeText}
            </SubtleBadge>
          )}
        </h6>
        <div
          className={classNames(
            valueClassName,
            'display-4 fs-5 mb-2 fw-normal font-sans-serif'
          )}
        >
          <CountUp
            start={0}
            end={value}
            duration={2.75}
            suffix={suffix}
            prefix={prefix}
            separator=","
            decimals={decimal ? 2 : 0}
            decimal="."
          />
        </div>
        <Link to={link} className="fw-semibold fs-10 text-nowrap">
          {linkText}
          <FontAwesomeIcon
            icon="angle-right"
            className="ms-1"
            transform="down-1"
          />
        </Link>
      </Card.Body>
    </Card>
  );
};

export default StatisticsCard;
