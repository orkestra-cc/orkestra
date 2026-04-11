import { useState } from 'react';
import { Col, Row, Tab, Tabs } from 'react-bootstrap';
import ModuleTable from './ModuleTable';

const ModuleManagementPage: React.FC = () => {
  const [tab, setTab] = useState<'core' | 'addons'>('core');

  return (
    <Row className="g-3">
      <Col xxl={12}>
        <Tabs
          id="module-management-tabs"
          activeKey={tab}
          onSelect={(k) => setTab((k as 'core' | 'addons') || 'core')}
          className="mb-3"
        >
          <Tab eventKey="core" title="Core Modules">
            <ModuleTable scope="core" title="Core Modules" />
          </Tab>
          <Tab eventKey="addons" title="Addons">
            <ModuleTable scope="addons" title="Addons" />
          </Tab>
        </Tabs>
      </Col>
    </Row>
  );
};

export default ModuleManagementPage;
