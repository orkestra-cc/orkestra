// Example type definitions for a feature module.
//
// To use: copy this file to `src/types/<name>.ts` and replace `Widget`
// with your domain entity. Keep request/response shapes aligned with
// the backend's Huma OpenAPI spec at `/openapi.json` — Huma generates
// JSON schemas from your Go structs, so the source of truth is the
// backend handler signature.

export interface Widget {
  uuid: string;
  name: string;
  description: string;
  status: 'active' | 'archived';
  createdAt: string;
  updatedAt: string;
}

export interface WidgetListParams {
  page?: number;
  limit?: number;
  status?: Widget['status'];
  search?: string;
}

export interface WidgetListResponse {
  widgets: Widget[];
  total: number;
  page: number;
  limit: number;
}

export interface CreateWidgetInput {
  name: string;
  description?: string;
}

export interface UpdateWidgetInput {
  name?: string;
  description?: string;
  status?: Widget['status'];
}
