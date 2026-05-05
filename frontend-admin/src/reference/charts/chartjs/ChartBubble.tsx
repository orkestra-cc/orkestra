
import FalconComponentCard from 'components/common/FalconComponentCard';
import { rgbaColor } from 'helpers/utils';
import { chartJsDefaultTooltip, getBubbleDataset } from 'helpers/chartjs-utils';
import { Bubble } from 'react-chartjs-2';
import { useAppContext } from 'providers/AppProvider';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    datasets: [
      {
        label: 'Dataset 1',
        data: getBubbleDataset(5, 5, 15, 0, 100),
        backgroundColor: getThemeColor('primary-bg-subtle'),
        hoverBackgroundColor: getThemeColor('primary')
      },
      {
        label: 'Dataset 2',
        data: getBubbleDataset(5, 5, 15, 0, 100),
        backgroundColor: getThemeColor('success-bg-subtle'),
        hoverBackgroundColor: getThemeColor('success')
      },
      {
        label: 'Dataset 3',
        data: getBubbleDataset(5, 5, 15, 0, 100),
        backgroundColor: getThemeColor('danger-bg-subtle'),
        hoverBackgroundColor: getThemeColor('danger')
      }
    ]
  };

  const options = {
    plugins: {
      legend: {
        position: 'top'
      },
      tooltip: chartJsDefaultTooltip(getThemeColor),
      legend: {
        labels: {
          color: getThemeColor('gray-500')
        }
      }
    },
    scales: {
      x: {
        ticks: {
          color: getThemeColor('gray-500')
        },
        grid: {
          color: getThemeColor('gray-300'),
          drawBorder: false
        }
      },
      y: {
        ticks: {
          color: getThemeColor('gray-500')
        },
        grid: {
          color: getThemeColor('gray-300'),
          drawBorder: false,
        }
      }
    }
  };

  return (
    <Bubble data={data} options={options}  width={200}/>
  );

}`;

const ChartBubble = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Bubble Chart" light={false} />

      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Bubble,
          rgbaColor,
          getBubbleDataset,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartBubble;
