
import FalconComponentCard from 'components/common/FalconComponentCard';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { Bar } from 'react-chartjs-2';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    labels: ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun'],
    datasets: [
      {
        label: '# of Votes',
        data: [12, 19, 3, 5, 2, 3],
        backgroundColor: [
          rgbaColor(getThemeColor('secondary'), 0.2),
          rgbaColor(getThemeColor('warning'), 0.2),
          rgbaColor(getThemeColor('info'), 0.2),
          rgbaColor(getThemeColor('success'), 0.2),
          rgbaColor(getThemeColor('info'), 0.2),
          rgbaColor(getThemeColor('primary'), 0.2)
        ],
        borderColor: [
          getThemeColor('secondary'),
          getThemeColor('warning'),
          getThemeColor('info'),
          getThemeColor('success'),
          getThemeColor('info'),
          getThemeColor('primary')
        ],
        borderWidth: 1
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
      <Bar 
        data={data} 
        options={options} 
        height={1000} 
        width={1618}
        className='chartjs-responsive'
      />
    );
  }
`;
const ChartBar = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Bar Chart" light={false} />
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

export default ChartBar;
