import dayjs from 'dayjs';
import duration from 'dayjs/plugin/duration';
import { RefObject } from 'react';

dayjs.extend(duration);

export const isIterableArray = function (array: unknown): array is unknown[] {
  return Array.isArray(array) && !!array.length;
};

//===============================
// Breakpoints
//===============================
interface Breakpoints {
  xs: number;
  sm: number;
  md: number;
  lg: number;
  xl: number;
  xxl: number;
}

export const breakpoints: Breakpoints = {
  xs: 0,
  sm: 576,
  md: 768,
  lg: 992,
  xl: 1200,
  xxl: 1540
};

export const getItemFromStore = function<T>(
  key: string, 
  defaultValue: T, 
  store: Storage = localStorage
): T {
  try {
    const item = store.getItem(key);
    return item === null ? defaultValue : JSON.parse(item);
  } catch {
    const item = store.getItem(key);
    return (item as unknown as T) || defaultValue;
  }
};

export const setItemToStore = (
  key: string, 
  payload: string, 
  store: Storage = localStorage
): void => {
  store.setItem(key, payload);
};

export const getStoreSpace = (store: Storage = localStorage): number =>
  parseFloat(
    (
      escape(encodeURIComponent(JSON.stringify(store))).length /
      (1024 * 1024)
    ).toFixed(2)
  );

//===============================
// Cookie
//===============================
export const getCookieValue = (name: string): string | null => {
  const value = document.cookie.match(
    '(^|[^;]+)\\s*' + name + '\\s*=\\s*([^;]+)'
  );
  return value ? value.pop() || null : null;
};

export const createCookie = (name: string, value: string, cookieExpireTime: number): void => {
  const date = new Date();
  date.setTime(date.getTime() + cookieExpireTime);
  const expires = '; expires=' + date.toUTCString();
  document.cookie = name + '=' + value + expires + '; path=/';
};

export const numberFormatter = (number: number | string, fixed: number = 2): string => {
  const num = Math.abs(Number(number));
  // Nine Zeroes for Billions
  return num >= 1.0e9
    ? (num / 1.0e9).toFixed(fixed) + 'B'
    : // Six Zeroes for Millions
    num >= 1.0e6
    ? (num / 1.0e6).toFixed(fixed) + 'M'
    : // Three Zeroes for Thousands
    num >= 1.0e3
    ? (num / 1.0e3).toFixed(fixed) + 'K'
    : num.toFixed(fixed);
};

//===============================
// Colors
//===============================
type RgbTuple = [number, number, number];

export const hexToRgb = (hexValue: string): RgbTuple | null => {
  let hex: string;
  hexValue.indexOf('#') === 0
    ? (hex = hexValue.substring(1))
    : (hex = hexValue);
  // Expand shorthand form (e.g. "03F") to full form (e.g. "0033FF")
  const shorthandRegex = /^#?([a-f\d])([a-f\d])([a-f\d])$/i;
  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(
    hex.replace(shorthandRegex, (_m, r, g, b) => r + r + g + g + b + b)
  );
  return result
    ? [
        parseInt(result[1], 16),
        parseInt(result[2], 16),
        parseInt(result[3], 16)
      ]
    : null;
};

export const rgbColor = (color: string = colors[0]): string => `rgb(${hexToRgb(color)})`;
export const rgbaColor = (color: string = colors[0], alpha: number = 0.5): string =>
  `rgba(${hexToRgb(color)},${alpha})`;

export const colors: string[] = [
  '#2c7be5',
  '#00d97e',
  '#e63757',
  '#39afd1',
  '#fd7e14',
  '#02a8b5',
  '#727cf5',
  '#6b5eae',
  '#ff679b',
  '#f6c343'
];

interface ThemeColors {
  primary: string;
  secondary: string;
  success: string;
  info: string;
  warning: string;
  danger: string;
  light: string;
  dark: string;
}

export const themeColors: ThemeColors = {
  primary: '#2c7be5',
  secondary: '#748194',
  success: '#00d27a',
  info: '#27bcfd',
  warning: '#f5803e',
  danger: '#e63757',
  light: '#f9fafd',
  dark: '#0b1727'
};

interface GrayColors {
  white: string;
  100: string;
  200: string;
  300: string;
  400: string;
  500: string;
  600: string;
  700: string;
  800: string;
  900: string;
  1000: string;
  1100: string;
  black: string;
}

