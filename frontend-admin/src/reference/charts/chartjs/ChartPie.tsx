
import FalconComponentCard from 'components/common/FalconComponentCard';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import { Pie } from 'react-chartjs-2';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    labels: ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun'],
    datasets:[
      {
        data: [5, 3, 2, 1, 1],
        backgroundColor: [
          rgbaColor(getThemeColor('facebook'), 0.75),
          rgbaColor(getThemeColor('youtube'), 0.75),
          rgbaColor(getThemeColor('twitter'), 0.75),
          rgbaColor(getThemeColor('linkedin'), 0.75),
          rgbaColor(getThemeColor('github'), 0.75)
        ],
        borderWidth: 1,
        borderColor: getThemeColor('gray-100')
      }
    ],
    labels: ['Facebook', 'Youtube', 'Twitter', 'Linkedin', 'GitHub']
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
    maintainAspectRatio: false
  };

    return (
      <Pie data={data} options={options} height={350} width={350}/>
    );
  }
`;
const ChartPie = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Pie Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Pie,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartPie;
