import world from 'assets/json/world.json';
import { MapChart } from 'echarts/charts';
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  ToolboxComponent,
  TooltipComponent,
  VisualMapComponent
} from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { rgbaColor } from 'helpers/utils';
import { useAppContext } from 'providers/AppProvider';
import ReactEchart from 'components/common/ReactEchart';
import ReactEChartsCore from 'echarts-for-react/lib/core';
import { CSSProperties, forwardRef } from 'react';

echarts.use([
  TitleComponent,
  TooltipComponent,
  GridComponent,
  MapChart,
  CanvasRenderer,
  ToolboxComponent,
  LegendComponent,
  VisualMapComponent
]);

// eslint-disable-next-line @typescript-eslint/no-explicit-any
echarts.registerMap('world', { geoJSON: world as any, specialAreas: {} });

const total = 6961500;

type ThemeColorGetter = (color: string) => string;
type MapDataItem = { name: string; value: number };
type TooltipParams = { data?: MapDataItem };

const getOptions = (getThemeColor: ThemeColorGetter, data: MapDataItem[], maxZoomLevel: number, minZoomLevel: number) => ({
  tooltip: {
    trigger: 'item',
    padding: [7, 10],
    backgroundColor: getThemeColor('gray-100'),
    borderColor: getThemeColor('gray-300'),
    textStyle: { color: getThemeColor('gray-1100') },
    borderWidth: 1,
    transitionDuration: 0,
    formatter: (params: TooltipParams) =>
      `<strong>${params.data?.name} :</strong> ${(
        ((params.data?.value || 0) / total) *
        100
      ).toFixed(2)}%`
  },
  toolbox: {
    show: false,
    feature: {
      restore: {}
    }
  },
  visualMap: {
    show: false,
    min: 800,
    max: 50000,
    inRange: {
      color: [
        getThemeColor('primary'),
        rgbaColor(getThemeColor('primary'), 0.8),
        rgbaColor(getThemeColor('primary'), 0.6),
        rgbaColor(getThemeColor('primary'), 0.4),
        rgbaColor(getThemeColor('primary'), 0.2)
      ].reverse()
    }
  },
  series: [
    {
      type: 'map',
      map: 'world',
      data,
      roam: true,
      scaleLimit: {
        min: minZoomLevel,
        max: maxZoomLevel
      },
      left: 0,
      right: 0,
      label: {
        show: false
      },
      itemStyle: {
        borderColor: getThemeColor('gray-300')
      },
      emphasis: {
        label: {
          show: false
        },
        itemStyle: {
          areaColor: getThemeColor('warning')
        }
      }
    }
  ]
});

interface WorldMapProps {
  data: MapDataItem[];
  style?: CSSProperties;
  minZoomLevel?: number;
  maxZoomLevel?: number;
}

const WorldMap = forwardRef<ReactEChartsCore, WorldMapProps>(
  ({ data, style, minZoomLevel = 1, maxZoomLevel = 5 }, ref) => {
    const { getThemeColor } = useAppContext();
    return (
      <ReactEchart
        ref={ref}
        echarts={echarts}
        option={getOptions(getThemeColor, data, maxZoomLevel, minZoomLevel)}
        style={style}
      />
    );
  }
);

WorldMap.displayName = 'WorldMap';

export default WorldMap;
