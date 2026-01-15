import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

interface BrowserSessionData {
  icon: string;
  label: string;
  value: string | number;
  color: string;
  progress: boolean;
  progressValue: string;
}

interface TableRowProps {
  data: BrowserSessionData;
}

const TableRow = ({ data }: TableRowProps) => {
  const { icon, label, value, color, progress, progressValue } = data;
  return (
    <tr>
      <td className="py-3">
        <Flex alignItems="center">
          <img src={icon} alt={label} width={16} />
          <h6 className="text-600 mb-0 ms-2">{label}</h6>
        </Flex>
      </td>
      <td className="py-3">
        <Flex alignItems="center">
          <FontAwesomeIcon
            icon="circle"
            className={`text-${color} fs-11 me-2`}
          />
          <h6 className="text-700 fw-normal mb-0">{value}</h6>
        </Flex>
      </td>
      <td className="py-3">
        <Flex alignItems="center" justifyContent="end">
          <FontAwesomeIcon
            icon={progress ? 'caret-up' : 'caret-down'}
            className={progress ? 'text-success' : 'text-danger'}
          />
          <h6 className="fs-11 text-700 mb-0 ms-2">{progressValue}</h6>
        </Flex>
      </td>
    </tr>
  );
};

export default TableRow;
