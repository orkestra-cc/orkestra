
import FalconComponentCard from 'components/common/FalconComponentCard';
import { rgbaColor } from 'helpers/utils';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { Doughnut } from 'react-chartjs-2';
import { useAppContext } from 'providers/AppProvider';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    labels: ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun'],
    datasets:[
      {
        data: [5, 3, 2, 1, 1],
        backgroundColor: [
          getThemeColor('facebook'),
          getThemeColor('youtube'),
          getThemeColor('twitter'),
          getThemeColor('linkedin'),
          getThemeColor('github')
        ],
        borderWidth: 1,
        borderColor: [
          getThemeColor('facebook'),
          getThemeColor('youtube'),
          getThemeColor('twitter'),
          getThemeColor('linkedin'),
          getThemeColor('github')
        ]
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
      <Doughnut data={data} options={options} height={350} width={350}/>
    );
  }
`;
const ChartDoughnut = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Doughnut Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Doughnut,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartDoughnut;
