import classNames from 'classnames';

export type BadgeColor =
  | 'primary'
  | 'secondary'
  | 'success'
  | 'danger'
  | 'warning'
  | 'info'
  | 'light'
  | 'dark';

interface SubtleBadgeProps {
  bg?: BadgeColor;
  pill?: boolean;
  children?: React.ReactNode;
  className?: string;
  title?: string;
}

const SubtleBadge: React.FC<SubtleBadgeProps> = ({
  bg = 'primary',
  pill,
  children,
  className,
  title
}) => {
  return (
    <div
      className={classNames(className, `badge badge-subtle-${bg}`, {
        'rounded-pill': pill
      })}
      title={title}
    >
      {children}
    </div>
  );
};

export default SubtleBadge;
