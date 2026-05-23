import React from 'react';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { useTranslation } from 'react-i18next';
import Flex from 'components/common/Flex';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';
import { translateNavItem } from 'helpers/navLabel';

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
  const { t } = useTranslation();
  return (
    <Flex alignItems="center">
      {route.icon && (
        <span className="nav-link-icon">
          <FontAwesomeIcon icon={route.icon as IconProp} />
        </span>
      )}
      <span className="nav-link-text ps-1">
        {translateNavItem(t, route.name)}
      </span>
      {route.badge && (
        <SubtleBadge pill bg={route.badge.type as BadgeColor} className="ms-2">
          {route.badge.text}
        </SubtleBadge>
      )}
    </Flex>
  );
};

export default React.memo(NavbarVerticalMenuItem);
