// --- Document Types ---

export interface RagDocument {
  uuid: string;
  title: string;
  fileName: string;
  fileSize: number;
  isoStandard?: string;
  version?: string;
  documentCategory?: string;
  docType: string;
  status: 'pending' | 'processing' | 'completed' | 'failed';
  error?: string;
  chunkCount: number;
  modelUuid: string;
  llmModelName?: string;
  chunkSize: number;
  chunkOverlap: number;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
}

// --- Chunk Types ---

export interface RagChunk {
  uuid: string;
  documentUuid: string;
  text: string;
  position: number;
  fullPath?: string;
  nodeType?: string;
  numbering?: string;
  requirementLevel?: string;
  depth?: number;
}

// --- Section Types ---

export interface RagSection {
  uuid: string;
  documentUuid: string;
  nodeType: string;
  numbering?: string;
  title?: string;
  depth: number;
  fullPath?: string;
  position: number;
}

// --- Update Types ---

export interface UpdateDocumentRequest {
  title?: string;
  isoStandard?: string;
  version?: string;
}

// --- Query Types ---

export interface RagQueryRequest {
  question: string;
  topK?: number;
  minScore?: number;
  isoStandard?: string;
  modelUuid?: string;
  requirementLevel?: string;
  nodeType?: string;
  retrievalMode?: string;
}

export interface RagQueryResponse {
  answer: string;
  sources: SourceRef[];
  metadata: QueryMeta;
}

export interface SourceRef {
  documentUuid: string;
  documentTitle: string;
  isoStandard?: string;
  chunkUuid: string;
  chunkText: string;
  fullPath?: string;
  nodeType?: string;
  requirementLevel?: string;
  score: number;
  position: number;
}

// --- Relationship Type Config ---

export interface RelationshipTypeConfig {
  uuid: string;
  name: string;
  description: string;
  fromNode: string;
  toNode: string;
  properties: string[] | null;
  categories: Record<string, boolean>;
  isSystem: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface CreateRelationshipTypeRequest {
  name: string;
  description?: string;
  fromNode: string;
  toNode: string;
  properties?: string[];
  categories: Record<string, boolean>;
}

export interface UpdateRelationshipTypeRequest {
  description?: string;
  properties?: string[];
  categories?: Record<string, boolean>;
}

export interface QueryMeta {
  embeddingTimeMs: number;
  searchTimeMs: number;
  llmTimeMs: number;
  totalTimeMs: number;
  chunksRetrieved: number;
  modelUsed: string;
}

// --- Cross-Document Relations ---

export interface CrossDocLink {
  sourceChunkUuid: string;
  sourceFullPath: string;
  sourceText: string;
  targetChunkUuid: string;
  targetFullPath: string;
  targetText: string;
  targetDocUuid: string;
  targetDocTitle: string;
  similarity: number;
}

export interface RelatedDocSummary {
  documentUuid: string;
  documentTitle: string;
  isoStandard?: string;
  linkCount: number;
  avgSimilarity: number;
  maxSimilarity: number;
}

export interface DocumentRelationsResponse {
  relatedDocuments: RelatedDocSummary[];
  links: CrossDocLink[];
  totalLinks: number;
}
