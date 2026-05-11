import React from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import Flex from 'components/common/Flex';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';

export interface NavbarVerticalMenuItemRoute {
  name: string;
  icon?: IconProp | string;
  badge?: {
    type: BadgeColor | string;
    text: string;
  };
}

interface NavbarVerticalMenuItemProps {
  route: NavbarVerticalMenuItemRoute;
}

const NavbarVerticalMenuItem = ({ route }: NavbarVerticalMenuItemProps) => {
  return (
    <Flex alignItems="center">
      {route.icon && (
        <span className="nav-link-icon">
          <FontAwesomeIcon icon={route.icon as IconProp} />
        </span>
      )}
      <span className="nav-link-text ps-1">{route.name}</span>
      {route.badge && (
        <SubtleBadge pill bg={route.badge.type as BadgeColor} className="ms-2">
          {route.badge.text}
        </SubtleBadge>
      )}
    </Flex>
  );
};

export default React.memo(NavbarVerticalMenuItem);
