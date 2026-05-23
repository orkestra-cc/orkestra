import { Button, OverlayTrigger, Tooltip, TooltipProps } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useAppContext } from 'providers/AppProvider';

const ToggleButton = () => {
  const { t } = useTranslation();
  const {
    config: { isNavbarVerticalCollapsed, isFluid, isRTL },
    setConfig
  } = useAppContext();

  const renderTooltip = (props: TooltipProps) => (
    <Tooltip style={{ position: 'fixed' }} id="button-tooltip" {...props}>
      {t('nav.toggleNavigation')}
    </Tooltip>
  );

  const handleClick = () => {
    document
      .getElementsByTagName('html')[0]
      .classList.toggle('navbar-vertical-collapsed');
    setConfig('isNavbarVerticalCollapsed', !isNavbarVerticalCollapsed);
  };

  return (
    <OverlayTrigger
      placement={
        isFluid ? (isRTL ? 'bottom' : 'right') : isRTL ? 'bottom' : 'left'
      }
      overlay={renderTooltip}
    >
      <div className="toggle-icon-wrapper">
        <Button
          variant="link"
          className="navbar-toggler-humburger-icon navbar-vertical-toggle"
          id="toggleNavigationTooltip"
          onClick={handleClick}
        >
          <span className="navbar-toggle-icon">
            <span className="toggle-line" />
          </span>
        </Button>
      </div>
    </OverlayTrigger>
  );
};

export default ToggleButton;
