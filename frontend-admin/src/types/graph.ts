// --- Core Graph Types ---

export interface GraphNode {
  id: number;
  elementId: string;
  labels: string[];
  properties: Record<string, unknown>;
}

export interface GraphRelationship {
  id: number;
  elementId: string;
  type: string;
  startNodeId: number;
  endNodeId: number;
  startElementId: string;
  endElementId: string;
  properties: Record<string, unknown>;
}

export interface GraphData {
  nodes: GraphNode[];
  relationships: GraphRelationship[];
}

export interface QueryMetadata {
  executionTimeMs: number;
  resultCount: number;
  nodesCreated?: number;
  nodesDeleted?: number;
  relationshipsCreated?: number;
  relationshipsDeleted?: number;
  propertiesSet?: number;
  labelsAdded?: number;
  labelsRemoved?: number;
  containsUpdates?: boolean;
}

export interface QueryResult {
  columns: string[];
  rows: Record<string, unknown>[];
  graph?: GraphData;
  metadata: QueryMetadata;
}

// --- Database Types ---

export interface DatabaseInfo {
  name: string;
  address?: string;
  currentStatus: string;
  default: boolean;
  home: boolean;
}

// --- Schema Types ---

export interface LabelInfo {
  name: string;
  count: number;
  properties: string[];
}

export interface RelTypeInfo {
  name: string;
  count: number;
  properties: string[];
}

export interface IndexInfo {
  name: string;
  type: string;
  entityType: string;
  labels: string[];
  properties: string[];
  state: string;
}

export interface ConstraintInfo {
  name: string;
  type: string;
  entityType: string;
  labels: string[];
  properties: string[];
}

export interface SchemaInfo {
  labels: LabelInfo[];
  relationshipTypes: RelTypeInfo[];
  indexes: IndexInfo[];
  constraints: ConstraintInfo[];
  nodeCount: number;
  relationshipCount: number;
}

// --- Algorithm Types (MAGE) ---

export interface AlgorithmInfo {
  name: string;
  category: string;
  procedure: string;
  description: string;
}

export interface AlgorithmRequest {
  algorithm: string;
  config?: Record<string, unknown>;
  database?: string;
}

// --- Vector Types ---

export interface VectorIndex {
  name: string;
  label: string;
  property: string;
  dimensions: number;
  similarity: string;
  state: string;
}

export interface VectorSearchRequest {
  indexName: string;
  queryVector: number[];
  topK?: number;
  minScore?: number;
  database?: string;
}

export interface CreateVectorIndexRequest {
  name: string;
  label: string;
  property: string;
  dimensions: number;
  capacity?: number;
  similarity: 'cos' | 'l2sq' | 'ip';
  database?: string;
}

// --- Request Types ---

export interface ExecuteQueryRequest {
  cypher: string;
  params?: Record<string, unknown>;
  database?: string;
  readOnly?: boolean;
}

export interface BrowseParams {
  database?: string;
  limit?: number;
  skip?: number;
}

export interface BrowseNodesParams extends BrowseParams {
  labels?: string[];
}

export interface BrowseRelationshipsParams extends BrowseParams {
  types?: string[];
}

export interface NodeNeighborsParams {
  nodeId: number;
  database?: string;
  depth?: number;
  limit?: number;
}

// --- Delete Response Types ---

export interface DeleteNodeResponse {
  message: string;
  nodesDeleted: number;
  relationshipsDeleted: number;
}

export interface DeleteRelationshipResponse {
  message: string;
  relationshipsDeleted: number;
}
