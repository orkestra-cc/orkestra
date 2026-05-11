/// <reference types="vite/client" />

declare module 'data/*';
declare module 'react-slick';
declare module 'd3';
declare module 'react-leaflet-markercluster';
declare module 'leaflet.tilelayer.colorfilter';

// Extend leaflet types
declare module 'leaflet' {
  namespace tileLayer {
    function colorFilter(
      url: string,
      options?: {
        attribution?: string | null;
        transparent?: boolean;
        filter?: string[];
      }
    ): TileLayer;
  }
}

interface ImportMetaEnv {
  readonly VITE_BACKEND_URL: string;
  readonly VITE_API_URL: string;
  readonly VITE_WS_URL: string;
  readonly VITE_PUBLIC_URL: string;
  readonly VITE_ENV: 'development' | 'staging' | 'production';
  readonly VITE_DEBUG: string;
  readonly VITE_REACT_APP_TINYMCE_APIKEY: string;
  readonly VITE_REACT_APP_GOOGLE_API_KEY: string;
  readonly VITE_APP_PORT: string;
  readonly VITE_APP_HOST: string;
  readonly VITE_GOOGLE_CLIENT_ID?: string;
  readonly VITE_APPLE_CLIENT_ID?: string;
  readonly VITE_SENTRY_DSN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}