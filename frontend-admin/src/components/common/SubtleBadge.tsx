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
  /** Optional test hook — forwarded verbatim to the rendered element. */
  'data-testid'?: string;
}

const SubtleBadge: React.FC<SubtleBadgeProps> = ({
  bg = 'primary',
  pill,
  children,
  className,
  title,
  'data-testid': dataTestId
}) => {
  return (
    <div
      className={classNames(className, `badge badge-subtle-${bg}`, {
        'rounded-pill': pill
      })}
      title={title}
      data-testid={dataTestId}
    >
      {children}
    </div>
  );
};

export default SubtleBadge;
