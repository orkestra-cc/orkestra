
import FalconComponentCard from 'components/common/FalconComponentCard';
import { rgbaColor } from 'helpers/utils';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { Radar } from 'react-chartjs-2';
import { useAppContext } from 'providers/AppProvider';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data =  {
    labels: ['English', 'Maths', 'Physics', 'Chemistry', 'Biology', 'History'],
    datasets: [
      {
        label: 'Student A',
        backgroundColor: rgbaColor(getThemeColor('success'), 0.5),
        data: [65, 75, 70, 80, 60, 80],
        borderWidth: 1
      },
      {
        label: 'Student B',
        backgroundColor: rgbaColor(getThemeColor('primary'), 0.5),
        data: [54, 65, 60, 70, 70, 75],
        borderWidth: 1
      }
    ]
  }

  const options= {
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
        },
        pointLabels: {
          color: getThemeColor('gray-500')
        }
      },
    }
  }

    return (
      <Radar data={data} options={options}  height={350} />
    );
  }
`;
const ChartRadar = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Radar Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Radar,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartRadar;
