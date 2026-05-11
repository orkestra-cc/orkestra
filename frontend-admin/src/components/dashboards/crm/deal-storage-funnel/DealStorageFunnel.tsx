
import { Card } from 'react-bootstrap';
import FalconCardHeader from 'components/common/FalconCardHeader';
import DealStorageFunnelChart from './DealStorageFunnelChart';
import Flex from 'components/common/Flex';
import FalconLink from 'components/common/FalconLink';

const DealStorageFunnel = () => {
  return (
    <Card>
      <FalconCardHeader
        title="Deal Storage Funnel"
        titleTag="h6"
        className="border-200 border-bottom py-2"
        endEl={<FalconLink title="View Details" className="px-0" />}
      />
      <Card.Body dir="ltr">
        <Flex
          justifyContent="between"
          alignItems="center"
          className="rtl-row-reverse"
        >
          <h6 className="fs-11 text-500 flex-1">Deal Stage</h6>
          <h6 className="fs-11 text-500 mx-2">Count of Deals </h6>
          <h6 className="fs-11 text-500">Conversion</h6>
        </Flex>
        <DealStorageFunnelChart />
      </Card.Body>
    </Card>
  );
};

export default DealStorageFunnel;
