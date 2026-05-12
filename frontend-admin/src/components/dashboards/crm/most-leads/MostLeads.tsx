import CardDropdown from 'components/common/CardDropdown';
import Flex from 'components/common/Flex';
import { Card, Col, Row } from 'react-bootstrap';
import MostLeadsChart from './MostLeadsChart';
import { leadsData } from 'data/dashboard/crm';
import OrkestraCardFooterLink from 'components/common/OrkestraCardFooterLink';
import classNames from 'classnames';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';

interface LeadItemData {
  id: string | number;
  img: string;
  title: string;
  target: string;
  amount: number;
}

interface LeadItemProps {
  item: LeadItemData;
  isLast: boolean;
}

const LeadItem = ({ item, isLast }: LeadItemProps) => {
  return (
    <Flex
      justifyContent="between"
      alignItems="center"
      className={classNames('border-bottom py-3', {
        'border-bottom-0 pb-0': isLast
      })}
    >
      <Flex>
        <img src={item.img} alt="..." width={16} height={16} className="me-2" />
        <h6 className="text-700 mb-0">{item.title}</h6>
      </Flex>
      <p className="fs-10 text-500 mb-0 fw-semibold">{item.target}</p>
      <h6 className="text-700 mb-0">{item.amount}%</h6>
    </Flex>
  );
};

const MostLeads = () => {
  return (
    <Card className="h-100">
      <OrkestraCardHeader
        title="Most Leads"
        titleTag="h6"
        className="border-200 border-bottom py-2"
        endEl={<CardDropdown />}
      />
      <Card.Body as={Row}>
        <Col md={5} xxl={12} className="mb-xxl-1">
          <MostLeadsChart />
        </Col>
        <Col xxl={12} md={7}>
          <hr className="mx-ncard mb-0 d-md-none d-xxl-block" />
          {(leadsData as LeadItemData[]).map(
            (item: LeadItemData, index: number) => (
              <LeadItem
                key={item.id}
                item={item}
                isLast={index === leadsData.length - 1}
              />
            )
          )}
        </Col>
      </Card.Body>
      <OrkestraCardFooterLink title="View all" size="sm" to="#!" />
    </Card>
  );
};

export default MostLeads;
