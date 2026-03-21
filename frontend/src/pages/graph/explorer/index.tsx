import { useState, useCallback, useMemo } from 'react';
import { Row, Col, Card, Button, ButtonGroup } from 'react-bootstrap';
import { CytoscapeViewer } from '../components/CytoscapeViewer';
import CypherEditor from '../components/CypherEditor';
import ResultsTable from '../components/ResultsTable';
import SchemaPanel from '../components/SchemaPanel';
import { useExecuteQueryMutation, useLazyGetNodeNeighborsQuery } from '../../../store/api/graphApi';
import type { QueryResult, GraphNode } from '../../../types/graph';

type ViewMode = 'graph' | 'table' | 'split';

const GraphExplorer: React.FC = () => {
  const [database, setDatabase] = useState<string>('');
  const [result, setResult] = useState<QueryResult | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>('split');
  const [readOnly, setReadOnly] = useState(true);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [selectedNode, setSelectedNode] = useState<GraphNode | null>(null);

  const [executeQuery, { isLoading }] = useExecuteQueryMutation();
  const [fetchNeighbors] = useLazyGetNodeNeighborsQuery();

  const handleExecute = useCallback(async (cypher: string, params?: Record<string, unknown>) => {
    try {
      const res = await executeQuery({
        cypher,
        params,
        database: database || undefined,
        readOnly,
      }).unwrap();
      setResult(res);
    } catch {
      setResult(null);
    }
  }, [executeQuery, database, readOnly]);

  const handleNodeClick = useCallback((node: GraphNode) => {
    setSelectedNode(node);
  }, []);

  const handleNodeDoubleClick = useCallback(async (node: GraphNode) => {
    try {
      const neighbors = await fetchNeighbors({
        nodeId: node.id,
        database: database || undefined,
        depth: 1,
        limit: 50,
      }).unwrap();

      if (result?.graph && neighbors) {
        const existingNodeIds = new Set(result.graph.nodes.map(n => n.id));
        const existingRelIds = new Set(result.graph.relationships.map(r => r.id));

        const newNodes = (neighbors.nodes ?? []).filter(n => !existingNodeIds.has(n.id));
        const newRels = (neighbors.relationships ?? []).filter(r => !existingRelIds.has(r.id));

        setResult({
          ...result,
          graph: {
            nodes: [...result.graph.nodes, ...newNodes],
            relationships: [...result.graph.relationships, ...newRels],
          },
        });
      }
    } catch {
      // Silently fail on neighbor expansion
    }
  }, [fetchNeighbors, database, result]);

  const handleLabelClick = useCallback((label: string) => {
    handleExecute(`MATCH (n:\`${label}\`) OPTIONAL MATCH (n)-[r]-(m) RETURN n, r, m LIMIT 100`);
  }, [handleExecute]);

  const handleRelTypeClick = useCallback((type: string) => {
    handleExecute(`MATCH (a)-[r:\`${type}\`]->(b) RETURN a, r, b LIMIT 100`);
  }, [handleExecute]);

  const handleDatabaseChange = useCallback((db: string) => {
    setDatabase(db);
    setResult(null);
  }, []);

  const graphNodes = useMemo(() => result?.graph?.nodes ?? [], [result]);
  const graphRels = useMemo(() => result?.graph?.relationships ?? [], [result]);
  const hasGraph = graphNodes.length > 0 || graphRels.length > 0;

  return (
    <>
      {/* Page Header */}
      <div className="d-flex align-items-center justify-content-between mb-3">
        <h5 className="mb-0">Graph Explorer</h5>
        <div className="d-flex gap-2 align-items-center">
          <Button
            variant={sidebarOpen ? 'falcon-primary' : 'falcon-default'}
            size="sm"
            onClick={() => setSidebarOpen(!sidebarOpen)}
          >
            {sidebarOpen ? 'Hide' : 'Show'} Schema
          </Button>
          {result && (
            <ButtonGroup size="sm">
              <Button
                variant={viewMode === 'graph' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('graph')}
                disabled={!hasGraph}
              >
                Graph
              </Button>
              <Button
                variant={viewMode === 'split' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('split')}
              >
                Split
              </Button>
              <Button
                variant={viewMode === 'table' ? 'primary' : 'outline-primary'}
                onClick={() => setViewMode('table')}
              >
                Table
              </Button>
            </ButtonGroup>
          )}
        </div>
      </div>

      <Row className="g-3">
        {/* Schema Sidebar */}
        {sidebarOpen && (
          <Col xl={3} lg={4}>
            <Card className="sticky-top" style={{ top: '1rem', maxHeight: 'calc(100vh - 6rem)', overflow: 'auto' }}>
              <Card.Header className="bg-body-tertiary py-2">
                <h6 className="mb-0">Database Schema</h6>
              </Card.Header>
              <Card.Body className="p-0">
                <SchemaPanel
                  database={database}
                  onDatabaseChange={handleDatabaseChange}
                  onLabelClick={handleLabelClick}
                  onRelTypeClick={handleRelTypeClick}
                />
              </Card.Body>
            </Card>
          </Col>
        )}

        {/* Main Content */}
        <Col xl={sidebarOpen ? 9 : 12} lg={sidebarOpen ? 8 : 12}>
          {/* Cypher Query Editor */}
          <CypherEditor
            onExecute={handleExecute}
            isLoading={isLoading}
            readOnly={readOnly}
            onReadOnlyChange={setReadOnly}
            defaultValue="MATCH (n) RETURN n LIMIT 25"
          />

          {/* Graph Visualization */}
          {(viewMode === 'graph' || viewMode === 'split') && hasGraph && (
            <div className="mt-3">
              <CytoscapeViewer
                nodes={graphNodes}
                relationships={graphRels}
                onNodeClick={handleNodeClick}
                onNodeDoubleClick={handleNodeDoubleClick}
              />
            </div>
          )}

          {/* Selected Node Info */}
          {selectedNode && (
            <Card className="mt-3 border">
              <Card.Body className="py-2 px-3">
                <div className="d-flex align-items-center justify-content-between">
                  <div>
                    <small className="text-muted me-2">Node {selectedNode.id}</small>
                    {(selectedNode.labels ?? []).map(l => (
                      <span key={l} className="badge bg-primary me-1">:{l}</span>
                    ))}
                  </div>
                  <Button variant="link" size="sm" className="p-0 text-muted" onClick={() => setSelectedNode(null)}>
                    Close
                  </Button>
                </div>
                <pre className="mb-0 mt-1 bg-body-tertiary rounded p-2" style={{ fontSize: '0.75rem', maxHeight: 120, overflow: 'auto' }}>
                  {JSON.stringify(selectedNode.properties, null, 2)}
                </pre>
              </Card.Body>
            </Card>
          )}

          {/* Results Table */}
          {(viewMode === 'table' || viewMode === 'split') && (
            <Card className="mt-3">
              <Card.Header className="bg-body-tertiary py-2">
                <h6 className="mb-0">
                  Results
                  {result && (
                    <small className="text-muted fw-normal ms-2">
                      {result.metadata.resultCount} row{result.metadata.resultCount !== 1 ? 's' : ''}
                      {' '}in {result.metadata.executionTimeMs}ms
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
                  <p className="text-muted mb-0">Run a query to see results here. Click a label in the schema to browse nodes.</p>
                )}
              </Card.Body>
            </Card>
          )}
        </Col>
      </Row>
    </>
  );
};

export default GraphExplorer;
