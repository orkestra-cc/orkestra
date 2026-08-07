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

/** A plain, non-group rail entry — e.g. Overview, Dependencies, Environments. */
export interface ModuleConfigRailItem {
  key: string;
  label: string;
}

export interface ModuleConfigRailProps {
  tree: GroupNode[];
  moduleName: string;
  activeKey: string;
  onSelect: (key: string) => void;
  statusFor: (node: GroupNode) => ModuleConfigRailStatus;
  /**
   * Non-config entries rendered before the tree (e.g. "Overview"). Omitted by
   * the config-card-only caller (`ModuleConfigSection`), which renders just
   * the tree with no captions — this keeps that usage byte-for-byte
   * unchanged. The full-page rail (`detail/index.tsx`) passes it.
   */
  leadingItems?: ModuleConfigRailItem[];
  /**
   * Caption shown directly above the tree. Only meaningful alongside
   * `leadingItems`/`trailingItems` — a caller that omits those has nothing
   * to caption the tree against, so this has no effect on its own.
   */
  treeCaption?: string;
  /** Caption shown above `trailingItems` (e.g. "Module"). */
  trailingCaption?: string;
  /** Non-config entries rendered after the tree (e.g. Dependencies, Environments). */
  trailingItems?: ModuleConfigRailItem[];
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
 *
 * `leadingItems`/`treeCaption`/`trailingCaption`/`trailingItems` extend the
 * same rail to span the whole module page (Task 4): Overview above the tree,
 * a "Configuration" caption over it, then a "Module" caption over
 * Dependencies/Environments. They render with the identical `Nav.Link`
 * markup as a tree node — same `role="button"`, same `aria-current` — so the
 * whole rail is one consistent, fully keyboard-reachable list.
 */
const ModuleConfigRail: React.FC<ModuleConfigRailProps> = ({
  tree,
  moduleName,
  activeKey,
  onSelect,
  statusFor,
  leadingItems,
  treeCaption,
  trailingCaption,
  trailingItems
}) => {
  const { t } = useTranslation();

  const renderItem = (item: ModuleConfigRailItem): React.ReactNode => {
    const isActive = item.key === activeKey;
    return (
      <Nav.Link
        key={item.key}
        active={isActive}
        aria-current={isActive ? 'true' : undefined}
        onClick={() => onSelect(item.key)}
      >
        {item.label}
      </Nav.Link>
    );
  };

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
    <Nav className="flex-column module-config-rail">
      {leadingItems?.map(renderItem)}
      {/* `text-700`, not the `text-500` these captions used to carry (2.62:1
          light / 3.22:1 dark) nor `text-600`, which still lands 4.25:1 on the
          dark card — under the 4.5 floor for 11.11px text. They now share the
          rail entries' ink and separate themselves by size, weight and case
          instead, which is the honest way round: de-emphasis by ink is what
          put this console's hierarchy backwards in the first place. */}
      {treeCaption && (
        <div className="fs-11 fw-semibold text-700 text-uppercase px-2 mt-3 mb-1">
          {treeCaption}
        </div>
      )}
      {renderNodes(tree)}
      {trailingCaption && (
        <div className="fs-11 fw-semibold text-700 text-uppercase px-2 mt-3 mb-1">
          {trailingCaption}
        </div>
      )}
      {trailingItems?.map(renderItem)}
    </Nav>
  );
};

export default ModuleConfigRail;
