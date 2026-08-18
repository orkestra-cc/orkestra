import { Card, Col, Row } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import UserTable from './UserTable';

const UserManagementPage: React.FC = () => {
  const { t } = useTranslation();
  return (
    <>
      <Row className="g-3 mb-3">
        <Col xxl={12}>
          <Card>
            <Card.Body className="py-3 px-4">
              {/* h3 = the level PageHeader renders — same header genre as
                  /admin/modules, so sibling admin surfaces read as one page
                  family. */}
              <h3 className="mb-0">{t('adminUsers.pageTitle')}</h3>
            </Card.Body>
          </Card>
        </Col>
      </Row>
      <Row className="g-3">
        <Col xxl={12}>
          <UserTable />
        </Col>
      </Row>
    </>
  );
};

export default UserManagementPage;
