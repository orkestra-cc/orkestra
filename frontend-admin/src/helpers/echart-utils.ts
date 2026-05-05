import dayjs from 'dayjs';

interface TooltipPosition {
  top: number;
  left: number;
}

interface TooltipSize {
  contentSize: [number, number];
}

export interface TooltipParam {
  borderColor?: string;
  color: string;
  seriesName: string;
  value: number | [number, number] | any;
  axisValue?: string | number | Date;
}

export const getPosition = (
  pos: [number, number],
  _params: TooltipParam | TooltipParam[],
  _dom: HTMLElement,
  _rect: { x: number; y: number; width: number; height: number } | DOMRect,
  size: TooltipSize
): TooltipPosition => ({
  top: pos[1] - size.contentSize[1] - 10,
  left: pos[0] - size.contentSize[0] / 2
});

export const tooltipFormatter = (params: TooltipParam | TooltipParam[]): string => {
  let tooltipItem = ``;
  
  // Ensure params is an array
  const paramsArray = Array.isArray(params) ? params : [params];
  
  if (paramsArray.length > 0) {
    paramsArray.forEach((el: TooltipParam) => {
      tooltipItem =
        tooltipItem +
        `<div class='ms-1'> 
      <h6 class="text-700">
      <div class="dot me-1 fs-11 d-inline-block" style="background-color:${
        el.borderColor ? el.borderColor : el.color
      }"></div>
      ${el.seriesName} : ${
          typeof el.value === 'object' ? el.value[1] : el.value
        }
      </h6>
      </div>`;
    });
  }
  
  const firstParam = paramsArray[0];
  return `<div>
            <p class='mb-2 text-600'>
              ${
                firstParam?.axisValue && dayjs(firstParam.axisValue).isValid()
                  ? dayjs(firstParam.axisValue).format('MMMM DD')
                  : firstParam?.axisValue || ''
              }
            </p>
            ${tooltipItem}
          </div>`;
};
