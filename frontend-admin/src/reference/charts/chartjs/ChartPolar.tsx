
import FalconComponentCard from 'components/common/FalconComponentCard';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { PolarArea } from 'react-chartjs-2';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    labels: ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun'],
    datasets:[
      {
        data: [10, 20, 50, 40, 30],
        backgroundColor: [
          rgbaColor(getThemeColor('facebook'), 0.5),
          rgbaColor(getThemeColor('youtube'), 0.5),
          rgbaColor(getThemeColor('twitter'), 0.5),
          rgbaColor(getThemeColor('linkedin'), 0.5),
          rgbaColor(getThemeColor('success'), 0.5)
        ],
        borderWidth: 1,
        borderColor: getThemeColor('gray-400')
      }
    ],
    labels: ['Facebook', 'Youtube', 'Twitter', 'Linkedin', 'GitHub'],
    borderColor: getThemeColor('gray-400') //- not working
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
    maintainAspectRatio: false,
    scales: {
      r: {
        grid: {
          color: getThemeColor('gray-300')
        }
      }
    }
  };

    return (
      <PolarArea data={data} options={options} height={350} />
    );
  }
`;
const ChartPolar = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Polar Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          PolarArea,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartPolar;
