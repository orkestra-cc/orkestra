interface ResizeHandleProps {
  direction: 'vertical' | 'horizontal';
  isDragging?: boolean;
  onPointerDown: (e: React.PointerEvent) => void;
}

const ResizeHandle: React.FC<ResizeHandleProps> = ({ direction, isDragging, onPointerDown }) => {
  const isVertical = direction === 'vertical';

  return (
    <div
      className={`resize-handle resize-handle--${direction}${isDragging ? ' resize-handle--active' : ''}`}
      onPointerDown={onPointerDown}
      role="separator"
      aria-orientation={isVertical ? 'horizontal' : 'vertical'}
      style={{
        [isVertical ? 'height' : 'width']: 8,
        [isVertical ? 'width' : 'height']: '100%',
        cursor: isVertical ? 'row-resize' : 'col-resize',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        flexShrink: 0,
        touchAction: 'none',
      }}
    >
      {/* Grip dots */}
      <div className="resize-handle__grip">
        <span />
        <span />
        <span />
      </div>
    </div>
  );
};

export default ResizeHandle;
