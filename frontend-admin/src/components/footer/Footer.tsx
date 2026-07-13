import { Col, Row } from 'react-bootstrap';
import { config } from 'config/environment';

// Deployment fingerprint. Two buckets, deliberately distinct:
//   A (curated)   — cloneVersion, from CHANGELOG.clone.md; changes at release.
//   B (automatic) — buildCommit (deploy) + startedAt (every restart).
// All values arrive via window.__ORKESTRA_CONFIG__ (runtime config.js), so
// the line reflects the running container, not what was compiled. Fields
// that aren't meaningfully populated on a fresh checkout are omitted so the
// line stays clean.
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
      <Row className="justify-content-between text-center fs-10 mt-4 mb-3">
        <Col sm="auto">
          <p className="mb-0 text-600">
            Thank you for creating with us{' '}
            <span className="d-none d-sm-inline-block">| </span>
            <br className="d-sm-none" /> {new Date().getFullYear()} &copy;{' '}
            <a href="https://orkestra.cc">orkestra.cc</a>
          </p>
        </Col>
        <Col sm="auto">
          <p className="mb-0 text-600 text-truncate mw-100" title={fingerprint}>
            {fingerprint}
          </p>
        </Col>
      </Row>
    </footer>
  );
};

export default Footer;
