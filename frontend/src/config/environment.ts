/**
 * Environment configuration for the frontend application.
 *
 * This module provides type-safe access to environment variables and
 * utility functions to check the current environment.
 */

export type Environment = 'development' | 'staging' | 'production';

interface EnvironmentConfig {
  /** Current environment name */
  env: Environment;
  /** Backend API URL */
  apiUrl: string;
  /** WebSocket URL */
  wsUrl: string;
  /** Debug mode enabled */
  debug: boolean;
  /** True if running in production */
  isProduction: boolean;
  /** True if running in staging */
  isStaging: boolean;
  /** True if running in development */
  isDevelopment: boolean;
  /** True for staging and production (production-like behavior) */
  isProductionLike: boolean;
}

function getEnvironment(): Environment {
  const env = import.meta.env.VITE_ENV;
  if (env === 'staging' || env === 'production') {
    return env;
  }
  return 'development';
}

function createConfig(): EnvironmentConfig {
  const env = getEnvironment();

  return {
    env,
    apiUrl: import.meta.env.VITE_API_URL || import.meta.env.VITE_BACKEND_URL || 'http://localhost:3000',
    wsUrl: import.meta.env.VITE_WS_URL || 'ws://localhost:3000/ws',
    debug: import.meta.env.VITE_DEBUG === 'true',
    isProduction: env === 'production',
    isStaging: env === 'staging',
    isDevelopment: env === 'development',
    isProductionLike: env === 'production' || env === 'staging',
  };
}

/** Environment configuration singleton */
export const config = createConfig();

/**
 * Check if running in development environment.
 */
export function isDevelopment(): boolean {
  return config.isDevelopment;
}

/**
 * Check if running in production environment.
 */
export function isProduction(): boolean {
  return config.isProduction;
}

/**
 * Check if running in staging environment.
 */
export function isStaging(): boolean {
  return config.isStaging;
}

/**
 * Check if running in a production-like environment (staging or production).
 * Use this for security and behavior that should match production.
 */
export function isProductionLike(): boolean {
  return config.isProductionLike;
}

/**
 * Get the current environment name.
 */
export function getEnv(): Environment {
  return config.env;
}

export default config;
