
import { Range, getTrackBackground } from 'react-range';
import { useAppContext } from 'providers/AppProvider';

interface OrkestraReactRangeProps {
  step?: number;
  min?: number;
  max?: number;
  variant?: string;
  trackHeight?: string;
  tipFormatter?: (value: number) => string;
  draggableTrack?: boolean;
  alwaysShowTooltip?: boolean;
  marks?: boolean;
  values: number[];
  onChange: (values: number[]) => void;
}

const OrkestraReactRange = ({
  step = 0.1,
  min = 0,
  max = 100,
  variant = 'primary',
  trackHeight = '0.75rem',
  tipFormatter,
  draggableTrack = false,
  alwaysShowTooltip = false,
  marks = false,
  values,
  onChange
}: OrkestraReactRangeProps) => {
  const {
    config: { isDark, isRTL },
    getThemeColor
  } = useAppContext();

  const Track = ({ props: properties, children }: { props: any; children: React.ReactNode }) => (
    <div
      key={properties.key}
      onMouseDown={properties.onMouseDown}
      onTouchStart={properties.onTouchStart}
      style={{
        ...properties.style
      }}
      className="orkestra-react-range"
    >
      <div
        ref={properties.ref}
        className="orkestra-react-range-track"
        style={{
          height: trackHeight,
          cursor: !draggableTrack ? 'pointer' : 'ew-resize',
          background: getTrackBackground({
            values,
            colors:
              values.length == 2
                ? [
                    getThemeColor('gray-300'),
                    getThemeColor(variant),
                    getThemeColor('gray-300')
                  ]
                : [getThemeColor(variant), getThemeColor('gray-300')],
            min,
            max,
            rtl: isRTL
          })
        }}
      >
        {children}
      </div>
    </div>
  );

  const Thumb = ({ props: properties, isDragged, index }: { props: any; isDragged: boolean; index: number }) => (
    <div
      {...properties}
      key={properties.key}
      className={`orkestra-react-range-thumb ${isDragged && 'dragging'}`}
      style={{
        ...properties.style
      }}
    >
      <div
        className={`orkestra-react-range-tooltip ${
          (alwaysShowTooltip || isDragged) && 'show'
        }`}
      >
        {tipFormatter
          ? tipFormatter(values[index])
          : values[index].toFixed(1)}
      </div>
    </div>
  );

  const Mark = ({ props: properties, index }: { props: any; index: number }) => {
    return (
      <div
        {...properties}
        key={properties.key}
        className="orkestra-react-range-mark"
        style={{
          ...properties.style,
          height: '16px',
          width: '4px',
          backgroundColor:
            values.length === 1
              ? index * step < values[0]
                ? getThemeColor(variant)
                : getThemeColor('gray-300')
              : index * step > values[0] && index * step < values[1]
              ? getThemeColor(variant)
              : getThemeColor('gray-300')
        }}
      ></div>
    );
  };

  return (
    <Range
      draggableTrack={draggableTrack}
      key={isDark ? 'dark' : 'light'}
      values={values}
      step={step}
      min={min}
      max={max}
      onChange={onChange}
      renderTrack={Track}
      renderThumb={Thumb}
      renderMark={marks ? Mark : undefined}
      rtl={isRTL}
    />
  );
};

export default OrkestraReactRange;
