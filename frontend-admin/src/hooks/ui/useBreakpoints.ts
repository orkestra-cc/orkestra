import { useState, useEffect } from 'react';

type BreakpointKey = 'xs' | 'sm' | 'md' | 'lg' | 'xl' | 'xxl';

interface GridBreakpoints {
  xs: number;
  sm: number;
  md: number;
  lg: number;
  xl: number;
  xxl: number;
}

interface Breakpoints {
  up: (bp: BreakpointKey) => boolean;
  down: (bp: BreakpointKey) => boolean;
}

export const useBreakpoints = () => {
  const gridBreakpoints: GridBreakpoints = {
    xs: 0,
    sm: 576,
    md: 768,
    lg: 992,
    xl: 1200,
    xxl: 1540
  };
  const [width, setWidth] = useState(window.innerWidth);
  const [height, setHeight] = useState(window.innerHeight);

  const [breakpoints, setBreakpoints] = useState<Breakpoints>({
    up: (bp: BreakpointKey) => {
      return width > gridBreakpoints[bp];
    },
    down: (bp: BreakpointKey) => {
      return width < gridBreakpoints[bp];
    }
  });
  const updateDimensions = () => {
    setWidth(window.innerWidth);
    setHeight(window.innerHeight);
  };

  useEffect(() => {
    window.addEventListener('resize', updateDimensions, { passive: true });
    return () => window.removeEventListener('resize', updateDimensions);
  }, []);

  useEffect(() => {
    setBreakpoints({
      up: (bp: BreakpointKey) => width >= gridBreakpoints[bp],
      down: (bp: BreakpointKey) => width < gridBreakpoints[bp]
    });
  }, [width]);

  return { width, height, breakpoints };
};
