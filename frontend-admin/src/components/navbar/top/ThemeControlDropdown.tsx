import { Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useTranslation } from 'react-i18next';
import { themeVariants, ThemeVariant } from 'config';
import { useAppContext } from 'providers/AppProvider';

interface ThemeControlDropdownProps {
  dropdownClassName?: string;
  iconClassName?: string;
}

const ThemeControlDropdown = ({
  dropdownClassName = '',
  iconClassName = 'fs-8'
}: ThemeControlDropdownProps) => {
  const { t } = useTranslation();
  const {
    config: { theme },
    changeTheme
  } = useAppContext();

  return (
    <Dropdown
      navbar={true}
      as="div"
      onSelect={colorMode =>
        colorMode && changeTheme(colorMode as ThemeVariant)
      }
      className={`theme-control-dropdown ${dropdownClassName}`}
    >
      <Dropdown.Toggle
        bsPrefix="toggle"
        variant="link"
        className="nav-link dropdown-toggle d-flex align-items-center pe-1"
      >
        <FontAwesomeIcon
          icon={
            theme === 'light' ? 'sun' : theme === 'dark' ? 'moon' : 'adjust'
          }
          className={iconClassName}
        />
      </Dropdown.Toggle>

      <Dropdown.Menu className="dropdown-caret dropdown-menu-card dropdown-menu-end mt-2">
        <div className="bg-white rounded-2 py-2 dark__bg-1000">
          {themeVariants.map(colorMode => (
            <Dropdown.Item
              key={colorMode}
              active={theme === colorMode}
              eventKey={colorMode}
              className="link-600 fs-10 d-flex align-items-center gap-2"
            >
              <FontAwesomeIcon
                icon={
                  colorMode === 'light'
                    ? 'sun'
                    : colorMode === 'dark'
                      ? 'moon'
                      : 'adjust'
                }
              />
              {t(`nav.theme.${colorMode}` as const)}
              {theme === colorMode && (
                <FontAwesomeIcon icon="check" className="ms-auto text-600" />
              )}
            </Dropdown.Item>
          ))}
        </div>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default ThemeControlDropdown;
