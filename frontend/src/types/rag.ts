// --- Document Types ---

export interface RagDocument {
  uuid: string;
  title: string;
  fileName: string;
  fileSize: number;
  isoStandard?: string;
  version?: string;
  docType: string;
  status: 'pending' | 'processing' | 'completed' | 'failed';
  error?: string;
  chunkCount: number;
  modelUuid: string;
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
  sectionTitle?: string;
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
  sectionTitle?: string;
  score: number;
  position: number;
}

export interface QueryMeta {
  embeddingTimeMs: number;
  searchTimeMs: number;
  llmTimeMs: number;
  totalTimeMs: number;
  chunksRetrieved: number;
  modelUsed: string;
}
