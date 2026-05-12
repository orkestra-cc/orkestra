import { RefObject, useEffect, useState } from 'react';

interface UseScrollSpyOptions {
  sectionElementRefs: RefObject<HTMLElement | null>[];
  offsetPx?: number;
}

// Local replacement for the abandoned `react-use-scrollspy` package, which
// transitively pinned a vulnerable lodash with no upstream remediation.
// API mirrors the v3 signature so the three Orkestra-reference call sites stay
// untouched in shape.
export default function useScrollSpy({
  sectionElementRefs,
  offsetPx = 0
}: UseScrollSpyOptions): number {
  const [activeIndex, setActiveIndex] = useState(0);

  useEffect(() => {
    const handler = () => {
      const scrollY = window.scrollY + offsetPx;
      let current = 0;
      for (let i = 0; i < sectionElementRefs.length; i++) {
        const el = sectionElementRefs[i].current;
        if (!el) continue;
        if (el.offsetTop <= scrollY) current = i;
      }
      setActiveIndex(current);
    };
    handler();
    window.addEventListener('scroll', handler, { passive: true });
    window.addEventListener('resize', handler);
    return () => {
      window.removeEventListener('scroll', handler);
      window.removeEventListener('resize', handler);
    };
  }, [sectionElementRefs, offsetPx]);

  return activeIndex;
}
