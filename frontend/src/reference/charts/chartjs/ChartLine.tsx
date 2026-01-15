
import FalconComponentCard from 'components/common/FalconComponentCard';
import { rgbaColor } from 'helpers/utils';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { Line } from 'react-chartjs-2';
import { useAppContext } from 'providers/AppProvider';

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
        data: [55, 80, 60, 22, 50, 40, 90],
        tension: 0.3
      }
    ]
  };

  const options = {
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
      <Line data={data} options={options}  height={1000} width={1618} />
    );  
  }
`;
const ChartLine = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Line Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Line,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartLine;
