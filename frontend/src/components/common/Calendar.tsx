


interface CalendarProps {
  month: string;
  day: string | number;
}

const Calendar: React.FC<CalendarProps> = ({ month, day }) => (
  <div className="calendar">
    <span className="calendar-month">{month}</span>
    <span className="calendar-day">{day}</span>
  </div>
);

export default Calendar;
