import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge, Button, Card, Col, Form, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import cytoscape from 'cytoscape';
import type { Core, EventObject, StylesheetStyle } from 'cytoscape';
// @ts-expect-error cytoscape-fcose ships without type declarations
import * as fcoseModule from 'cytoscape-fcose';
import type { GraphNode, GraphRelationship } from '../../../types/graph';

// Register fcose layout (guarded against HMR double-registration)
try {
  const fcose = fcoseModule.default ?? fcoseModule;
  if (typeof fcose === 'function') cytoscape.use(fcose);
} catch {
  // Already registered
}

// --- Color palette for node labels ---

const NODE_COLORS = [
  '#4e79a7',
  '#f28e2b',
  '#e15759',
  '#76b7b2',
  '#59a14f',
  '#edc948',
  '#b07aa1',
  '#ff9da7',
  '#9c755f',
  '#bab0ac'
];

// --- Edge line styles per relationship type ---

const EDGE_LINE_STYLES: Array<'solid' | 'dashed' | 'dotted'> = [
  'solid',
  'dashed',
  'dotted'
];

type LayoutName =
  | 'fcose'
  | 'cose'
  | 'circle'
  | 'grid'
  | 'breadthfirst'
  | 'concentric';

interface CytoscapeViewerProps {
  nodes: GraphNode[];
  relationships: GraphRelationship[];
  onNodeClick?: (node: GraphNode) => void;
  onNodeDoubleClick?: (node: GraphNode) => void;
  onEdgeClick?: (rel: GraphRelationship) => void;
  onNodeContextMenu?: (
    node: GraphNode,
    position: { x: number; y: number }
  ) => void;
  onEdgeContextMenu?: (
    rel: GraphRelationship,
    position: { x: number; y: number }
  ) => void;
  layout?: LayoutName;
  className?: string;
  style?: React.CSSProperties;
  /** When set, the canvas fills its container height instead of using minHeight */
  fillHeight?: boolean;
}

function buildLabelColorMap(nodes: GraphNode[]): Map<string, string> {
  const map = new Map<string, string>();
  let colorIndex = 0;
  for (const node of nodes) {
    for (const label of node.labels) {
      if (!map.has(label)) {
        map.set(label, NODE_COLORS[colorIndex % NODE_COLORS.length]);
        colorIndex++;
      }
    }
  }
  return map;
}

function buildRelTypeStyleMap(
  relationships: GraphRelationship[]
): Map<string, 'solid' | 'dashed' | 'dotted'> {
  const map = new Map<string, 'solid' | 'dashed' | 'dotted'>();
  let styleIndex = 0;
  for (const rel of relationships) {
    if (!map.has(rel.type)) {
      map.set(rel.type, EDGE_LINE_STYLES[styleIndex % EDGE_LINE_STYLES.length]);
      styleIndex++;
    }
  }
  return map;
}

function getNodeDisplayLabel(node: GraphNode): string {
  const primary = node.labels[0] ?? 'Node';
  const props = node.properties;
  const detail = (props.name as string) ?? (props.title as string) ?? undefined;
  return detail ? `${primary}\n${detail}` : primary;
}

function buildCytoscapeElements(
  nodes: GraphNode[],
  relationships: GraphRelationship[],
  labelColorMap: Map<string, string>
) {
  const cyNodes = nodes.map(node => ({
    group: 'nodes' as const,
    data: {
      id: String(node.id),
      label: getNodeDisplayLabel(node),
      color: labelColorMap.get(node.labels[0] ?? '') ?? NODE_COLORS[0],
      _graphNode: node
    }
  }));

  const cyEdges = relationships.map(rel => ({
    group: 'edges' as const,
    data: {
      id: `e${rel.id}`,
      source: String(rel.startNodeId),
      target: String(rel.endNodeId),
      label: rel.type,
      _graphRel: rel
    }
  }));

  return [...cyNodes, ...cyEdges];
}

