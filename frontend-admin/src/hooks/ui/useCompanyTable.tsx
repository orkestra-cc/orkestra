import React, { useState } from 'react';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  useGetCompaniesQuery,
  useDeleteCompanyMutation,
  useSetDefaultCompanyMutation,
} from 'store/api/billingApi';
import type { Company } from 'types/billing';
import { REGIME_FISCALE_LABELS } from 'types/billing';
import FalconCloseButton from 'components/common/FalconCloseButton';

// Deactivation Modal Component
interface CompanyDeactivationModalProps {
  show: boolean;
  onHide: () => void;
  company: Company | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const CompanyDeactivationModal: React.FC<CompanyDeactivationModalProps> = ({
  show,
  onHide,
  company,
  onConfirm,
  isLoading
}) => {
  if (!company) return null;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>Disattiva Azienda</Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Sei sicuro di voler disattivare l'azienda{' '}
          <strong>{company.denomination}</strong>?
        </p>
        <p className="text-warning mb-0">
          L'azienda non sarà più disponibile per nuove fatture.
          {company.isDefault && " Questa è l'azienda default - dopo l'eliminazione dovrai impostarne un'altra."}
        </p>
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Annulla
        </Button>
        <Button variant="warning" onClick={onConfirm} disabled={isLoading}>
          {isLoading ? 'Attendere...' : 'Disattiva'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

interface UseCompanyTableOptions {
  selection?: boolean;
  sortable?: boolean;
  pagination?: boolean;
  perPage?: number;
  selectionColumnWidth?: number;
  onEdit?: (company: Company) => void;
}

const useCompanyTable = (options?: UseCompanyTableOptions) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedCompany, setSelectedCompany] = useState<Company | null>(null);
  const [deleteCompany, { isLoading: isDeleting }] = useDeleteCompanyMutation();
  const [setDefaultCompany, { isLoading: isSettingDefault }] = useSetDefaultCompanyMutation();

  // Fetch companies from backend API
  const { data: companiesResponse } = useGetCompaniesQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const companies = companiesResponse?.companies || [];

  // Handle deactivation
  const handleDeactivate = (company: Company) => {
    setSelectedCompany(company);
    setShowModal(true);
  };

  const handleConfirmDeactivate = async () => {
    if (!selectedCompany) return;

    try {
      await deleteCompany(selectedCompany.id).unwrap();
      setShowModal(false);
      setSelectedCompany(null);
    } catch (error) {
      console.error('Failed to deactivate company:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedCompany(null);
  };

  const handleSetDefault = async (company: Company) => {
    try {
      await setDefaultCompany(company.id).unwrap();
    } catch (error) {
      console.error('Failed to set default company:', error);
    }
  };

  const columns = [
    {
      accessorKey: 'denomination',
      header: 'Ragione Sociale',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div>
              <h6 className="mb-0 text-900">
                {original.denomination}
                {original.isDefault && (
                  <SubtleBadge bg="primary" className="ms-2">
                    Default
                  </SubtleBadge>
                )}
              </h6>
              <small className="text-muted">
                {REGIME_FISCALE_LABELS[original.regimeFiscale] || original.regimeFiscale}
              </small>
            </div>
          </Flex>
        );
      }
    },
    {
      accessorKey: 'fiscalIdCode',
      header: 'P.IVA / CF',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        return (
          <div>
            <div className="text-900 font-monospace">{original.fiscalIdCode}</div>
            {original.codiceFiscale && original.codiceFiscale !== original.fiscalIdCode && (
              <small className="text-muted font-monospace">{original.codiceFiscale}</small>
            )}
          </div>
        );
      }
    },
    {
      accessorKey: 'city',
      header: 'Sede',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        return (
          <div>
            <div className="text-900">{original.city}</div>
            {original.province && (
              <small className="text-muted">({original.province})</small>
            )}
          </div>
        );
      }
    },
    {
      accessorKey: 'rea',
      header: 'REA',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        if (original.reaOffice && original.reaNumber) {
          return (
            <span className="font-monospace text-900">
              {original.reaOffice}-{original.reaNumber}
            </span>
          );
        }
        return <span className="text-muted">-</span>;
      }
    },
    {
      accessorKey: 'isActive',
      header: 'Stato',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'fs-9 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        const { isActive } = original;
        return (
          <SubtleBadge bg={isActive ? 'success' : 'secondary'} className="me-2">
            {isActive ? 'Attivo' : 'Inattivo'}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'actions',
      header: 'Azioni',
      meta: {
        headerProps: { className: 'text-end text-900' }
      },
      cell: ({ row: { original } }: { row: { original: Company } }) => {
        return (
          <Dropdown align="end" className="btn-reveal-trigger">
            <Dropdown.Toggle
              variant="link"
              size="sm"
              className="text-600 btn-reveal"
            >
              <FontAwesomeIcon icon="ellipsis-h" className="fs-11" />
            </Dropdown.Toggle>

            <Dropdown.Menu className="border py-0">
              <div className="py-2">
                {options?.onEdit && (
                  <Dropdown.Item
                    onClick={() => options.onEdit?.(original)}
                  >
                    Modifica
                  </Dropdown.Item>
                )}
                {!original.isDefault && original.isActive && (
                  <Dropdown.Item
                    onClick={() => handleSetDefault(original)}
                    disabled={isSettingDefault}
                  >
                    Imposta come Default
                  </Dropdown.Item>
                )}
                <Dropdown.Divider />
                {original.isActive && (
                  <Dropdown.Item
                    className="text-warning"
                    onClick={() => handleDeactivate(original)}
                  >
                    Disattiva
                  </Dropdown.Item>
                )}
              </div>
            </Dropdown.Menu>
          </Dropdown>
        );
      }
    }
  ];

  const table = useAdvanceTable({
    columns,
    data: companies,
    ...options
  });

  return {
    ...table,
    DeactivationModal: () => (
      <CompanyDeactivationModal
        show={showModal}
        onHide={handleCloseModal}
        company={selectedCompany}
        onConfirm={handleConfirmDeactivate}
        isLoading={isDeleting}
      />
    )
  };
};

export default useCompanyTable;
