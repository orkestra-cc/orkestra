import { useState } from 'react';
import { Card, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import AdvanceTableProvider from 'providers/AdvanceTableProvider';
import AdvanceTable from 'components/common/advance-table/AdvanceTable';
import AdvanceTableFooter from 'components/common/advance-table/AdvanceTableFooter';
import useCompanyTable from 'hooks/ui/useCompanyTable';
import CompanyModal from './CompanyModal';
import type { Company } from 'types/billing';

const CompanyTable = () => {
  const [showModal, setShowModal] = useState(false);
  const [editingCompany, setEditingCompany] = useState<Company | null>(null);

  const handleEdit = (company: Company) => {
    setEditingCompany(company);
    setShowModal(true);
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setEditingCompany(null);
  };

  const { DeactivationModal, ...table } = useCompanyTable({
    selection: false,
    sortable: true,
    pagination: true,
    perPage: 10,
    selectionColumnWidth: 52,
    onEdit: handleEdit
  });

  return (
    <>
      <AdvanceTableProvider {...table}>
        <Card>
          <Card.Header className="border-bottom border-200 d-flex justify-content-between align-items-center">
            <h5 className="mb-0">Aziende Emittenti</h5>
            <Button
              variant="primary"
              size="sm"
              onClick={() => setShowModal(true)}
            >
              <FontAwesomeIcon icon="plus" className="me-1" />
              Nuova Azienda
            </Button>
          </Card.Header>
          <Card.Body className="p-0">
            <AdvanceTable
              headerClassName="bg-body-tertiary align-middle"
              bodyClassName=""
              rowClassName="btn-reveal-trigger align-middle"
              tableProps={{
                size: 'sm',
                className: 'fs-10 mb-0 overflow-hidden'
              }}
            />
          </Card.Body>
          <Card.Footer>
            <AdvanceTableFooter
              viewAllBtn={false}
              navButtons={true}
              className=""
              rowInfo={true}
              rowsPerPageSelection={true}
            />
          </Card.Footer>
        </Card>
      </AdvanceTableProvider>
      <DeactivationModal />
      <CompanyModal
        show={showModal}
        onHide={handleCloseModal}
        company={editingCompany}
        onSuccess={handleCloseModal}
      />
    </>
  );
};

export default CompanyTable;