function getLayoutOptions(name: LayoutName) {
  const base = { name, animate: false, fit: true, padding: 40 };

  switch (name) {
    case 'fcose':
      return {
        ...base,
        animate: 'end' as const,
        animationDuration: 400,
        quality: 'default' as const,
        randomize: true,
        nodeDimensionsIncludeLabels: true,
        nodeSeparation: 120,
        idealEdgeLength: () => 120,
        nodeRepulsion: () => 6000,
        edgeElasticity: () => 0.45,
        gravity: 0.2,
        numIter: 2500,
        packComponents: true
      };
    case 'cose':
      return {
        ...base,
        animate: 'end' as const,
        animationDuration: 400,
        nodeDimensionsIncludeLabels: true,
        nodeRepulsion: () => 8000,
        idealEdgeLength: () => 150,
        edgeElasticity: () => 0.45,
        gravity: 0.1,
        numIter: 1000,
        randomize: true,
        componentSpacing: 100,
        nestingFactor: 1.2
      };
    case 'breadthfirst':
      return { ...base, directed: true, spacingFactor: 1.75 };
    case 'concentric':
      return {
        ...base,
        minNodeSpacing: 80,
        concentric: (node: { degree: () => number }) => node.degree(),
        levelWidth: () => 2
      };
    case 'grid':
      return { ...base, spacingFactor: 1.5 };
    case 'circle':
      return { ...base, spacingFactor: 1.5 };
    default:
      return base;
  }
}

const CYTOSCAPE_STYLE: StylesheetStyle[] = [
  {
    selector: 'node',
    style: {
      'background-color': 'data(color)',
      label: 'data(label)',
      color: '#e6edf3',
      'font-size': '12px',
      'font-weight': 'bold',
      'text-valign': 'bottom',
      'text-halign': 'center',
      'text-margin-y': 8,
      'text-wrap': 'wrap',
      'text-max-width': '140px',
      'text-outline-color': '#0b1727',
      'text-outline-width': 2,
      'text-outline-opacity': 0.9,
      width: 40,
      height: 40,
      'border-width': 2,
      'border-color': '#30363d',
      'overlay-padding': 4
    }
  },
  {
    selector: 'node:selected',
    style: {
      'border-width': 3,
      'border-color': '#58a6ff',
      'overlay-color': '#58a6ff',
      'overlay-opacity': 0.15
    }
  },
  {
    selector: 'edge',
    style: {
      width: 2,
      'line-color': '#8b949e',
      'target-arrow-color': '#8b949e',
      'target-arrow-shape': 'triangle',
      'curve-style': 'bezier',
      label: 'data(label)',
      'font-size': '10px',
      color: '#c9d1d9',
      'text-outline-color': '#0b1727',
      'text-outline-width': 2,
      'text-outline-opacity': 0.9,
      'text-rotation': 'autorotate',
      'text-margin-y': -10
    }
  },
  {
    selector: 'edge:selected',
    style: {
      'line-color': '#58a6ff',
      'target-arrow-color': '#58a6ff',
      width: 3
    }
  }
];

