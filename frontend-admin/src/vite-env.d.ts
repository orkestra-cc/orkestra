/// <reference types="vite/client" />

declare module 'data/*';
declare module 'react-slick';
declare module 'd3';
declare module 'react-leaflet-markercluster';
declare module 'leaflet.tilelayer.colorfilter';

// Minimal leaflet typing — @types/leaflet isn't installed (it's only a
// devDependency of react-leaflet, so it doesn't propagate), and we only
// touch a tiny surface of the API directly via `L.tileLayer(...)`.
// The colorFilter mixins below are for leaflet.tilelayer.colorfilter v2.x —
// the plugin no longer exposes an `L.tileLayer.colorFilter()` factory
// (that was v1); it adds a `colorFilter` TileLayerOption and an
// `updateColorFilter` method to TileLayer.
declare module 'leaflet' {
  interface TileLayerOptions {
    attribution?: string;
    colorFilter?: string[];
  }
  interface TileLayer {
    addTo(map: unknown): this;
    updateColorFilter(filter: string[]): this;
  }
  function tileLayer(
    urlTemplate: string,
    options?: TileLayerOptions
  ): TileLayer;
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

// Injected by vite.config.js `define`. Derived from the git tag at
// build time so the footer always matches the released artefact.
declare const __APP_VERSION__: string;
