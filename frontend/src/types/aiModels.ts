export type Provider = 'ollama' | 'openai' | 'anthropic' | 'gemini';
export type ModelType = 'embedding' | 'llm';
export type ProviderCategory = 'local' | 'cloud';

export interface AIModelConfig {
  uuid: string;
  name: string;
  provider: Provider;
  providerCategory: ProviderCategory;
  modelType: ModelType;
  modelName: string;
  baseUrl?: string;
  dimensions?: number;
  temperature?: number;
  maxTokens?: number;
  isDefault: boolean;
  isActive: boolean;
  lastTestedAt?: string;
  lastTestStatus?: string; // "ok" | "error" | ""
  createdAt: string;
  updatedAt: string;
}

export interface CreateAIModelRequest {
  name: string;
  provider: Provider;
  modelType: ModelType;
  modelName: string;
  baseUrl?: string;
  apiKey?: string;
  dimensions?: number;
  temperature?: number;
  maxTokens?: number;
}

export interface UpdateAIModelRequest {
  name?: string;
  baseUrl?: string;
  apiKey?: string;
  dimensions?: number;
  temperature?: number;
  maxTokens?: number;
  isActive?: boolean;
}

export interface TestModelResult {
  status: 'ok' | 'error';
  message: string;
}

export interface QuickPromptResult {
  response: string;
  timeMs: number;
}

export interface AvailableModel {
  id: string;
  ownedBy?: string;
  capabilities?: string;
}

// Provider metadata for UI rendering
export const PROVIDER_INFO: Record<Provider, { label: string; category: ProviderCategory; color: string }> = {
  ollama: { label: 'Ollama', category: 'local', color: 'info' },
  openai: { label: 'OpenAI / Compatible', category: 'cloud', color: 'success' },
  anthropic: { label: 'Anthropic (Claude)', category: 'cloud', color: 'warning' },
  gemini: { label: 'Google (Gemini)', category: 'cloud', color: 'primary' },
};
