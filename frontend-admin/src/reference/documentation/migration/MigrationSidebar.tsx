import { useState } from 'react';
import { Card, Collapse } from 'react-bootstrap';
import { useLocation } from 'react-router';
import classNames from 'classnames';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

const navItems = [
  {
    id: '1',
    name: 'Migration',
    expanded: true,
    children: [
      {
        id: '5.0.0',
        to: 'v5.0.0',
        name: 'v5.0.0',
        children: [
          {
            id: 33,
            to: 'v5-0-package-json-update',
            name: 'Update Package.json'
          },
          {
            id: 34,
            to: 'vv5-0-react-router',
            name: 'Update React Router'
          },
          {
            id: 35,
            to: 'v5-0-suspense-boundary',
            name: 'Update Suspense'
          },
          {
            id: 36,
            to: 'v5-0-codemods',
            name: 'Update using codemods'
          },
          {
            id: 37,
            to: 'v5-0-forward-ref',
            name: 'Remove forwardRef'
          },
          {
            id: 38,
            to: 'v5-0-useContext',
            name: 'Update use() API'
          },
          {
            id: 39,
            to: 'v5-0-provider',
            name: 'Update Provider'
          },
          {
            id: 40,
            to: 'v5-0-rm-prop-type',
            name: 'Remove Prop Types'
          },
          {
            id: 41,
            to: 'v5-0-rm-node',
            name: 'Remove Node Modules'
          },
          {
            id: 42,
            to: 'v5-0-run-project',
            name: 'Run the Project'
          }
        ]
      },
      {
        id: '4.8.0',
        to: 'v4.8.0',
        name: 'v4.8.0',
        expanded: false,
        children: [
          { id: '2', to: 'pre-requisites', name: 'Pre-requisites' },
          { id: '3', to: 'remove-dependencies', name: 'Remove Dependencies' },
          { id: '4', to: 'remove-webpack', name: 'Remove Webpack' },
          { id: '5', to: 'install-vite', name: 'Install Vite' },
          { id: '6', to: 'edit-scripts', name: 'Edit Package.json' },
          { id: '7', to: 'update-env', name: 'Update .env file' },
          { id: '8', to: 'configure-vite-config', name: 'configure Vite Config' },
          { id: '9', to: 'add-compile-scss', name: 'Add Compile SCSS' },
          {
            id: '10',
            to: 'renaming-js-files',
            name: 'Renaming JS Files',
            expanded: true,
            children: [
              { id: '11', to: 'add-rename-script', name: 'Add Rename Script' },
              {
                id: '12',
                to: 'configure-rename-paths',
                name: 'Configure Rename Paths'
              },
              { id: '13', to: 'run-rename-script', name: 'Run Rename Script' },
              {
                id: '14',
                to: 'reinstall-dependencies',
                name: 'Reinstall Dependencies'
              },
              { id: '15', to: 'important-notes', name: 'Important Notes' }
            ]
          },
          { id: '16', to: 'update-index-html', name: 'Update Index.html' },
          { id: '17', to: 'edit-useToggleStyle', name: 'Edit useToggleStyle' },
          {
            id: '18',
            to: 'update-environment',
            name: 'Update Environment Variables'
          },
          {
            id: '19',
            to: 'update-bootstrap-import',
            name: 'Update Bootstrap Imports'
          },
          { id: '20', to: 'update-css-imports', name: 'Update CSS Imports' },
          { id: '21', to: 'run-application', name: 'Run The Application' }
        ]
      }
    ]
  }
];

const MigrationSidebar = () => {
  return (
    <div className="sticky-sidebar migration-sidebar">
      <Card className="sticky-top font-sans-serif">
        <Card.Header className="border-bottom">
          <h6 className="mb-0 fs-9">On this page</h6>
        </Card.Header>
        <Card.Body>
          <div className="scrollbar p-2" style={{ maxHeight: '70vh' }}>
            <ul id="migrationTreeView" className="mb-0 treeview">
              {navItems.map(item =>
                item.children && item.children.length > 0 ? (
                  <MigrationCollapse key={item.id} item={item} />
                ) : (
                  <MigrationNavItem key={item.id} item={item} />
                )
              )}
            </ul>
          </div>
        </Card.Body>
      </Card>
    </div>
  );
};

const MigrationCollapse = ({ item }: { item: any }) => {
  const [open, setOpen] = useState(item.expanded ?? true);

  const hasVisibleChild = (item: any): boolean => {
    if (!item.children) return false;
    return item.children.some((child: any) => {
      if (child.children && child.children.length > 0) {
        return hasVisibleChild(child);
      }
      return true;
    });
  };

  const showBorder = open && hasVisibleChild(item);

  return (
    <li className="treeview-list-item mb-2">
      <div className="toggle-container">
        <a
          className={classNames('collapse-toggle text-nowrap', {
            collapsed: open
          })}
          href="#!"
          onClick={() => setOpen((prev: boolean) => !prev)}
        >
          <p className="treeview-text">{item.name}</p>
        </a>
      </div>
      <Collapse in={open}>
        <ul
          className={classNames('treeview-list', {
            'collapse-hidden': !open,
            'collapse-show': open,
            'treeview-border': showBorder,
            'treeview-border-transparent': !showBorder
          })}
        >
          {item?.children?.map((subItem: any) =>
            subItem.children && subItem.children.length > 0 ? (
              <MigrationCollapse key={subItem.id} item={subItem} />
            ) : (
              <MigrationNavItem key={subItem.id} item={subItem} />
            )
          )}
        </ul>
      </Collapse>
    </li>
  );
};

const MigrationNavItem = ({ item }: { item: any }) => {
  const { hash } = useLocation();
  return (
    <li className="treeview-list-item mb-2">
      <a
        className={classNames('treeview-item flex-1', {
          active: hash === `#${item.to}`
        })}
        href={`#${item.to}`}
      >
        <p className="treeview-text fw-medium text-nowrap">
          <FontAwesomeIcon icon="hashtag" className="fa-w-14 me-2 fs-11" />
          {item.name}
        </p>
      </a>
    </li>
  );
};

export default MigrationSidebar;
