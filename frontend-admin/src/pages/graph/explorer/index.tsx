import { useState, useCallback, useMemo, useEffect } from 'react';
import { Card, Button, ButtonGroup } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { CytoscapeViewer } from '../components/CytoscapeViewer';
import CypherEditor from '../components/CypherEditor';
import ResultsTable from '../components/ResultsTable';
import SchemaPanel from '../components/SchemaPanel';
import { JsonView, darkStyles } from 'react-json-view-lite';
import 'react-json-view-lite/dist/index.css';
import ResizeHandle from '../../../components/common/ResizeHandle';
import { useResizable } from '../../../hooks/ui/useResizable';
import {
  useExecuteQueryMutation,
  useLazyGetNodeNeighborsQuery,
  useDeleteNodeMutation,
  useDeleteRelationshipMutation
} from '../../../store/api/graphApi';
import {
  GraphContextMenu,
  type ContextMenuState
} from '../components/GraphContextMenu';
import type {
  QueryResult,
  GraphNode,
  GraphRelationship
} from '../../../types/graph';

type ViewMode = 'graph' | 'table' | 'split';

const SIDEBAR_STORAGE_KEY = 'orkestra:graph-explorer-sidebar-open';
const SPLIT_HEIGHT_STORAGE_KEY = 'orkestra:graph-explorer-split-height';

