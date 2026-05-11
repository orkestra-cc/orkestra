import { useCallback, useEffect, useRef, useState } from 'react';

type Direction = 'vertical' | 'horizontal';

interface UseResizableOptions {
  direction: Direction;
  initialSize: number;
  minSize: number;
  maxSize: number;
  storageKey?: string;
}

interface UseResizableReturn {
  size: number;
  isDragging: boolean;
  handleProps: {
    onPointerDown: (e: React.PointerEvent) => void;
  };
}

export function useResizable({
  direction,
  initialSize,
  minSize,
  maxSize,
  storageKey,
}: UseResizableOptions): UseResizableReturn {
  const [size, setSize] = useState(() => {
    if (storageKey) {
      const stored = localStorage.getItem(storageKey);
      if (stored) {
        const parsed = Number(stored);
        if (!isNaN(parsed) && parsed >= minSize && parsed <= maxSize) return parsed;
      }
    }
    return initialSize;
  });

  const [isDragging, setIsDragging] = useState(false);
  const startPos = useRef(0);
  const startSize = useRef(0);
  const rafId = useRef(0);

  // Persist to localStorage on drag end
  useEffect(() => {
    if (!isDragging && storageKey) {
      localStorage.setItem(storageKey, String(size));
    }
  }, [isDragging, size, storageKey]);

  // Set body cursor during drag
  useEffect(() => {
    if (isDragging) {
      const cursor = direction === 'vertical' ? 'row-resize' : 'col-resize';
      document.body.style.cursor = cursor;
      document.body.style.userSelect = 'none';
      return () => {
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
      };
    }
  }, [isDragging, direction]);

  const handlePointerMove = useCallback(
    (e: PointerEvent) => {
      cancelAnimationFrame(rafId.current);
      rafId.current = requestAnimationFrame(() => {
        const delta =
          direction === 'vertical'
            ? e.clientY - startPos.current
            : e.clientX - startPos.current;
        const newSize = Math.min(maxSize, Math.max(minSize, startSize.current + delta));
        setSize(newSize);
      });
    },
    [direction, minSize, maxSize]
  );

  const handlePointerUp = useCallback(() => {
    cancelAnimationFrame(rafId.current);
    setIsDragging(false);
    document.removeEventListener('pointermove', handlePointerMove);
    document.removeEventListener('pointerup', handlePointerUp);
  }, [handlePointerMove]);

  const handlePointerDown = useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault();
      startPos.current = direction === 'vertical' ? e.clientY : e.clientX;
      startSize.current = size;
      setIsDragging(true);
      document.addEventListener('pointermove', handlePointerMove);
      document.addEventListener('pointerup', handlePointerUp);
    },
    [direction, size, handlePointerMove, handlePointerUp]
  );

  return {
    size,
    isDragging,
    handleProps: {
      onPointerDown: handlePointerDown,
    },
  };
}
