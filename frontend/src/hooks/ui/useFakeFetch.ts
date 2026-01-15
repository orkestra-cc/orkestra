import { useEffect, useState } from 'react';

const useFakeFetch = <T = any>(resolvedData: T, waitingTime: number = 500) => {
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<T>(resolvedData);

  useEffect(() => {
    let isMounted = true;
    const timeoutId = setTimeout(() => {
      if (isMounted) {
        setData(resolvedData);
        setLoading(false);
      }
    }, waitingTime);

    return () => {
      isMounted = false;
      clearTimeout(timeoutId);
    };
  }, [resolvedData, waitingTime]);

  return { loading, setLoading, data, setData };
};

export default useFakeFetch;
