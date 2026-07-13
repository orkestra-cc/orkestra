import { config } from 'config/environment';

// Deployment fingerprint. Two buckets, deliberately distinct:
//   A (curated)   — cloneVersion, from CHANGELOG.clone.md; changes at release.
//   B (automatic) — buildCommit (deploy) + startedAt (every restart).
// All values arrive via window.__ORKESTRA_CONFIG__ (runtime config.js), so
// the line reflects the running container, not what was compiled. Fields
// that aren't meaningfully populated on a fresh checkout are omitted so the
// line stays clean. Rendered small + monospace so it reads as a build stamp.
const Footer = () => {
  const { version, appName, cloneVersion, buildCommit, startedAt, env } =
    config;

  const parts = [
    appName || null,
    env || null,
    cloneVersion && cloneVersion !== 'dev' ? `clone ${cloneVersion}` : null,
    buildCommit ? `build ${buildCommit}` : null,
    startedAt || null
  ].filter(Boolean);

  const fingerprint = [`Orkestra v${version}`, ...parts].join(' · ');

  return (
    <footer className="footer">
      <p
        className="mb-3 mt-4 text-center text-truncate text-600 fs-11 font-monospace"
        title={fingerprint}
      >
        {fingerprint}
      </p>
    </footer>
  );
};

export default Footer;
