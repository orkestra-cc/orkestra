
import SimpleBar from 'simplebar-react';
import { Table } from 'react-bootstrap';
import { Link } from 'react-router';
import classNames from 'classnames';

interface DealForecastItem {
  id: string | number;
  owner: string;
  qualifiedItem: number;
  appointment: number;
  contactSent: number;
  closedWon: number;
}

const getTotal = (data: DealForecastItem[], key: keyof Omit<DealForecastItem, 'id' | 'owner'>) =>
  data.reduce((acc: number, val: DealForecastItem) => acc + Number(val[key]), 0);

interface DealForeCastTableRowProps {
  item: DealForecastItem;
  isLast: boolean;
}

const DealForeCastTableRow = ({ item, isLast }: DealForeCastTableRowProps) => {
  return (
    <tr>
      <td
        className={classNames(
          'align-middle font-sans-serif fw-medium text-nowrap',
          {
            'border-bottom-0': isLast
          }
        )}
      >
        <Link to="#!">{item.owner}</Link>
      </td>
      <td
        className={classNames('align-middle text-center', {
          'border-bottom-0': isLast
        })}
      >
        {item.qualifiedItem}
      </td>
      <td
        className={classNames('align-middle text-center', {
          'border-bottom-0': isLast
        })}
      >
        {item.appointment}
      </td>
      <td
        className={classNames('align-middle text-center', {
          'border-bottom-0': isLast
        })}
      >
        {item.contactSent}
      </td>
      <td
        className={classNames('align-middle text-end', {
          'border-bottom-0': isLast
        })}
      >
        {item.closedWon}
      </td>
    </tr>
  );
};
interface DealForeCastTableProps {
  data: DealForecastItem[];
}

const DealForeCastTable = ({ data }: DealForeCastTableProps) => {
  return (
    <SimpleBar>
      <Table className="fs-10 mb-0">
        <thead className="bg-200">
          <tr>
            <th className="text-800 text-nowrap">Owner</th>
            <th className="text-800 text-nowrap text-center">
              Qualified to buy
            </th>
            <th className="text-800 text-nowrap text-center">Appointment</th>
            <th className="text-800 text-nowrap text-center">Contact sent</th>
            <th className="text-800 text-nowrap text-end">Closed won</th>
          </tr>
        </thead>
        <tbody>
          {data.map((item: DealForecastItem, index: number) => (
            <DealForeCastTableRow
              key={item.id}
              item={item}
              isLast={data.length - 1 === index}
            />
          ))}
        </tbody>
        <tfoot className="bg-body-tertiary">
          <tr className="fw-bold">
            <th className="text-700">Total</th>
            <th className="text-700 text-center">
              ${getTotal(data, 'qualifiedItem')}
            </th>
            <th className="text-700 text-center">
              ${getTotal(data, 'appointment')}
            </th>
            <th className="text-700 text-center">
              ${getTotal(data, 'contactSent')}
            </th>
            <th className="text-700 pe-x1 text-end">
              ${getTotal(data, 'closedWon')}
            </th>
          </tr>
        </tfoot>
      </Table>
    </SimpleBar>
  );
};

export default DealForeCastTable;
