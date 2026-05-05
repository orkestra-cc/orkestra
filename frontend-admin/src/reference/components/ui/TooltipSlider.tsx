import * as React from 'react';
import 'rc-tooltip/assets/bootstrap.css';
import Slider from 'rc-slider';
import raf from 'rc-util/lib/raf';
import Tooltip from 'rc-tooltip';

interface HandleTooltipProps {
  value: number;
  children: React.ReactElement;
  visible: boolean;
  tipFormatter?: (val: number) => string;
  [key: string]: any;
}

const HandleTooltip = (props: HandleTooltipProps) => {
  const {
    value,
    children,
    visible,
    tipFormatter = (val: number) => `${val} %`,
    ...restProps
  } = props;

  const tooltipRef = React.useRef<any>(null);
  const rafRef = React.useRef<number | null>(null);

  function cancelKeepAlign() {
    if (rafRef.current !== null) {
      raf.cancel(rafRef.current);
    }
  }

  function keepAlign() {
    rafRef.current = raf(() => {
      tooltipRef.current?.forcePopupAlign();
    });
  }

  React.useEffect(() => {
    if (visible) {
      keepAlign();
    } else {
      cancelKeepAlign();
    }

    return cancelKeepAlign;
  }, [value, visible]);

  return (
    <Tooltip
      placement="top"
      overlay={tipFormatter(value)}
      overlayInnerStyle={{ minHeight: 'auto' }}
      ref={tooltipRef}
      visible={visible}
      {...restProps}
    >
      {children}
    </Tooltip>
  );
};

export const handleRender = (node: React.ReactElement, props: any) => {
  return (
    <HandleTooltip value={props.value} visible={props.dragging}>
      {node}
    </HandleTooltip>
  );
};

interface TooltipSliderProps {
  tipFormatter?: (val: number) => string;
  tipProps?: any;
  [key: string]: any;
}

const TooltipSlider = ({ tipFormatter, tipProps, ...props }: TooltipSliderProps) => {
  const tipHandleRender = (node: React.ReactElement, handleProps: any) => {
    return (
      <HandleTooltip
        value={handleProps.value}
        visible={handleProps.dragging}
        tipFormatter={tipFormatter}
        {...tipProps}
      >
        {node}
      </HandleTooltip>
    );
  };

  return <Slider {...props} handleRender={tipHandleRender} />;
};

export default TooltipSlider;
