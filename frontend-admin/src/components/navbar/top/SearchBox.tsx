import { Fragment, useEffect, useState } from 'react';
import classNames from 'classnames';
import { Form, Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import Fuse from 'fuse.js';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import Avatar from 'components/common/Avatar';
import { isIterableArray } from 'helpers/utils';
import Flex from 'components/common/Flex';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';
import SubtleBadge, { BadgeColor } from 'components/common/SubtleBadge';

interface SearchItem {
  id: string | number;
  title?: string;
  text?: string;
  time?: string;
  url: string;
  catagories?: string;
  breadCrumbTexts?: string[];
  file?: boolean;
  img?: string;
  imgAttrs?: { class: string };
  icon?: { img: string; status?: string };
  type?: BadgeColor;
  key?: string;
}

interface MediaSearchContentProps {
  item: SearchItem;
}

const MediaSearchContent = ({ item }: MediaSearchContentProps) => {
  return (
    <Dropdown.Item className="px-x1 py-2" as={Link} to={item.url}>
      <Flex alignItems="center">
        {item.file && (
          <div className="file-thumbnail">
            <img src={item.img} alt="" className={item.imgAttrs?.class} />
          </div>
        )}
        {item.icon && (
          <Avatar src={item.icon.img} size="l" className={item.icon.status} />
        )}

        <div className="ms-2">
          <h6 className="mb-0">{item.title}</h6>
          <p
            className="fs-11 mb-0"
            dangerouslySetInnerHTML={{ __html: item.text ?? item.time ?? '' }}
          />
        </div>
      </Flex>
    </Dropdown.Item>
  );
};

interface SearchBoxProps {
  autoCompleteItem: SearchItem[];
}

const SearchBox = ({ autoCompleteItem }: SearchBoxProps) => {
  const { t } = useTranslation();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [searchInputValue, setSearchInputValue] = useState('');
  const [resultItem, setResultItem] = useState<SearchItem[]>(autoCompleteItem);

  const fuseJsOptions = {
    includeScore: true,
    keys: ['title', 'text', 'breadCrumbTexts']
  };

  const searchResult: SearchItem[] = new Fuse(autoCompleteItem, fuseJsOptions)
    .search(searchInputValue)
    .map(item => item.item);

  const recentlyBrowsedItems = resultItem.filter(
    item => item.catagories === 'recentlyBrowsedItems'
  );

  const suggestedFilters = resultItem.filter(
    item => item.catagories === 'suggestedFilters'
  );

  const suggestionFiles = resultItem.filter(
    item => item.catagories === 'suggestionFiles'
  );

  const suggestionMembers = resultItem.filter(
    item => item.catagories === 'suggestionMembers'
  );

  useEffect(() => {
    if (searchInputValue) {
      setResultItem(searchResult);
      isIterableArray(searchResult) && setDropdownOpen(true);
    } else {
      setResultItem(autoCompleteItem);
    }
  }, [searchInputValue]);

  return (
    <Dropdown
      show={dropdownOpen}
      className="search-box"
      onToggle={() => setDropdownOpen(!dropdownOpen)}
    >
      <Dropdown.Toggle as="div" className="dropdown-caret-none">
        <Form className="position-relative">
          <Form.Control
            type="search"
            name="navbar-search"
            id="navbar-search"
            placeholder={t('nav.search.placeholder')}
            aria-label={t('nav.search.placeholder')}
            className="rounded-pill search-input"
            value={searchInputValue}
            onChange={({ target }) => setSearchInputValue(target.value)}
          />
          <FontAwesomeIcon
            icon="search"
            className="position-absolute text-400 search-box-icon"
          />
          {(dropdownOpen || searchInputValue) && (
            <div className="search-box-close-btn-container">
              <OrkestraCloseButton
                size="sm"
                noOutline
                className="fs-11"
                onClick={() => {
                  setSearchInputValue('');
                  setDropdownOpen(false);
                }}
              />
            </div>
          )}
        </Form>
      </Dropdown.Toggle>
      <Dropdown.Menu>
        <div className="scrollbar py-3" style={{ maxHeight: '24rem' }}>
          {isIterableArray(recentlyBrowsedItems) && (
            <>
              <Dropdown.Header as="h6" className="px-x1 pt-0 pb-2 fw-medium">
                {t('nav.search.recentlyBrowsed')}
              </Dropdown.Header>
              {recentlyBrowsedItems.map(item => (
                <Dropdown.Item
                  as={Link}
                  to={item.url}
                  className="fs-10 px-x1 py-1 hover-primary"
                  key={item.id}
                >
                  <Flex alignItems="center">
                    <FontAwesomeIcon
                      icon="circle"
                      className="me-2 text-300 fs-11"
                    />
                    <div className="fw-normal">
                      {item.breadCrumbTexts?.map((breadCrumbText, index) => {
                        return (
                          <Fragment key={breadCrumbText}>
                            {breadCrumbText}
                            {(item.breadCrumbTexts?.length ?? 0) - 1 >
                              index && (
                              <FontAwesomeIcon
                                icon="chevron-right"
                                className="mx-1 text-500 fs-11"
                                transform="shrink 2"
                              />
                            )}
                          </Fragment>
                        );
                      })}
                    </div>
                  </Flex>
                </Dropdown.Item>
              ))}
              {(isIterableArray(suggestedFilters) ||
                isIterableArray(suggestionFiles) ||
                isIterableArray(suggestionMembers)) && (
                <hr className="text-200 dark__text-900" />
              )}
            </>
          )}

          {isIterableArray(suggestedFilters) && (
            <>
              <Dropdown.Header as="h6" className="px-x1 pt-0 pb-2 fw-medium">
                {t('nav.search.suggestedFilter')}
              </Dropdown.Header>
              {suggestedFilters.map(item => (
                <Dropdown.Item
                  as={Link}
                  to={item.url}
                  className="fs-9 px-x1 py-1"
                  key={item.id}
                >
                  <Flex alignItems="center">
                    <SubtleBadge
                      bg={item.type}
                      className="fw-medium text-decoration-none me-2"
                    >
                      {item.key}:{' '}
                    </SubtleBadge>
                    <div className="flex-1 fs-10">{item.text}</div>
                  </Flex>
                </Dropdown.Item>
              ))}
              {(isIterableArray(suggestionFiles) ||
                isIterableArray(suggestionMembers)) && (
                <hr className="text-200 dark__text-900" />
              )}
            </>
          )}

          {isIterableArray(suggestionFiles) && (
            <>
              <Dropdown.Header as="h6" className="px-x1 pt-0 pb-2 fw-medium">
                {t('nav.search.files')}
              </Dropdown.Header>
              {suggestionFiles.map(item => (
                <MediaSearchContent item={item} key={item.id} />
              ))}
              {isIterableArray(suggestionMembers) && (
                <hr className="text-200 dark__text-900" />
              )}
            </>
          )}

          {isIterableArray(suggestionMembers) && (
            <>
              <Dropdown.Header as="h6" className="px-x1 pt-0 pb-2 fw-medium">
                {t('nav.search.members')}
              </Dropdown.Header>
              {suggestionMembers.map(item => (
                <MediaSearchContent item={item} key={item.id} />
              ))}
            </>
          )}
        </div>
        <div className="text-center mt-n3">
          <p
            className={classNames('fs-8 fw-bold text-center', {
              'd-none': resultItem.length > 0
            })}
          >
            {t('nav.search.noResults')}
          </p>
        </div>
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default SearchBox;
