import { Fragment } from 'react';
import { Nav } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { SubtleBadge } from 'components/common';
import type { GroupNode } from '../configModel';
import { translateConfigGroup } from 'helpers/configLabel';

export interface ModuleConfigRailStatus {
  /** Count of that node's visible required fields still empty. */
  unfilled: number;
}

export interface ModuleConfigRailProps {
  tree: GroupNode[];
  moduleName: string;
  activeKey: string;
  onSelect: (key: string) => void;
  statusFor: (node: GroupNode) => ModuleConfigRailStatus;
}

/**
 * Vertical settings rail, one entry per group node, nested children indented
 * one DOM level under their parent so depth compounds naturally.
 *
 * Deliberately no `role="tablist"` here: `@restart/ui` only wires the
 * ArrowLeft/ArrowRight roving-tabIndex handler (and the `aria-controls` /
 * `role="tabpanel"` pairing) inside a `<Tab.Container>`. A bare `<Nav>` with
 * `role="tablist"` buys `role="tab"` + `aria-selected` and nothing else,
 * while making every inactive entry unreachable by keyboard. Each entry stays
 * a plain `role="button"` link (the `<Nav.Link>` default for an anchor
 * without `href`), reachable by sequential Tab.
 */
const ModuleConfigRail: React.FC<ModuleConfigRailProps> = ({
  tree,
  moduleName,
  activeKey,
  onSelect,
  statusFor
}) => {
  const { t } = useTranslation();

  const renderNodes = (nodes: GroupNode[]): React.ReactNode =>
    nodes.map(node => {
      const isActive = node.key === activeKey;
      const { unfilled } = statusFor(node);
      return (
        <Fragment key={node.key}>
          <Nav.Link
            active={isActive}
            aria-current={isActive ? 'true' : undefined}
            className="d-flex align-items-center justify-content-between gap-2"
            onClick={() => onSelect(node.key)}
          >
            <span className="text-truncate">
              {translateConfigGroup(t, moduleName, node)}
            </span>
            {unfilled > 0 && (
              <SubtleBadge bg="warning" pill className="fs-11 flex-shrink-0">
                {unfilled}
              </SubtleBadge>
            )}
          </Nav.Link>
          {node.children.length > 0 && (
            <div className="ms-3">{renderNodes(node.children)}</div>
          )}
        </Fragment>
      );
    });

  return (
    <Nav className="flex-column module-config-rail">{renderNodes(tree)}</Nav>
  );
};

export default ModuleConfigRail;
