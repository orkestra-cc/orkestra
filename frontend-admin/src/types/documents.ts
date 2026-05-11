// Document Template Types

export type TemplateType = 'invoice' | 'offer' | 'receipt' | 'custom';
export type PageSize = 'A4' | 'A3' | 'Letter' | 'Legal';
export type PageOrientation = 'portrait' | 'landscape';
export type SourceType = 'invoice' | 'offer' | 'custom';

// Page Margins
export interface PageMargins {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

// Template model
export interface Template {
  id: string;
  name: string;
  description?: string;
  type: TemplateType;
  htmlContent: string;
  cssContent?: string;
  pageSize: PageSize;
  orientation: PageOrientation;
  margins: PageMargins;
  headerHtml?: string;
  footerHtml?: string;
  isDefault: boolean;
  isBuiltIn: boolean;
  isActive: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
  createdBy?: string;
  updatedBy?: string;
}

// Template list item (lighter version)
export interface TemplateListItem {
  id: string;
  name: string;
  description?: string;
  type: TemplateType;
  pageSize: PageSize;
  orientation: PageOrientation;
  isDefault: boolean;
  isBuiltIn: boolean;
  isActive: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
}

// Generated document metadata
export interface GeneratedDocumentMeta {
  id: string;
  sourceType: SourceType;
  sourceUuid: string;
  templateUuid: string;
  fileName: string;
  fileSize: number;
  contentType: string;
  generatedAt: string;
  generatedBy: string;
  expiresAt?: string;
  createdAt: string;
}

// DTOs for API requests

export interface CreateTemplateInput {
  name: string;
  description?: string;
  type: TemplateType;
  htmlContent: string;
  cssContent?: string;
  pageSize?: PageSize;
  orientation?: PageOrientation;
  margins?: PageMargins;
  headerHtml?: string;
  footerHtml?: string;
}

export interface UpdateTemplateInput {
  name?: string;
  description?: string;
  htmlContent?: string;
  cssContent?: string;
  pageSize?: PageSize;
  orientation?: PageOrientation;
  margins?: PageMargins;
  headerHtml?: string;
  footerHtml?: string;
  isActive?: boolean;
}

export interface GeneratePDFInput {
  templateUuid: string;
  data: Record<string, unknown>;
  fileName?: string;
  sourceType?: SourceType;
  sourceUuid?: string;
}

export interface PreviewHTMLInput {
  templateUuid: string;
  data: Record<string, unknown>;
}

export interface PreviewHTMLFromContentInput {
  htmlContent: string;
  cssContent?: string;
  data: Record<string, unknown>;
}

export interface DuplicateTemplateInput {
  name: string;
}

// API Response types

export interface TemplateListResponse {
  templates: TemplateListItem[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface TemplateResponse {
  template: Template;
}

export interface GeneratePDFResponse {
  document: GeneratedDocumentMeta;
}

export interface PreviewHTMLResponse {
  html: string;
}

export interface ServiceStatusResponse {
  available: boolean;
  message: string;
}

// Template variable types
export interface TemplateVariableInfo {
  name: string;
  description: string;
  type: string;
  required: boolean;
}

export interface TemplateVariableGroup {
  name: string;
  description: string;
  variables: TemplateVariableInfo[];
}

export interface TemplateVariablesResponse {
  templateType: TemplateType;
  groups: TemplateVariableGroup[];
}

// Query params

export interface TemplateListParams {
  page?: number;
  pageSize?: number;
  type?: TemplateType;
  isDefault?: boolean;
  isBuiltIn?: boolean;
  isActive?: boolean;
  search?: string;
}

// Constants for UI

export const TEMPLATE_TYPE_LABELS: Record<TemplateType, string> = {
  invoice: 'Fattura',
  offer: 'Preventivo',
  receipt: 'Ricevuta',
  custom: 'Personalizzato',
};

export const PAGE_SIZE_LABELS: Record<PageSize, string> = {
  A4: 'A4',
  A3: 'A3',
  Letter: 'Letter',
  Legal: 'Legal',
};

export const PAGE_ORIENTATION_LABELS: Record<PageOrientation, string> = {
  portrait: 'Verticale',
  landscape: 'Orizzontale',
};

export const TEMPLATE_TYPE_COLORS: Record<TemplateType, string> = {
  invoice: 'primary',
  offer: 'success',
  receipt: 'info',
  custom: 'secondary',
};

// Default values

export const DEFAULT_MARGINS: PageMargins = {
  top: 20,
  bottom: 20,
  left: 20,
  right: 20,
};

export const DEFAULT_PAGE_SIZE: PageSize = 'A4';
export const DEFAULT_ORIENTATION: PageOrientation = 'portrait';

// Utility functions

export const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return '0 Bytes';
  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
};

export const formatDate = (dateString: string): string => {
  const date = new Date(dateString);
  return date.toLocaleDateString('it-IT', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
  });
};

export const formatDateTime = (dateString: string): string => {
  const date = new Date(dateString);
  return date.toLocaleDateString('it-IT', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};
