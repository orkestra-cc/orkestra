import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';
import { useEffect, useRef, useState } from 'react';
import { Collapse } from 'react-bootstrap';

interface TreeviewItem {
  id: string;
  name: string;
  icon?: IconProp;
  iconClass?: string;
  children?: TreeviewItem[];
  expanded?: boolean;
  dot?: string;
  badge?: number;
}

interface TreeviewListItemProps {
  item: TreeviewItem;
  openedItems: string[];
  setOpenedItems: React.Dispatch<React.SetStateAction<string[]>>;
  selectedItems: string[];
  setSelectedItems: React.Dispatch<React.SetStateAction<string[]>>;
  selection?: boolean;
}

const TreeviewListItem = ({
  item,
  openedItems,
  setOpenedItems,
  selectedItems,
  setSelectedItems,
  selection
}: TreeviewListItemProps) => {
  const [open, setOpen] = useState(openedItems.indexOf(item.id) !== -1);
  const [children, setChildren] = useState<string[]>([]);
  const [firstChildren, setFirstChildren] = useState<string[]>([]);
  const [childrenOpen, setChildrenOpen] = useState(false);
  const checkRef = useRef<HTMLInputElement>(null);

  const getChildrens = (treeItem: TreeviewItem): string[] => {
    function flatInner(items: TreeviewItem[]): string[] {
      let flat: string[] = [];
      items.forEach((child) => {
        if (child.children) {
          flat = [...flat, child.id, ...flatInner(child.children)];
        } else {
          flat = [...flat, child.id];
        }
      });

      return flat;
    }
    if (treeItem.children) {
      return flatInner(treeItem.children);
    } else {
      return [];
    }
  };

  const isChildrenOpen = () => {
    return openedItems.some((openedItem) => firstChildren.indexOf(openedItem) !== -1);
  };

  const handleOnExiting = () => {
    setOpenedItems(openedItems.filter((openedItem) => openedItem !== item.id));
  };
  const handleEntering = () => {
    setOpenedItems([...openedItems, item.id]);
  };

  const handleSingleCheckboxChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.checked) {
      setSelectedItems([...selectedItems, item.id]);
    } else {
      setSelectedItems(
        selectedItems.filter((selectedItem) => selectedItem !== item.id)
      );
    }
  };

  const handleParentCheckboxChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const filteredItems = selectedItems.filter(
      (selectedItem) => children.indexOf(selectedItem) === -1
    );
    if (e.target.checked) {
      setSelectedItems([...filteredItems, ...children]);
    } else {
      setSelectedItems(filteredItems);
    }
  };

  useEffect(() => {
    setChildren(getChildrens(item));
    if (item.children) {
      setFirstChildren(item.children.map((child) => child.id));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    setChildrenOpen(isChildrenOpen());
  }, [children, openedItems]);

  useEffect(() => {
    const childrenSelected = selectedItems.some(
      selectedItem => children.indexOf(selectedItem) !== -1
    );
    const allChildrenSelected = children.every(
      child => selectedItems.indexOf(child) !== -1
    );
    if (childrenSelected && checkRef.current) {
      checkRef.current.indeterminate = true;
    }
    if (!childrenSelected && checkRef.current) {
      checkRef.current.indeterminate = false;
    }
    if (allChildrenSelected && checkRef.current) {
      checkRef.current.indeterminate = false;
      checkRef.current.checked = true;
    }
    if (!allChildrenSelected && checkRef.current) {
      checkRef.current.checked = false;
    }
  }, [selectedItems, checkRef.current]);

  return (
    <li className="treeview-list-item">
      {Object.prototype.hasOwnProperty.call(item, 'children') ? (
        <>
          <div className="toggle-container">
            {selection && (
              <input
                type="checkbox"
                className="form-check-input"
                onChange={handleParentCheckboxChange}
                ref={checkRef}
              />
            )}
            <a
              className={classNames('collapse-toggle', {
                collapsed: open || item.expanded
              })}
              href="#!"
              onClick={() => setOpen(!open)}
            >
              <p
                className={classNames('treeview-text', { 'ms-2': !selection })}
              >
                {item.name}
              </p>
            </a>
          </div>
          <Collapse
            in={open}
            onExiting={handleOnExiting}
            onEntering={handleEntering}
          >
            <ul
              className={classNames('treeview-list', {
                'collapse-hidden': !open,
                'collapse-show treeview-border': open,
                'treeview-border-transparent': childrenOpen
              })}
            >
              {item.children?.map((nestedItem: TreeviewItem, index: number) => (
                <TreeviewListItem
                  key={index}
                  item={nestedItem}
                  openedItems={openedItems}
                  setOpenedItems={setOpenedItems}
                  selectedItems={selectedItems}
                  setSelectedItems={setSelectedItems}
                  selection={selection}
                />
              ))}
            </ul>
          </Collapse>
        </>
      ) : (
        <div className="treeview-item">
          {selection && (
            <input
              type="checkbox"
              className="form-check-input"
              onChange={handleSingleCheckboxChange}
              checked={selectedItems.indexOf(item.id) !== -1}
            />
          )}
          <a href="#!" className="flex-1">
            <p className="treeview-text">
              {item.icon && (
                <FontAwesomeIcon
                  icon={item.icon}
                  className={classNames('me-2', item.iconClass)}
                />
              )}
              {item.name}
            </p>
          </a>
        </div>
      )}
    </li>
  );
};

interface TreeviewProps {
  data: TreeviewItem[];
  selection?: boolean;
  expanded?: string[];
  selectedItems?: string[];
  setSelectedItems?: React.Dispatch<React.SetStateAction<string[]>>;
  defaultSelected?: string[];
}

const Treeview = ({
  data,
  selection,
  expanded = [],
  selectedItems = [],
  setSelectedItems = () => {}
}: TreeviewProps) => {
  const [openedItems, setOpenedItems] = useState<string[]>(expanded);

  return (
    <ul className="treeview treeview-select">
      {data.map((treeviewItem: TreeviewItem, index: number) => (
        <TreeviewListItem
          key={index}
          item={treeviewItem}
          openedItems={openedItems}
          setOpenedItems={setOpenedItems}
          selectedItems={selectedItems}
          setSelectedItems={setSelectedItems}
          selection={selection}
        />
      ))}
    </ul>
  );
};

export default Treeview;
export type { TreeviewItem, TreeviewProps };
