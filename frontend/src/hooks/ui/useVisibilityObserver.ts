import { useState, useEffect, RefObject } from 'react';

const useVisibilityObserver = (
  element: RefObject<HTMLElement>,
  rootMargin: string = '0px'
) => {
  const [isVisible, setState] = useState(false);
  const [observer, setObserver] = useState<IntersectionObserver | null>(null);

  useEffect(() => {
    const intersectionObserver = new IntersectionObserver(
      ([entry]) => {
        setState(entry.isIntersecting);
      },
      { rootMargin }
    );

    setObserver(intersectionObserver);

    element.current && intersectionObserver.observe(element.current);

    return () => {
      intersectionObserver.disconnect();
    };
  }, [element, rootMargin]);

  return { isVisible, observer };
};

export default useVisibilityObserver;
