
import FalconComponentCard from 'components/common/FalconComponentCard';
import { rgbaColor } from 'helpers/utils';
import { chartJsDefaultTooltip } from 'helpers/chartjs-utils';
import { Scatter } from 'react-chartjs-2';
import { useAppContext } from 'providers/AppProvider';

const chartCode = `function ChartOptions() {
  const { getThemeColor } = useAppContext();
  const data = {
    datasets: [
      {
        label: 'Dataset one',
        data: [
          {
            x: -98,
            y: 42
          },
          {
            x: -85,
            y: -29
          },
          {
            x: -87,
            y: -70
          },
          {
            x: -53,
            y: 28
          },
          {
            x: -29,
            y: 4
          },
          {
            x: -2,
            y: -42
          },
          {
            x: 5,
            y: 3
          },
          {
            x: 39,
            y: 19
          },
          {
            x: 49,
            y: 79
          },
          {
            x: 83,
            y: -9
          },
          {
            x: 93,
            y: 12
          }
        ],
        pointBackgroundColor: getThemeColor('primary'),
        borderColor: getThemeColor('primary'),
        borderWidth: 1
      },
      {
        label: 'Dataset Two',
        data: [
          {
            x: 53,
            y: 12
          },
          {
            x: -78,
            y: 42
          },
          {
            x: -65,
            y: -39
          },
          {
            x: -57,
            y: -20
          },
          {
            x: 57,
            y: 28
          },
          {
            x: -35,
            y: 75
          },
          {
            x: -29,
            y: -43
          },
          {
            x: 15,
            y: 31
          },
          {
            x: 97,
            y: 19
          },
          {
            x: 49,
            y: 69
          },
          {
            x: 33,
            y: -57
          }
        ],
        pointBackgroundColor: getThemeColor('warning'),
        borderColor: getThemeColor('warning'),
        borderWidth: 1,
        borderRadius: '50%'
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
            drawBorder: false
          }
        }
      },
      animation: {
        duration: 2000
      }
    }
  };

  return (
    <Scatter data={data} options={options} width={200}/>
  )

}`;

const ChartScatter = () => {
  return (
    <FalconComponentCard>
      <FalconComponentCard.Header title="Scatter Chart" light={false} />
      <FalconComponentCard.Body
        code={chartCode}
        language="jsx"
        scope={{
          Scatter,
          rgbaColor,
          chartJsDefaultTooltip,
          useAppContext
        }}
      />
    </FalconComponentCard>
  );
};

export default ChartScatter;
