import { getRandomNumber } from './utils';

interface ChartJsTooltipConfig {
  backgroundColor: string;
  borderColor: string;
  borderWidth: number;
  titleColor: string;
  callbacks: {
    labelTextColor(): string;
  };
}

interface BubbleDataPoint {
  x: number;
  y: number;
  r: number;
}

type GetThemeColorFunction = (colorName: string) => string;

export const chartJsDefaultTooltip = (getThemeColor: GetThemeColorFunction): ChartJsTooltipConfig => ({
  backgroundColor: getThemeColor('gray-100'),
  borderColor: getThemeColor('gray-300'),
  borderWidth: 1,
  titleColor: getThemeColor('gray-1100'),
  callbacks: {
    labelTextColor(): string {
      return getThemeColor('gray-1100');
    }
  }
});

export const getBubbleDataset = (
  count: number, 
  rmin: number, 
  rmax: number, 
  min: number, 
  max: number
): BubbleDataPoint[] => {
  const arr = Array.from(Array(count).keys());
  return arr.map(() => ({
    x: getRandomNumber(min, max),
    y: getRandomNumber(min, max),
    r: getRandomNumber(rmin, rmax)
  }));
};