const GraphExplorer: React.FC = () => {
  const { t } = useTranslation();
  const [database, setDatabase] = useState<string>('');
  const [selectedDocumentUuid, setSelectedDocumentUuid] = useState<string>('');
  const [result, setResult] = useState<QueryResult | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>('split');
  const [readOnly, setReadOnly] = useState(true);
  const [lastSidebarQuery, setLastSidebarQuery] = useState('');
  const [sidebarOpen, setSidebarOpen] = useState(() => {
    const stored = localStorage.getItem(SIDEBAR_STORAGE_KEY);
    return stored === null ? true : stored === 'true';
  });
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);
  const [contextMenu, setContextMenu] = useState<ContextMenuState>({
    visible: false,
    x: 0,
    y: 0,
    type: 'node'
  });

  const [executeQuery, { isLoading }] = useExecuteQueryMutation();
  const [fetchNeighbors] = useLazyGetNodeNeighborsQuery();
  const [deleteNodeMutation] = useDeleteNodeMutation();
  const [deleteRelationshipMutation] = useDeleteRelationshipMutation();

  // Vertical resize between graph and table in split mode
  const {
    size: graphHeight,
    isDragging: isResizing,
    handleProps: resizeHandleProps
  } = useResizable({
    direction: 'vertical',
    initialSize: 500,
    minSize: 200,
    maxSize: 900,
    storageKey: SPLIT_HEIGHT_STORAGE_KEY
  });

  // Persist sidebar state
  useEffect(() => {
    localStorage.setItem(SIDEBAR_STORAGE_KEY, String(sidebarOpen));
  }, [sidebarOpen]);

  const handleExecute = useCallback(
    async (cypher: string, params?: Record<string, unknown>) => {
      try {
        const res = await executeQuery({
          cypher,
          params,
          database: database || undefined,
          readOnly
        }).unwrap();
        setResult(res);
      } catch {
        setResult(null);
      }
    },
    [executeQuery, database, readOnly]
  );

  const handleNodeClick = useCallback((node: GraphNode) => {
    setSelectedNode(node);
  }, []);

  const handleNodeDoubleClick = useCallback(
    async (node: GraphNode) => {
      try {
        const neighbors = await fetchNeighbors({
          nodeId: node.id,
          database: database || undefined,
          depth: 1,
          limit: 50
        }).unwrap();

        if (result?.graph && neighbors) {
          const existingNodeIds = new Set(result.graph.nodes.map(n => n.id));
          const existingRelIds = new Set(
            result.graph.relationships.map(r => r.id)
          );

          const newNodes = (neighbors.nodes ?? []).filter(
            n => !existingNodeIds.has(n.id)
          );
          const newRels = (neighbors.relationships ?? []).filter(
            r => !existingRelIds.has(r.id)
          );

          setResult({
            ...result,
            graph: {
              nodes: [...result.graph.nodes, ...newNodes],
              relationships: [...result.graph.relationships, ...newRels]
            }
          });
        }
      } catch {
        // Silently fail on neighbor expansion
      }
    },
    [fetchNeighbors, database, result]
  );

  const handleLabelClick = useCallback(
    (label: string) => {
      let cypher: string;
      let params: Record<string, unknown> | undefined;
      if (!selectedDocumentUuid) {
        cypher = `MATCH (n:\`${label}\`) OPTIONAL MATCH (n)-[r]-(m) RETURN n, r, m LIMIT 100`;
      } else if (label === 'RagDocument') {
        cypher =
          'MATCH (n:`RagDocument` {uuid: $docUuid}) OPTIONAL MATCH (n)-[r]-(m) RETURN n, r, m LIMIT 100';
        params = { docUuid: selectedDocumentUuid };
      } else {
        cypher = `MATCH (n:\`${label}\`) WHERE n.documentUuid = $docUuid OPTIONAL MATCH (n)-[r]-(m) RETURN n, r, m LIMIT 100`;
        params = { docUuid: selectedDocumentUuid };
      }
      setLastSidebarQuery(cypher);
      handleExecute(cypher, params);
    },
    [handleExecute, selectedDocumentUuid]
  );

  const handleRelTypeClick = useCallback(
    (type: string) => {
      let cypher: string;
      let params: Record<string, unknown> | undefined;
      if (!selectedDocumentUuid) {
        cypher = `MATCH (a)-[r:\`${type}\`]->(b) RETURN a, r, b LIMIT 100`;
      } else {
        cypher = `MATCH (a)-[r:\`${type}\`]->(b) WHERE a.documentUuid = $docUuid OR a.uuid = $docUuid RETURN a, r, b LIMIT 100`;
        params = { docUuid: selectedDocumentUuid };
      }
      setLastSidebarQuery(cypher);
      handleExecute(cypher, params);
    },
    [handleExecute, selectedDocumentUuid]
  );

  const handleDatabaseChange = useCallback((db: string) => {
    setDatabase(db);
    setResult(null);
  }, []);

  const handleDocumentChange = useCallback((docUuid: string) => {
    setSelectedDocumentUuid(docUuid);
    setResult(null);
  }, []);

  const handleNodeContextMenu = useCallback(
    (node: GraphNode, position: { x: number; y: number }) => {
      setContextMenu({
        visible: true,
        x: position.x,
        y: position.y,
        type: 'node',
        node
      });
    },
    []
  );

  const handleEdgeContextMenu = useCallback(
    (rel: GraphRelationship, position: { x: number; y: number }) => {
      setContextMenu({
        visible: true,
        x: position.x,
        y: position.y,
        type: 'edge',
        relationship: rel
      });
    },
    []
  );

  const handleCloseContextMenu = useCallback(() => {
    setContextMenu(prev => ({ ...prev, visible: false }));
  }, []);

  const handleDeleteNode = useCallback(
    async (node: GraphNode) => {
      const labels = node.labels.join(', ');
      if (
        !window.confirm(
          t('graph.explorer.deleteNodeConfirm', { id: node.id, labels })
        )
      )
        return;

      try {
        await deleteNodeMutation({
          nodeId: node.id,
          database: database || undefined
        }).unwrap();
        if (result?.graph) {
          setResult({
            ...result,
            graph: {
              nodes: result.graph.nodes.filter(n => n.id !== node.id),
              relationships: result.graph.relationships.filter(
                r => r.startNodeId !== node.id && r.endNodeId !== node.id
              )
            }
          });
        }
        if (selectedNode?.id === node.id) setSelectedNode(null);
      } catch {
        // Error handled by RTK Query
      }
    },
    [deleteNodeMutation, database, result, selectedNode, t]
  );

  const handleDeleteRelationship = useCallback(
    async (rel: GraphRelationship) => {
      if (
        !window.confirm(
          t('graph.explorer.deleteRelConfirm', { id: rel.id, type: rel.type })
        )
      )
        return;

      try {
        await deleteRelationshipMutation({
          relationshipId: rel.id,
          database: database || undefined
        }).unwrap();
        if (result?.graph) {
          setResult({
            ...result,
            graph: {
              nodes: result.graph.nodes,
              relationships: result.graph.relationships.filter(
                r => r.id !== rel.id
              )
            }
          });
        }
      } catch {
        // Error handled by RTK Query
      }
    },
    [deleteRelationshipMutation, database, result, t]
  );

  const graphNodes = useMemo(() => result?.graph?.nodes ?? [], [result]);
  const graphRels = useMemo(() => result?.graph?.relationships ?? [], [result]);
  const hasGraph = graphNodes.length > 0 || graphRels.length > 0;

  const showGraph = (viewMode === 'graph' || viewMode === 'split') && hasGraph;
  const showTable = viewMode === 'table' || viewMode === 'split';
  const isSplit = viewMode === 'split' && hasGraph;

  return (
    <>
      {/* Page Header */}
      <div className="d-flex align-items-center justify-content-between mb-3">
        <h5 className="mb-0">{t('graph.explorer.pageTitle')}</h5>
        <div className="d-flex gap-2 align-items-center">
          {result && (
            <ButtonGroup size="sm">
              <Button
                variant={viewMode === 'graph' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('graph')}
                disabled={!hasGraph}
              >
                {t('graph.explorer.view.graph')}
              </Button>
              <Button
                variant={viewMode === 'split' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('split')}
              >
                {t('graph.explorer.view.split')}
              </Button>
              <Button
                variant={viewMode === 'table' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('table')}
              >
                {t('graph.explorer.view.table')}
              </Button>
            </ButtonGroup>
          )}
        </div>
      </div>

      <div className="d-flex gap-3" style={{ minHeight: 'calc(100vh - 8rem)' }}>
        {/* Collapsible Schema Sidebar */}
        <div
          className={`graph-sidebar${isResizing ? ' resizing' : ''}`}
          style={{ width: sidebarOpen ? 280 : 40 }}
        >
          {sidebarOpen ? (
            <Card
              style={{
                height: '100%',
                maxHeight: 'calc(100vh - 8rem)',
                overflow: 'auto'
              }}
            >
              <Card.Header className="bg-body-tertiary py-2 d-flex align-items-center justify-content-between">
                <h6 className="mb-0">{t('graph.explorer.sidebar.title')}</h6>
                <Button
                  variant="link"
                  size="sm"
                  className="p-0 text-muted"
                  onClick={() => setSidebarOpen(false)}
                  title={t('graph.explorer.sidebar.collapse')}
                >
                  <i className="fas fa-chevron-left" />
                </Button>
              </Card.Header>
              <Card.Body className="p-0">
                <SchemaPanel
                  database={database}
                  selectedDocumentUuid={selectedDocumentUuid}
                  onDatabaseChange={handleDatabaseChange}
                  onDocumentChange={handleDocumentChange}
                  onLabelClick={handleLabelClick}
                  onRelTypeClick={handleRelTypeClick}
                />
              </Card.Body>
            </Card>
          ) : (
            <div
              className="graph-sidebar-toggle"
              onClick={() => setSidebarOpen(true)}
              title={t('graph.explorer.sidebar.expand')}
            >
              <span className="toggle-icon">
                <i className="fas fa-chevron-right" />
              </span>
            </div>
          )}
        </div>

        {/* Main Content */}
        <div
          style={{
            flex: 1,
            minWidth: 0,
            display: 'flex',
            flexDirection: 'column'
          }}
        >
          {/* Cypher Query Editor */}
          <CypherEditor
            onExecute={handleExecute}
            isLoading={isLoading}
            readOnly={readOnly}
            onReadOnlyChange={setReadOnly}
            defaultValue="MATCH (n) OPTIONAL MATCH (n)-[r]-(m) RETURN n, r, m LIMIT 100"
            externalQuery={lastSidebarQuery}
          />

          {/* Split view: graph + resize handle + table */}
          {isSplit ? (
            <div className="graph-panels mt-3" style={{ flex: 1 }}>
              {/* Graph panel - fixed height from resize */}
              <div
                className={`graph-panel${isResizing ? ' resizing' : ''}`}
                style={{ height: graphHeight, flexShrink: 0 }}
              >
                <CytoscapeViewer
                  nodes={graphNodes}
                  relationships={graphRels}
                  onNodeClick={handleNodeClick}
                  onNodeDoubleClick={handleNodeDoubleClick}
                  onNodeContextMenu={handleNodeContextMenu}
                  onEdgeContextMenu={handleEdgeContextMenu}
                  fillHeight
                  style={{ height: '100%' }}
                />
              </div>

              <ResizeHandle
                direction="vertical"
                isDragging={isResizing}
                onPointerDown={resizeHandleProps.onPointerDown}
              />

              {/* Selected Node Info */}
              {selectedNode && (
                <SelectedNodeCard
                  node={selectedNode}
                  onClose={() => setSelectedNode(null)}
                />
              )}

              {/* Table panel - takes remaining space */}
              <div style={{ flex: 1, minHeight: 150, overflow: 'auto' }}>
                <Card style={{ height: '100%' }}>
                  <Card.Header className="bg-body-tertiary py-2">
                    <h6 className="mb-0">
                      {t('graph.explorer.results.title')}
                      {result && (
                        <small className="text-muted fw-normal ms-2">
                          {t(
                            result.metadata.resultCount === 1
                              ? 'graph.explorer.results.rowsOne'
                              : 'graph.explorer.results.rowsOther',
                            {
                              count: result.metadata.resultCount,
                              ms: result.metadata.executionTimeMs
                            }
                          )}
                        </small>
                      )}
                    </h6>
                  </Card.Header>
                  <Card.Body className={result ? 'p-0' : 'text-center py-4'}>
                    {result ? (
                      <ResultsTable result={result} isLoading={isLoading} />
                    ) : isLoading ? (
                      <ResultsTable result={null} isLoading={true} />
                    ) : (
                      <p className="text-muted mb-0">
                        {t('graph.explorer.results.emptyHint')}
                      </p>
                    )}
                  </Card.Body>
                </Card>
              </div>
            </div>
          ) : (
            <>
              {/* Graph only view */}
              {showGraph && (
                <div className="mt-3">
                  <div
                    className={`graph-panel${isResizing ? ' resizing' : ''}`}
                    style={{ height: graphHeight, flexShrink: 0 }}
                  >
                    <CytoscapeViewer
                      nodes={graphNodes}
                      relationships={graphRels}
                      onNodeClick={handleNodeClick}
                      onNodeDoubleClick={handleNodeDoubleClick}
                      onNodeContextMenu={handleNodeContextMenu}
                      onEdgeContextMenu={handleEdgeContextMenu}
                      fillHeight
                      style={{ height: '100%' }}
                    />
                  </div>
                  <ResizeHandle
                    direction="vertical"
                    isDragging={isResizing}
                    onPointerDown={resizeHandleProps.onPointerDown}
                  />
                </div>
              )}

              {/* Selected Node Info */}
              {selectedNode && (
                <SelectedNodeCard
                  node={selectedNode}
                  onClose={() => setSelectedNode(null)}
                />
              )}

              {/* Table only view */}
              {showTable && (
                <Card className="mt-3">
                  <Card.Header className="bg-body-tertiary py-2">
                    <h6 className="mb-0">
                      {t('graph.explorer.results.title')}
                      {result && (
                        <small className="text-muted fw-normal ms-2">
                          {t(
                            result.metadata.resultCount === 1
                              ? 'graph.explorer.results.rowsOne'
                              : 'graph.explorer.results.rowsOther',
                            {
                              count: result.metadata.resultCount,
                              ms: result.metadata.executionTimeMs
                            }
                          )}
                        </small>
                      )}
                    </h6>
                  </Card.Header>
                  <Card.Body className={result ? 'p-0' : 'text-center py-4'}>
                    {result ? (
                      <ResultsTable result={result} isLoading={isLoading} />
                    ) : isLoading ? (
                      <ResultsTable result={null} isLoading={true} />
                    ) : (
                      <p className="text-muted mb-0">
                        {t('graph.explorer.results.emptyHint')}
                      </p>
                    )}
                  </Card.Body>
                </Card>
              )}
            </>
          )}
        </div>
      </div>

      <GraphContextMenu
        menu={contextMenu}
        onClose={handleCloseContextMenu}
        onExpandNeighbors={handleNodeDoubleClick}
        onDeleteNode={handleDeleteNode}
        onDeleteRelationship={handleDeleteRelationship}
      />
    </>
  );
};

