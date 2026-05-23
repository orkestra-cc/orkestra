import { useEffect, useRef, useCallback } from 'react';
import { ListGroup } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import type { GraphNode, GraphRelationship } from '../../../types/graph';

export interface ContextMenuState {
  visible: boolean;
  x: number;
  y: number;
  type: 'node' | 'edge';
  node?: GraphNode;
  relationship?: GraphRelationship;
}

interface GraphContextMenuProps {
  menu: ContextMenuState;
  onClose: () => void;
  onExpandNeighbors?: (node: GraphNode) => void;
  onDeleteNode?: (node: GraphNode) => void;
  onDeleteRelationship?: (rel: GraphRelationship) => void;
}

export function GraphContextMenu({
  menu,
  onClose,
  onExpandNeighbors,
  onDeleteNode,
  onDeleteRelationship
}: GraphContextMenuProps) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDivElement>(null);

  const handleClickOutside = useCallback(
    (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    },
    [onClose]
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    },
    [onClose]
  );

  useEffect(() => {
    if (!menu.visible) return;
    document.addEventListener('mousedown', handleClickOutside);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [menu.visible, handleClickOutside, handleKeyDown]);

  if (!menu.visible) return null;

  // Keep menu within viewport
  const menuWidth = 200;
  const menuHeight = menu.type === 'node' ? 100 : 50;
  const x =
    menu.x + menuWidth > window.innerWidth ? menu.x - menuWidth : menu.x;
  const y =
    menu.y + menuHeight > window.innerHeight ? menu.y - menuHeight : menu.y;

  return (
    <div
      ref={ref}
      style={{
        position: 'fixed',
        top: y,
        left: x,
        zIndex: 1050,
        minWidth: menuWidth
      }}
    >
      <ListGroup
        variant="flush"
        className="shadow-sm border rounded overflow-hidden"
        style={{ fontSize: '0.85rem' }}
      >
        {menu.type === 'node' && menu.node && (
          <>
            <ListGroup.Item
              action
              className="py-2 px-3"
              onClick={() => {
                onExpandNeighbors?.(menu.node!);
                onClose();
              }}
            >
              {t('graph.contextMenu.expandNeighbors')}
            </ListGroup.Item>
            <ListGroup.Item
              action
              className="py-2 px-3 text-danger"
              onClick={() => {
                onDeleteNode?.(menu.node!);
                onClose();
              }}
            >
              {t('graph.contextMenu.deleteNode')}
            </ListGroup.Item>
          </>
        )}
        {menu.type === 'edge' && menu.relationship && (
          <ListGroup.Item
            action
            className="py-2 px-3 text-danger"
            onClick={() => {
              onDeleteRelationship?.(menu.relationship!);
              onClose();
            }}
          >
            {t('graph.contextMenu.deleteRelationship')}
          </ListGroup.Item>
        )}
      </ListGroup>
    </div>
  );
}
