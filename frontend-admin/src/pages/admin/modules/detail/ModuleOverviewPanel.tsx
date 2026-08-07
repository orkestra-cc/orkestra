import { Col, Row } from 'react-bootstrap';
import {
  faHeartPulse,
  faGear,
  faSitemap,
  faClock
} from '@fortawesome/free-solid-svg-icons';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import StatCard from 'components/common/StatCard';
import type { BadgeColor } from 'components/common/SubtleBadge';
import type { ModuleConfig, ModuleHealthStatus } from 'store/api/moduleApi';
import { configCompleteness } from '../configModel';

interface ModuleOverviewPanelProps {
  module: ModuleConfig;
  health?: ModuleHealthStatus;
  allModules?: ModuleConfig[];
}

/**
 * This string is the *headline value* of a StatCard, so it was the largest
 * untranslated text on the page \u2014 "11h ago" at 27.65px inside an otherwise
 * Italian console. Routed through `t()` like everything else on screen.
 */
const formatRelativeTime = (t: TFunction, dateStr: string): string => {
  if (!dateStr) return '\u2014';
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return t('adminModules.detail.relative.justNow');
  if (minutes < 60)
    return t('adminModules.detail.relative.minutes', { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24)
    return t('adminModules.detail.relative.hours', { count: hours });
  const days = Math.floor(hours / 24);
  return t('adminModules.detail.relative.days', { count: days });
};

/**
 * The module detail page's KPI row, built on the console's `StatCard` tile
 * rather than on per-page cards.
 *
 * It used to hand-roll four `<Card>`s with their own type ramp — an `fs-10`
 * caption over an `fs-8` (19.2px) value over an `fs-11` note — which made this
 * the only KPI row in the console rendering its headline value at 19.2px,
 * while every other summary row (`admin/tenants`, `admin/compliance`, and the
 * dashboards a fork's addons add) renders at `h3` (27.65px) through
 * `StatCard`, with the 4px status border and the faded 3x icon that make the
 * tile recognisable. Same data, same icons, same colors — the difference was
 * purely that this page reimplemented the tile instead of importing it.
 *
 * `color` is the tile's status channel (it paints the 4px accent border), so
 * each card derives it from its own metric rather than picking a decorative
 * hue. The `badge` corner ribbon stays unused here: per DESIGN.md it is
 * reserved for real attention states, and "some dependencies are down" is
 * already carried by the border plus the `n/m` value.
 */
const ModuleOverviewPanel: React.FC<ModuleOverviewPanelProps> = ({
  module: mod,
  health,
  allModules
}) => {
  const { t, i18n } = useTranslation();
  const healthStatus = health?.status || (mod.enabled ? 'healthy' : 'disabled');
  const healthColor = ({
    healthy: 'success',
    unhealthy: 'danger',
    disabled: 'secondary',
    failed: 'danger'
  }[healthStatus] || 'secondary') as BadgeColor;

  const { filled, total } = configCompleteness(
    mod.configSchema,
    mod.configValues,
    mod.secretStatus
  );
  const configColor: BadgeColor =
    total === 0 ? 'secondary' : filled === total ? 'success' : 'warning';

  const depCount = mod.dependsOn?.length || 0;
  const depsHealthy =
    mod.dependsOn?.filter(dep => {
      const depMod = allModules?.find(m => m.moduleName === dep);
      return depMod && depMod.status === 'running';
    }).length || 0;
  const depColor: BadgeColor =
    depCount === 0
      ? 'secondary'
      : depsHealthy === depCount
        ? 'success'
        : 'warning';

  return (
    <Row className="g-3 mb-3">
      <Col md={6} xl={3}>
        <StatCard
          title={t('adminModules.detail.cards.health')}
          value={healthStatus}
          icon={faHeartPulse}
          color={healthColor}
          subtitle={
            health?.error ? (
              <span className="text-danger" title={health.error}>
                {health.error.length > 40
                  ? health.error.slice(0, 40) + '...'
                  : health.error}
              </span>
            ) : undefined
          }
        />
      </Col>

      <Col md={6} xl={3}>
        <StatCard
          title={t('adminModules.detail.cards.configuration')}
          value={total > 0 ? `${filled}/${total}` : '\u2014'}
          icon={faGear}
          color={configColor}
          subtitle={
            <>
              {total > 0
                ? t('adminModules.detail.cards.requiredFieldsSet')
                : t('adminModules.detail.cards.noRequiredFields')}
              {/* Spans, not divs: `StatCard` renders `subtitle` inside a
                  `<small>`, which is phrasing content. The progress classes
                  supply their own `display`, so the elements lay out
                  identically either way. */}
              {total > 0 && (
                <span
                  className="progress stat-card-progress mt-2"
                  aria-hidden="true"
                >
                  <span
                    className={`progress-bar bg-${configColor}`}
                    style={{ width: `${(filled / total) * 100}%` }}
                  />
                </span>
              )}
            </>
          }
        />
      </Col>

      <Col md={6} xl={3}>
        <StatCard
          title={t('adminModules.detail.cards.dependenciesCount')}
          value={depCount > 0 ? `${depsHealthy}/${depCount}` : '\u2014'}
          icon={faSitemap}
          color={depColor}
          subtitle={
            depCount > 0
              ? t('adminModules.detail.cards.dependenciesRunning')
              : t('adminModules.detail.cards.noDependencies')
          }
        />
      </Col>

      <Col md={6} xl={3}>
        <StatCard
          title={t('adminModules.detail.cards.lastModified')}
          value={formatRelativeTime(t, mod.updatedAt)}
          icon={faClock}
          color="info"
          subtitle={
            mod.updatedAt
              ? // `i18n.language`, not a pinned 'en-GB': the month abbreviation
                // is user-visible text and belongs in the operator's locale.
                new Date(mod.updatedAt).toLocaleDateString(i18n.language, {
                  day: '2-digit',
                  month: 'short',
                  year: 'numeric'
                })
              : undefined
          }
        />
      </Col>
    </Row>
  );
};

export default ModuleOverviewPanel;
