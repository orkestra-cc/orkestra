
import FalconComponentCard from 'components/common/FalconComponentCard';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { Bar } from 'react-chartjs-2';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    labels: ['January', 'February', 'March', 'April', 'May', 'June', 'July'],
    datasets: [
      {
        type: 'line',
        label: 'Dataset 1',
        borderColor: getThemeColor('primary'),
        borderWidth: 2,
        fill: false,
        data: [55, 80, -60, -22, -50, 40, 90]
      },
      {
        type: 'bar',
        label: 'Dataset 2',
        backgroundColor: getThemeColor('danger-bg-subtle'),
        data: [4, -80, 90, -22, 70, 35, -50],
        borderWidth: 1
      },
      {
        type: 'bar',
        label: 'Dataset 3',
        backgroundColor: getThemeColor('primary-bg-subtle'),
        data: [-30, 30, -18, 100, -45, -25, -50],
        borderWidth: 1
      }
    ]
  };

  const options = {
    maintainAspectRatio: false,
    plugins: {
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
      <Bar data={data} options={options} height={500} width={1618}/>
    );
  }
`;
const ChartCombo = () => {
  return (
    <FalconComponentCard className="mb-0">
      <FalconComponentCard.Header title="Combo Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Bar,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartCombo;