export function CytoscapeViewer({
  nodes,
  relationships,
  onNodeClick,
  onNodeDoubleClick,
  onEdgeClick,
  onNodeContextMenu,
  onEdgeContextMenu,
  layout: layoutProp = 'fcose',
  className,
  style,
  fillHeight
}: CytoscapeViewerProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const cyRef = useRef<Core | null>(null);
  const [activeLayout, setActiveLayout] = useState<LayoutName>(layoutProp);
  const contextMenuCleanupRef = useRef<(() => void) | null>(null);

  // Callback ref: suppress the browser context menu the instant the DOM node exists.
  // A callback ref fires synchronously when React attaches the node — no effect
  // timing gaps, no missed events.
  const setContainerRef = useCallback((node: HTMLDivElement | null) => {
    if (contextMenuCleanupRef.current) {
      contextMenuCleanupRef.current();
      contextMenuCleanupRef.current = null;
    }
    containerRef.current = node;
    if (node) {
      const handler = (e: Event) => {
        e.preventDefault();
        e.stopImmediatePropagation();
      };
      node.addEventListener('contextmenu', handler, true);
      contextMenuCleanupRef.current = () =>
        node.removeEventListener('contextmenu', handler, true);
    }
  }, []);

  const labelColorMap = useMemo(() => buildLabelColorMap(nodes), [nodes]);
  const relTypeStyleMap = useMemo(
    () => buildRelTypeStyleMap(relationships),
    [relationships]
  );

  // Store callbacks in refs to avoid effect re-runs
  const onNodeClickRef = useRef(onNodeClick);
  const onNodeDoubleClickRef = useRef(onNodeDoubleClick);
  const onEdgeClickRef = useRef(onEdgeClick);
  const onNodeContextMenuRef = useRef(onNodeContextMenu);
  const onEdgeContextMenuRef = useRef(onEdgeContextMenu);
  onNodeClickRef.current = onNodeClick;
  onNodeDoubleClickRef.current = onNodeDoubleClick;
  onEdgeClickRef.current = onEdgeClick;
  onNodeContextMenuRef.current = onNodeContextMenu;
  onEdgeContextMenuRef.current = onEdgeContextMenu;

  // Single effect: build cytoscape, add data, run layout, cleanup
  useEffect(() => {
    if (!containerRef.current) return;
    if (nodes.length === 0 && relationships.length === 0) return;

    const elements = buildCytoscapeElements(
      nodes,
      relationships,
      labelColorMap
    );

    const edgeStyleOverrides: StylesheetStyle[] = Array.from(
      relTypeStyleMap.entries()
    ).map(([type, lineStyle]) => ({
      selector: `edge[label="${type}"]`,
      style: { 'line-style': lineStyle }
    }));

    // Create instance with elements but NO layout yet (preset = keep positions as-is)
    const cy = cytoscape({
      container: containerRef.current,
      elements,
      style: [...CYTOSCAPE_STYLE, ...edgeStyleOverrides],
      layout: { name: 'preset' },
      boxSelectionEnabled: true,
      selectionType: 'single',
      minZoom: 0.05,
      maxZoom: 10,
      wheelSensitivity: 1
    });

    cyRef.current = cy;

    // Resize observer: re-render cytoscape when container dimensions change
    let resizeRaf = 0;
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(resizeRaf);
      resizeRaf = requestAnimationFrame(() => {
        cy.resize();
        cy.fit(undefined, 40);
      });
    });
    if (containerRef.current) ro.observe(containerRef.current);

    // Also suppress contextmenu directly on every canvas Cytoscape created
    const canvases = containerRef.current.querySelectorAll('canvas');
    const canvasHandler = (e: Event) => {
      e.preventDefault();
      e.stopImmediatePropagation();
    };
    canvases.forEach(c =>
      c.addEventListener('contextmenu', canvasHandler, true)
    );

    cy.on('tap', 'node', (evt: EventObject) => {
      const gn = evt.target.data('_graphNode') as GraphNode | undefined;
      if (gn && onNodeClickRef.current) onNodeClickRef.current(gn);
    });
    cy.on('dbltap', 'node', (evt: EventObject) => {
      const gn = evt.target.data('_graphNode') as GraphNode | undefined;
      if (gn && onNodeDoubleClickRef.current) onNodeDoubleClickRef.current(gn);
    });
    cy.on('tap', 'edge', (evt: EventObject) => {
      const gr = evt.target.data('_graphRel') as GraphRelationship | undefined;
      if (gr && onEdgeClickRef.current) onEdgeClickRef.current(gr);
    });

    cy.on('cxttap', 'node', (evt: EventObject) => {
      const gn = evt.target.data('_graphNode') as GraphNode | undefined;
      if (gn && onNodeContextMenuRef.current) {
        const me = evt.originalEvent as MouseEvent;
        me.preventDefault();
        onNodeContextMenuRef.current(gn, { x: me.clientX, y: me.clientY });
      }
    });

    cy.on('cxttap', 'edge', (evt: EventObject) => {
      const gr = evt.target.data('_graphRel') as GraphRelationship | undefined;
      if (gr && onEdgeContextMenuRef.current) {
        const me = evt.originalEvent as MouseEvent;
        me.preventDefault();
        onEdgeContextMenuRef.current(gr, { x: me.clientX, y: me.clientY });
      }
    });

    // Pick layout: force-directed needs edges, fall back to circle if none
    let cancelled = false;
    const layoutTimer = requestAnimationFrame(() => {
      if (cancelled) return;
      const isForceDirected =
        activeLayout === 'fcose' || activeLayout === 'cose';
      const effectiveLayout =
        isForceDirected && relationships.length === 0
          ? ('circle' as LayoutName)
          : activeLayout;
      const lo = cy.layout(getLayoutOptions(effectiveLayout));
      lo.run();
    });

    return () => {
      cancelled = true;
      cancelAnimationFrame(layoutTimer);
      cancelAnimationFrame(resizeRaf);
      ro.disconnect();
      canvases.forEach(c =>
        c.removeEventListener('contextmenu', canvasHandler, true)
      );
      cy.destroy();
      cyRef.current = null;
    };
  }, [nodes, relationships, activeLayout]);

  const handleFit = useCallback(() => {
    cyRef.current?.fit(undefined, 40);
  }, []);

  const handleExportPng = useCallback(() => {
    const cy = cyRef.current;
    if (!cy) return;

    const png = cy.png({ output: 'blob', full: true, scale: 2, bg: '#0b1727' });
    const url = URL.createObjectURL(png);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'graph.png';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }, []);

  const handleLayoutChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setActiveLayout(e.target.value as LayoutName);
    },
    []
  );

  // Sync when the parent passes a different layout prop
  useEffect(() => {
    setActiveLayout(layoutProp);
  }, [layoutProp]);

  return (
    <Card
      className={className}
      style={{
        ...(fillHeight
          ? { display: 'flex', flexDirection: 'column', height: '100%' }
          : {}),
        ...style
      }}
    >
      {/* Toolbar */}
      <Card.Header className="py-2">
        <Row className="align-items-center g-2">
          <Col xs="auto">
            <Form.Select
              size="sm"
              value={activeLayout}
              onChange={handleLayoutChange}
              aria-label={t('graph.viewer.layoutAriaLabel')}
              style={{ width: 160 }}
            >
              <option value="fcose">{t('graph.viewer.layouts.fcose')}</option>
              <option value="cose">{t('graph.viewer.layouts.cose')}</option>
              <option value="circle">{t('graph.viewer.layouts.circle')}</option>
              <option value="grid">{t('graph.viewer.layouts.grid')}</option>
              <option value="breadthfirst">
                {t('graph.viewer.layouts.breadthfirst')}
              </option>
              <option value="concentric">
                {t('graph.viewer.layouts.concentric')}
              </option>
            </Form.Select>
          </Col>
          <Col xs="auto">
            <Button variant="outline-secondary" size="sm" onClick={handleFit}>
              {t('graph.viewer.fit')}
            </Button>
          </Col>
          <Col xs="auto">
            <Button
              variant="outline-secondary"
              size="sm"
              onClick={handleExportPng}
            >
              {t('graph.viewer.exportPng')}
            </Button>
          </Col>
          <Col xs="auto" className="ms-auto d-flex gap-2">
            <Badge bg="primary" pill>
              {t(
                nodes.length === 1
                  ? 'graph.viewer.nodesOne'
                  : 'graph.viewer.nodesOther',
                { count: nodes.length }
              )}
            </Badge>
            <Badge bg="secondary" pill>
              {t(
                relationships.length === 1
                  ? 'graph.viewer.edgesOne'
                  : 'graph.viewer.edgesOther',
                { count: relationships.length }
              )}
            </Badge>
          </Col>
        </Row>
      </Card.Header>

      {/* Graph canvas */}
      <Card.Body
        className="p-0 position-relative"
        style={fillHeight ? { flex: 1, minHeight: 0 } : undefined}
      >
        <div
          ref={setContainerRef}
          style={{
            width: '100%',
            height: '100%',
            ...(fillHeight ? {} : { minHeight: 700 })
          }}
        />

        {/* Color legend */}
        {labelColorMap.size > 0 && (
          <div
            className="position-absolute rounded shadow-sm p-2"
            style={{
              bottom: 12,
              left: 12,
              maxWidth: 220,
              fontSize: '0.75rem',
              zIndex: 10,
              backgroundColor: 'rgba(11, 23, 39, 0.85)',
              border: '1px solid #30363d',
              color: '#c9d1d9'
            }}
          >
            <div className="fw-semibold mb-1" style={{ color: '#e6edf3' }}>
              {t('graph.viewer.legendTitle')}
            </div>
            {Array.from(labelColorMap.entries()).map(([label, color]) => (
              <div key={label} className="d-flex align-items-center gap-1 mb-1">
                <span
                  style={{
                    display: 'inline-block',
                    width: 12,
                    height: 12,
                    borderRadius: '50%',
                    backgroundColor: color,
                    flexShrink: 0
                  }}
                />
                <span className="text-truncate">{label}</span>
              </div>
            ))}
          </div>
        )}
      </Card.Body>
    </Card>
  );
}
