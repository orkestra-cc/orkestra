const windowHeight: number = window.innerHeight;

const handleNavbarTransparency = (): void => {
  const scrollTop: number = window.scrollY;
  let alpha: number = (scrollTop / windowHeight) * 2;
  alpha >= 1 && (alpha = 1);
  
  const navbarElements = document.getElementsByClassName('navbar-theme');
  if (navbarElements.length > 0) {
    const navbarElement = navbarElements[0] as HTMLElement;
    navbarElement.style.backgroundColor = `rgba(11, 23, 39, ${alpha})`;
  }
};

export default handleNavbarTransparency;
