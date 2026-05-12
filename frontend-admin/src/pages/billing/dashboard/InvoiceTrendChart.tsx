import { useMemo } from 'react';
import { Card, Spinner } from 'react-bootstrap';
import { BarChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TooltipComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import ReactEchart from 'components/common/ReactEchart';
import { useGetBillingStatsQuery } from 'store/api/billingApi';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { faExclamationTriangle } from '@fortawesome/free-solid-svg-icons';
import { formatCurrency, WeeklyInvoiceData } from 'types/billing';

echarts.use([
  TooltipComponent,
  GridComponent,
  BarChart,
  CanvasRenderer,
  LegendComponent
]);

// Month names in Italian
const MONTH_NAMES = [
  'Gen',
  'Feb',
  'Mar',
  'Apr',
  'Mag',
  'Giu',
  'Lug',
  'Ago',
  'Set',
  'Ott',
  'Nov',
  'Dic'
];

// Get the month for a given ISO week number (approximate)
// ISO week 1 starts around Jan 4, each month is roughly 4-5 weeks
const getMonthForWeek = (week: number): number => {
  // Approximate: week 1-4 = Jan, 5-8 = Feb, etc.
  // More accurate mapping based on typical ISO week distribution
  if (week <= 4) return 0; // Jan
  if (week <= 8) return 1; // Feb
  if (week <= 13) return 2; // Mar
  if (week <= 17) return 3; // Apr
  if (week <= 22) return 4; // May
  if (week <= 26) return 5; // Jun
  if (week <= 30) return 6; // Jul
  if (week <= 35) return 7; // Aug
  if (week <= 39) return 8; // Sep
  if (week <= 43) return 9; // Oct
  if (week <= 48) return 10; // Nov
  return 11; // Dec
};

// Build 53-week arrays from weekly data, filling missing weeks with zeros
const buildWeeklyArrays = (
  weeklyData: WeeklyInvoiceData[] | undefined,
  year: number
) => {
  const issuedAmounts = new Array(53).fill(0);
  const receivedAmounts = new Array(53).fill(0);

  if (weeklyData) {
    for (const data of weeklyData) {
      if (data.year === year && data.week >= 1 && data.week <= 53) {
        issuedAmounts[data.week - 1] = Math.round(data.issuedAmount);
        receivedAmounts[data.week - 1] = Math.round(data.receivedAmount);
      }
    }
  }

  return { issuedAmounts, receivedAmounts };
};

// Generate week labels with month indicators
const generateWeekLabels = (): string[] => {
  const labels: string[] = [];
  let lastMonth = -1;

  for (let week = 1; week <= 53; week++) {
    const month = getMonthForWeek(week);
    if (month !== lastMonth) {
      labels.push(MONTH_NAMES[month]);
      lastMonth = month;
    } else {
      labels.push('');
    }
  }

  return labels;
};

const InvoiceTrendChart = () => {
  const currentYear = new Date().getFullYear();
  const fromDate = `${currentYear}-01-01`;
  const toDate = `${currentYear}-12-31`;

  const {
    data: stats,
    isLoading,
    error
  } = useGetBillingStatsQuery({ fromDate, toDate });

  const { issuedAmounts, receivedAmounts } = useMemo(
    () => buildWeeklyArrays(stats?.weeklyData, currentYear),
    [stats?.weeklyData, currentYear]
  );

  const weekLabels = useMemo(() => generateWeekLabels(), []);

  const getChartOptions = () => {
    if (!stats) return {};

    return {
      tooltip: {
        trigger: 'axis',
        axisPointer: {
          type: 'shadow'
        },
        formatter: (params: any) => {
          const weekIndex = params[0].dataIndex;
          const weekNum = weekIndex + 1;
          const month = MONTH_NAMES[getMonthForWeek(weekNum)];
          let result = `<div class="fw-medium mb-1">Settimana ${weekNum} (${month})</div>`;
          params.forEach((param: any) => {
            if (param.value > 0) {
              result += `<div class="d-flex align-items-center">
                <span class="badge rounded-circle p-1 me-2" style="background-color: ${param.color}"></span>
                <span>${param.seriesName}: ${formatCurrency(param.value)}</span>
              </div>`;
            }
          });
          // Show message if no data
          if (params.every((p: any) => p.value === 0)) {
            result += '<div class="text-muted">Nessuna fattura</div>';
          }
          return result;
        }
      },
      legend: {
        data: ['Fatture Emesse', 'Fatture Ricevute'],
        bottom: 0,
        textStyle: {
          color: '#8991a7'
        }
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '15%',
        top: '3%',
        containLabel: true
      },
      xAxis: {
        type: 'category',
        data: weekLabels,
        axisLine: {
          lineStyle: {
            color: '#e6e6e6'
          }
        },
        axisLabel: {
          color: '#8991a7',
          interval: 0,
          rotate: 0
        },
        axisTick: {
          show: false
        }
      },
      yAxis: {
        type: 'value',
        axisLabel: {
          color: '#8991a7',
          formatter: (value: number) => {
            if (value >= 1000) {
              return `${(value / 1000).toFixed(0)}K`;
            }
            return value.toString();
          }
        },
        splitLine: {
          lineStyle: {
            color: '#f0f0f0'
          }
        }
      },
      series: [
        {
          name: 'Fatture Emesse',
          type: 'bar',
          barGap: '0%',
          barCategoryGap: '40%',
          emphasis: {
            focus: 'series'
          },
          itemStyle: {
            color: '#10b981',
            borderRadius: [2, 2, 0, 0]
          },
          data: issuedAmounts
        },
        {
          name: 'Fatture Ricevute',
          type: 'bar',
          emphasis: {
            focus: 'series'
          },
          itemStyle: {
            color: '#3b82f6',
            borderRadius: [2, 2, 0, 0]
          },
          data: receivedAmounts
        }
      ]
    };
  };

  if (isLoading) {
    return (
      <Card className="h-100">
        <OrkestraCardHeader title="Andamento Fatturazione" titleTag="h6" light />
        <Card.Body
          className="d-flex align-items-center justify-content-center"
          style={{ minHeight: 300 }}
        >
          <Spinner animation="border" />
        </Card.Body>
      </Card>
    );
  }

  if (error || !stats) {
    return (
      <Card className="h-100">
        <OrkestraCardHeader title="Andamento Fatturazione" titleTag="h6" light />
        <Card.Body
          className="d-flex align-items-center justify-content-center"
          style={{ minHeight: 300 }}
        >
          <div className="text-warning text-center">
            <FontAwesomeIcon
              icon={faExclamationTriangle}
              className="fs-3 mb-2 d-block mx-auto"
            />
            <span>Impossibile caricare il grafico</span>
          </div>
        </Card.Body>
      </Card>
    );
  }

  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Andamento Fatturazione"
        titleTag="h6"
        light
        endEl={
          <div className="d-flex gap-3 fs-10">
            <div>
              <span className="text-body-tertiary">Emesse: </span>
              <span className="fw-medium text-success">
                {formatCurrency(stats.issuedAmount)}
              </span>
            </div>
            <div>
              <span className="text-body-tertiary">Ricevute: </span>
              <span className="fw-medium text-primary">
                {formatCurrency(stats.receivedAmount)}
              </span>
            </div>
          </div>
        }
      />
      <Card.Body>
        <ReactEchart
          echarts={echarts}
          option={getChartOptions()}
          style={{ height: 280 }}
        />
      </Card.Body>
    </Card>
  );
};

export default InvoiceTrendChart;
