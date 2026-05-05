import { useEffect, useState } from 'react';

const useToggleStylesheet = (isRTL: boolean, isDark: boolean) => {
  const [isLoaded, setIsLoaded] = useState(false);
  const publicUrl = import.meta.env.VITE_PUBLIC_URL;

  useEffect(() => {
    setIsLoaded(false);
    Array.from(document.getElementsByClassName('theme-stylesheet')).forEach(
      link => link.remove()
    );
    const link = document.createElement('link');
    link.href = `${publicUrl}css/theme${isRTL ? '.rtl' : ''}.css`;
    link.type = 'text/css';
    link.rel = 'stylesheet';
    link.className = 'theme-stylesheet';

    const userLink = document.createElement('link');
    userLink.href = `${publicUrl}css/user${isRTL ? '.rtl' : ''}.css`;
    userLink.type = 'text/css';
    userLink.rel = 'stylesheet';
    userLink.className = 'theme-stylesheet';

    link.onload = () => {
      setIsLoaded(true);
    };

    document.getElementsByTagName('head')[0].appendChild(link);
    document.getElementsByTagName('head')[0].appendChild(userLink);
    document
      .getElementsByTagName('html')[0]
      .setAttribute('dir', isRTL ? 'rtl' : 'ltr');
  }, [isRTL]);

  useEffect(() => {
    document.documentElement.setAttribute(
      'data-bs-theme',
      isDark ? 'dark' : 'light'
    );
  }, []);

  return { isLoaded };
};

export default useToggleStylesheet;