export const grays: GrayColors = {
  white: '#fff',
  100: '#f9fafd',
  200: '#edf2f9',
  300: '#d8e2ef',
  400: '#b6c1d2',
  500: '#9da9bb',
  600: '#748194',
  700: '#5e6e82',
  800: '#4d5969',
  900: '#344050',
  1000: '#232e3c',
  1100: '#0b1727',
  black: '#000'
};

export const darkGrays: GrayColors = {
  white: '#fff',
  1100: '#f9fafd',
  1000: '#edf2f9',
  900: '#d8e2ef',
  800: '#b6c1d2',
  700: '#9da9bb',
  600: '#748194',
  500: '#5e6e82',
  400: '#4d5969',
  300: '#344050',
  200: '#232e3c',
  100: '#0b1727',
  black: '#000'
};

export const getGrays = (isDark: boolean): GrayColors => (isDark ? darkGrays : grays);

export const rgbColors: string[] = colors.map(color => rgbColor(color));
export const rgbaColors: string[] = colors.map(color => rgbaColor(color));

export const getColor = (name: string): string => {
  const dom = document.documentElement;
  return getComputedStyle(dom).getPropertyValue(`--falcon-${name}`).trim();
};

//===============================
// Echarts
//===============================
interface EchartsTooltipSize {
  contentSize: [number, number];
}

interface EchartsTooltipPosition {
  top: number;
  left: number;
}

export const getPosition = (
  pos: [number, number], 
  _params: any, 
  _dom: HTMLElement, 
  _rect: DOMRect, 
  size: EchartsTooltipSize
): EchartsTooltipPosition => ({
  top: pos[1] - size.contentSize[1] - 10,
  left: pos[0] - size.contentSize[0] / 2
});

//===============================
// Helpers
//===============================
export const getPaginationArray = (totalSize: number, sizePerPage: number): number[] => {
  const noOfPages = Math.ceil(totalSize / sizePerPage);
  const array: number[] = [];
  let pageNo = 1;
  while (pageNo <= noOfPages) {
    array.push(pageNo);
    pageNo = pageNo + 1;
  }
  return array;
};

export const capitalize = (str: string): string =>
  (str.charAt(0).toUpperCase() + str.slice(1)).replace(/-/g, ' ');

export const camelize = (str: string): string => {
  return str.replace(/(?:^\w|[A-Z]|\b\w|\s+)/g, function (match, index) {
    if (+match === 0) return ''; // or if (/\s+/.test(match)) for white spaces
    return index === 0 ? match.toLowerCase() : match.toUpperCase();
  });
};

export const dashed = (str: string): string => {
  return str.toLowerCase().replace(/ /g, '-');
};

//routes helper

interface Route {
  name?: string;
  children?: Route[];
  [key: string]: any;
}

interface FlatRoutesResult {
  [key: string]: Route[];
  unTitled: Route[];
}

interface RoutesSlicerParams {
  routes: Route[];
  columns?: number;
  rows?: number;
}

export const flatRoutes = (childrens: Route[]): Route[] => {
  const allChilds: Route[] = [];

  const flatChild = (childrens: Route[]): void => {
    childrens.forEach(child => {
      if (child.children) {
        flatChild(child.children);
      } else {
        allChilds.push(child);
      }
    });
  };
  flatChild(childrens);

  return allChilds;
};

export const getFlatRoutes = (children: Route[]): FlatRoutesResult =>
  children.reduce(
    (acc: FlatRoutesResult, val: Route) => {
      if (val.children) {
        return {
          ...acc,
          [camelize(val.name || '')]: flatRoutes(val.children)
        };
      } else {
        return {
          ...acc,
          unTitled: [...acc.unTitled, val]
        };
      }
    },
    { unTitled: [] }
  );

export const routesSlicer = ({ routes, columns = 3, rows }: RoutesSlicerParams): Route[][] => {
  const routesCollection: Route[] = [];
  routes.map(route => {
    if (route.children) {
      return route.children.map(item => {
        if (item.children) {
          return routesCollection.push(...item.children);
        }
        return routesCollection.push(item);
      });
    }
    return routesCollection.push(route);
  });

  const totalRoutes = routesCollection.length;
  const calculatedRows = rows || Math.ceil(totalRoutes / columns);
  const routesChunks: Route[][] = [];
  for (let i = 0; i < totalRoutes; i += calculatedRows) {
    routesChunks.push(routesCollection.slice(i, i + calculatedRows));
  }
  return routesChunks;
};

export const getPageName = (pageName: string): boolean => {
  return window.location.pathname.split('/').slice(-1)[0] === pageName;
};