/** Selected node info card with collapsible JSON tree and resizable height */
function SelectedNodeCard({
  node,
  onClose
}: {
  node: GraphNode;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const {
    size: cardHeight,
    isDragging,
    handleProps
  } = useResizable({
    direction: 'vertical',
    initialSize: 200,
    minSize: 80,
    maxSize: 600,
    storageKey: 'orkestra:graph-explorer-node-card-height'
  });

  return (
    <div className="mt-3" style={{ flexShrink: 0 }}>
      <Card className="border">
        <Card.Header className="py-2 px-3 bg-body-tertiary">
          <div className="d-flex align-items-center justify-content-between">
            <div>
              <small className="text-muted me-2">
                {t('graph.explorer.node.labelPrefix', { id: node.id })}
              </small>
              {(node.labels ?? []).map(l => (
                <span key={l} className="badge bg-primary me-1">
                  :{l}
                </span>
              ))}
            </div>
            <Button
              variant="link"
              size="sm"
              className="p-0 text-muted"
              onClick={onClose}
            >
              {t('graph.explorer.node.close')}
            </Button>
          </div>
        </Card.Header>
        <div
          style={{
            height: cardHeight,
            overflow: 'auto',
            padding: '0.5rem',
            fontSize: '0.875rem',
            fontFamily:
              "'JetBrains Mono', 'Fira Code', 'Source Code Pro', Consolas, monospace"
          }}
        >
          <JsonView
            data={node.properties}
            shouldExpandNode={(_level, _value, field) => field !== 'embedding'}
            style={darkStyles}
          />
        </div>
      </Card>
      <ResizeHandle
        direction="vertical"
        isDragging={isDragging}
        onPointerDown={handleProps.onPointerDown}
      />
    </div>
  );
}

export default GraphExplorer;
