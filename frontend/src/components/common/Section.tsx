
import classNames from 'classnames';
import Background from './Background';
import { Container } from 'react-bootstrap';

interface BackgroundPosition {
  x?: string;
  y?: string;
}

interface SectionProps extends React.HTMLAttributes<HTMLElement> {
  fluid?: boolean;
  bg?: string;
  image?: string;
  overlay?: boolean | string;
  position?: string | BackgroundPosition;
  video?: string[];
  bgClassName?: string;
  className?: string;
  children?: React.ReactNode;
}

const Section: React.FC<SectionProps> = ({
  fluid = false,
  bg,
  image,
  overlay,
  position,
  video,
  bgClassName,
  className,
  children,
  ...rest
}) => {
  const bgProps: any = { image, overlay, position, video };
  bgClassName && (bgProps.className = bgClassName);

  return (
    <section className={classNames({ [`bg-${bg}`]: bg }, className)} {...rest}>
      {image && <Background {...bgProps} />}
      <Container fluid={fluid}>{children}</Container>
    </section>
  );
};

export default Section;