export const copyToClipBoard = (textFieldRef: RefObject<HTMLInputElement | HTMLTextAreaElement>): void => {
  const textField = textFieldRef.current;
  if (textField) {
    textField.focus();
    textField.select();
    document.execCommand('copy');
  }
};

export const reactBootstrapDocsUrl: string = 'https://react-bootstrap.github.io';

export const pagination = (currentPage: number, size: number): number[] => {
  const pages = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
  let prev = currentPage - 1 - Math.floor(size / 2);

  if (currentPage - 1 - Math.floor(size / 2) < 0) {
    prev = 0;
  }
  if (currentPage - 1 - Math.floor(size / 2) > pages.length - size) {
    prev = pages.length - size;
  }
  const next = prev + size;

  return pages.slice(prev, next);
};

interface TooltipParam {
  borderColor?: string;
  color: string;
  seriesName: string;
  value: number | [number, number] | any;
  axisValue?: string | number | Date;
}

export const tooltipFormatter = (params: TooltipParam[]): string => {
  let tooltipItem = ``;
  params.forEach((el: TooltipParam) => {
    tooltipItem =
      tooltipItem +
      `<div class='ms-1'> 
        <h6 class="text-700"><span class="fas fa-circle me-1 fs-11" style="color:${
          el.borderColor ? el.borderColor : el.color
        }"></span>
          ${el.seriesName} : ${
        typeof el.value === 'object' ? el.value[1] : el.value
      }
        </h6>
      </div>`;
  });
  return `<div>
            <p class='mb-2 text-600'>
              ${
                params[0]?.axisValue && dayjs(params[0].axisValue).isValid()
                  ? dayjs(params[0].axisValue).format('MMMM DD')
                  : params[0]?.axisValue || ''
              }
            </p>
            ${tooltipItem}
          </div>`;
};

export const addIdField = function<T extends Record<string, any>>(items: T[]): (T & { id: number })[] {
  return items.map((item, index) => ({
    id: index + 1,
    ...item
  }));
};

// get file size

export const getSize = (size: number): string => {
  if (size < 1024) {
    return `${size} Byte`;
  } else if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(2)} KB`;
  } else {
    return `${(size / (1024 * 1024)).toFixed(2)} MB`;
  }
};

/* Get A Random Number */
export const getRandomNumber = (min: number, max: number): number => {
  return Math.floor(Math.random() * (max - min) + min);
};

/* get Dates between */

export const getDates = (
  startDate: Date,
  endDate: Date,
  interval: number = 1000 * 60 * 60 * 24
): Date[] => {
  // Ensure we have proper Date objects
  const start = startDate instanceof Date ? startDate : new Date(startDate);
  const end = endDate instanceof Date ? endDate : new Date(endDate);

  const duration = end.getTime() - start.getTime();
  const steps = duration / interval;
  return Array.from(
    { length: steps + 1 },
    (_v, i) => new Date(start.valueOf() + interval * i)
  );
};

/* Get Past Dates */
export const getPastDates = (duration: 'week' | 'month' | 'year' | number): Date[] => {
  let days: number;

  switch (duration) {
    case 'week':
      days = 7;
      break;
    case 'month':
      days = 30;
      break;
    case 'year':
      days = 365;
      break;

    default:
      days = duration;
  }

  const date = new Date();
  const endDate = date;
  const startDate = new Date(new Date().setDate(date.getDate() - (days - 1)));
  return getDates(startDate, endDate);
};

// Add id to items in array
export const addId = function<T extends Record<string, any>>(items: T[]): (T & { id: number })[] {
  return items.map((item, index) => ({
    id: index + 1,
    ...item
  }));
};

//
export const getTimeDuration = (startDate: dayjs.Dayjs, endDate: dayjs.Dayjs, format: string = ''): string => {
  return dayjs.duration(endDate.diff(startDate)).format(format);
};

// Get Percentage
export const getPercentage = (number: number | string, percent: number | string): number => {
  return (Number(number) / 100) * Number(percent);
};

//get chunk from array
export const chunk = function<T>(arr: T[], chunkSize: number = 1, cache: T[][] = []): T[][] {
  const tmp = [...arr];
  if (chunkSize <= 0) return cache;
  while (tmp.length) cache.push(tmp.splice(0, chunkSize));
  return cache;
};

// Slugify text
export const slugifyText = (str: string): string =>
  str
    .toLowerCase()
    .replace(/\s+/g, '-')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/--+/g, '-')
    .replace(/^-+/, '')
    .replace(/-+$/, '');
