import { useMemo, useState } from 'react';
import { Col, Form, Row, Spinner } from 'react-bootstrap';
import {
  faChartColumn,
  faCoins,
  faGaugeHigh,
  faHourglassHalf,
  faPaperPlane
} from '@fortawesome/free-solid-svg-icons';
import type { CellContext, ColumnDef } from '@tanstack/react-table';
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { useTranslation } from 'react-i18next';
import EmptyState from 'components/common/EmptyState';
import ReactEchart from 'components/common/ReactEchart';
import StatCard from 'components/common/StatCard';
import type { BadgeColor } from 'components/common/SubtleBadge';
import SearchablePagedTable from 'components/common/advance-table/SearchablePagedTable';
import { useAppContext } from 'providers/AppProvider';
import {
  llmMonthConsumed,
  useGetLlmUsageQuery,
  useListLlmModelsQuery,
  type LlmUsageGroupBy,
  type LlmUsageRow
} from 'store/api/llmApi';
import LlmLoadError from './LlmLoadError';

echarts.use([
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

const PERIODS = ['30d', '90d', 'month'] as const;
type Period = (typeof PERIODS)[number];
const GROUPS: LlmUsageGroupBy[] = ['model', 'user', 'caller'];
const DAY_MS = 24 * 60 * 60 * 1000;

// fromOf is the start of the report window. Floored to the minute so a
// remount within the same minute reuses the cached report; `to` is left to
// the backend (now).
const fromOf = (period: Period, now = new Date()): string => {
  if (period === 'month') {
    return new Date(
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1)
    ).toISOString();
  }
  const days = period === '30d' ? 30 : 90;
  const start = new Date(now.getTime() - days * DAY_MS);
  start.setUTCSeconds(0, 0);
  return start.toISOString();
};

interface LabelledRow extends LlmUsageRow {
  label: string;
}

// UsageTab is the org's LLM consumption (llm.usage.read): the current UTC
// month's rollup against the budget, then the ledger grouped by model, user
// or caller over a chosen window. Consumed and reserved are shown apart:
// the budget admits a call by reserving its estimate, and only the
// finalized usage counts as consumed.
const UsageTab = () => {
  const { t, i18n } = useTranslation();
  const { getThemeColor } = useAppContext();
  const [period, setPeriod] = useState<Period>('30d');
  const [groupBy, setGroupBy] = useState<LlmUsageGroupBy>('model');
  const from = useMemo(() => fromOf(period), [period]);
  const { data, isLoading, isError, refetch } = useGetLlmUsageQuery({
    from,
    groupBy
  });
  const models = useListLlmModelsQuery(undefined, {
    skip: groupBy !== 'model'
  });

  const num = (n: number) => n.toLocaleString(i18n.language);
  const pctFormat = useMemo(
    () =>
      new Intl.NumberFormat(i18n.language, {
        style: 'percent',
        maximumFractionDigits: 0
      }),
    [i18n.language]
  );

  const rows = useMemo<LabelledRow[]>(() => {
    const names = new Map(
      (models.data?.items ?? []).map(m => [m.uuid, m.name])
    );
    return (data?.rows ?? []).map(r => {
      let label = r.key;
      if (!r.key) {
        label = t(`adminLlm.usage.noKey.${groupBy}`);
      } else if (groupBy === 'model') {
        label =
          names.get(r.key) ?? t('adminLlm.usage.unknownModel', { id: r.key });
      }
      return { ...r, label };
    });
  }, [data?.rows, models.data, groupBy, t]);

  const columns = useMemo<ColumnDef<LabelledRow>[]>(() => {
    const numeric = (
      id: string,
      header: string,
      get: (r: LabelledRow) => number
    ): ColumnDef<LabelledRow> => ({
      id,
      accessorFn: get,
      header,
      meta: {
        headerProps: { className: 'text-end text-900' },
        cellProps: { className: 'text-end' }
      },
      cell: ({ row: { original } }: CellContext<LabelledRow, unknown>) =>
        get(original).toLocaleString(i18n.language)
    });
    return [
      {
        accessorKey: 'label',
        header: t(`adminLlm.usage.groupBy.${groupBy}`),
        meta: { headerProps: { className: 'text-900' } },
        cell: ({ row: { original } }: CellContext<LabelledRow, unknown>) => (
          <span className={groupBy === 'model' ? undefined : 'font-monospace'}>
            {original.label}
          </span>
        )
      },
      numeric(
        'requests',
        t('adminLlm.usage.columns.requests'),
        r => r.requests
      ),
      numeric(
        'inputTokens',
        t('adminLlm.usage.columns.inputTokens'),
        r => r.inputTokens
      ),
      numeric(
        'outputTokens',
        t('adminLlm.usage.columns.outputTokens'),
        r => r.outputTokens
      ),
      numeric(
        'total',
        t('adminLlm.usage.columns.total'),
        r => r.inputTokens + r.outputTokens
      )
    ];
  }, [t, groupBy, i18n.language]);

  // Theme tokens only, so the chart follows light and dark mode.
  const chartOption = useMemo(
    () => ({
      color: [getThemeColor('primary'), getThemeColor('info')],
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        backgroundColor: getThemeColor('gray-100'),
        borderColor: getThemeColor('gray-300'),
        textStyle: { color: getThemeColor('gray-1100') },
        borderWidth: 1,
        valueFormatter: (v: number) => v.toLocaleString(i18n.language)
      },
      legend: { top: 0, textStyle: { color: getThemeColor('gray-700') } },
      grid: { left: 8, right: 8, top: 36, bottom: 8, containLabel: true },
      xAxis: {
        type: 'category',
        data: rows.map(r => r.label),
        axisLine: { lineStyle: { color: getThemeColor('gray-300') } },
        axisLabel: { color: getThemeColor('gray-600') },
        axisTick: { show: false }
      },
      yAxis: {
        type: 'value',
        axisLabel: { color: getThemeColor('gray-600') },
        splitLine: {
          lineStyle: { type: 'dashed', color: getThemeColor('gray-200') }
        }
      },
      series: [
        {
          name: t('adminLlm.usage.series.input'),
          type: 'bar',
          barMaxWidth: 24,
          itemStyle: { borderRadius: [4, 4, 0, 0] },
          data: rows.map(r => r.inputTokens)
        },
        {
          name: t('adminLlm.usage.series.output'),
          type: 'bar',
          barMaxWidth: 24,
          itemStyle: { borderRadius: [4, 4, 0, 0] },
          data: rows.map(r => r.outputTokens)
        }
      ]
    }),
    [rows, t, getThemeColor, i18n.language]
  );

  if (isLoading) return <Spinner animation="border" size="sm" />;
  if (isError || !data) return <LlmLoadError onRetry={() => refetch()} />;

  const { month, budget } = data;
  const consumed = llmMonthConsumed(month);
  const reserved = month.reservedTokens;
  const share = budget ? consumed / budget.monthlyTokenLimit : 0;
  let accent: BadgeColor | undefined;
  if (budget && month.exhaustedAt) accent = 'danger';
  else if (budget && budget.warnPct > 0 && share * 100 >= budget.warnPct) {
    accent = 'warning';
  }

  return (
    <>
      <Row className="g-3 mb-2">
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminLlm.usage.stats.requests.title')}
            value={num(month.requests)}
            icon={faPaperPlane}
            color="primary"
            subtitle={t('adminLlm.usage.stats.requests.subtitle', {
              period: month.period
            })}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminLlm.usage.stats.consumed.title')}
            value={num(consumed)}
            icon={faCoins}
            color="info"
            subtitle={t('adminLlm.usage.stats.consumed.subtitle', {
              org: num(month.orgInputTokens + month.orgOutputTokens),
              user: num(
                month.userAccountInputTokens + month.userAccountOutputTokens
              )
            })}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminLlm.usage.stats.reserved.title')}
            value={num(reserved)}
            icon={faHourglassHalf}
            color="secondary"
            subtitle={t('adminLlm.usage.stats.reserved.subtitle')}
          />
        </Col>
        <Col md={6} xl={3}>
          <StatCard
            title={t('adminLlm.usage.stats.budget.title')}
            value={
              budget
                ? pctFormat.format(share)
                : t('adminLlm.usage.stats.budget.none')
            }
            icon={faGaugeHigh}
            color="warning"
            accent={accent}
            badge={
              budget && month.exhaustedAt
                ? {
                    text: t('adminLlm.usage.stats.budget.exhausted'),
                    bg: 'danger'
                  }
                : undefined
            }
            subtitle={
              budget
                ? t('adminLlm.usage.stats.budget.subtitle', {
                    consumed: num(consumed),
                    reserved: num(reserved),
                    limit: num(budget.monthlyTokenLimit)
                  })
                : t('adminLlm.usage.stats.budget.unlimited')
            }
          />
        </Col>
      </Row>
      <p className="fs-10 text-600 mb-4">{t('adminLlm.usage.admissionNote')}</p>

      <Row className="g-2 mb-3 align-items-end">
        <Col xs="auto">
          <Form.Group controlId="llm-usage-period">
            <Form.Label className="fs-10 mb-1">
              {t('adminLlm.usage.period')}
            </Form.Label>
            <Form.Select
              size="sm"
              value={period}
              onChange={e => setPeriod(e.target.value as Period)}
            >
              {PERIODS.map(p => (
                <option key={p} value={p}>
                  {t(`adminLlm.usage.periods.${p}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
        <Col xs="auto">
          <Form.Group controlId="llm-usage-group">
            <Form.Label className="fs-10 mb-1">
              {t('adminLlm.usage.groupByLabel')}
            </Form.Label>
            <Form.Select
              size="sm"
              value={groupBy}
              onChange={e => setGroupBy(e.target.value as LlmUsageGroupBy)}
            >
              {GROUPS.map(g => (
                <option key={g} value={g}>
                  {t(`adminLlm.usage.groupBy.${g}`)}
                </option>
              ))}
            </Form.Select>
          </Form.Group>
        </Col>
      </Row>

      {rows.length === 0 ? (
        <EmptyState
          icon={faChartColumn}
          message={t('adminLlm.usage.emptyTitle')}
          hint={t('adminLlm.usage.emptyHint')}
        />
      ) : (
        <>
          <h6 className="text-900 mb-2">{t('adminLlm.usage.chartTitle')}</h6>
          <ReactEchart
            echarts={echarts}
            option={chartOption}
            style={{ height: 280 }}
            className="mb-4"
          />
          <SearchablePagedTable
            data={rows}
            columns={columns}
            searchPlaceholder={t('adminLlm.usage.search')}
          />
        </>
      )}
    </>
  );
};

export default UsageTab;
