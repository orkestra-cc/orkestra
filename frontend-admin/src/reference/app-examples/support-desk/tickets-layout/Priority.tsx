interface PriorityProps {
  title: string;
  color: string;
  data: string;
}

const Priority = ({ title, color, data }: PriorityProps) => {
  return (
    <div
      style={{ width: '7.5rem' }}
      className="d-flex align-items-center gap-2 ms-md-4 ms-xl-0"
    >
      <div style={{ '--falcon-circle-progress-bar': data } as React.CSSProperties}>
        <svg
          className="circle-progress-svg"
          width="26"
          height="26"
          viewBox="0 0 120 120"
        >
          <circle
            className="progress-bar-rail"
            cx="60"
            cy="60"
            r="54"
            fill="none"
            strokeLinecap="round"
            strokeWidth="12"
          ></circle>
          <circle
            className="progress-bar-top"
            cx="60"
            cy="60"
            r="54"
            fill="none"
            strokeLinecap="round"
            stroke={color}
            strokeWidth="12"
          ></circle>
        </svg>
      </div>
      <h6 className="mb-0 text-700">{title}</h6>
    </div>
  );
};

export default Priority;
