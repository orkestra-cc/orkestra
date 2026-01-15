import React, { useState } from 'react';
import { Link } from 'react-router';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  useGetCustomersQuery,
  useDeleteCustomerMutation,
} from 'store/api/billingApi';
import type { Customer } from 'types/billing';
import { getPartyDisplayName } from 'types/billing';
import FalconCloseButton from 'components/common/FalconCloseButton';

// Deactivation Modal Component
interface CustomerDeactivationModalProps {
  show: boolean;
  onHide: () => void;
  customer: Customer | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const CustomerDeactivationModal: React.FC<CustomerDeactivationModalProps> = ({
  show,
  onHide,
  customer,
  onConfirm,
  isLoading
}) => {
  if (!customer) return null;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>Disattiva Cliente</Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Sei sicuro di voler disattivare il cliente{' '}
          <strong>{getPartyDisplayName(customer)}</strong>?
        </p>
        <p className="text-warning mb-0">
          Il cliente non sarà più disponibile per nuove fatture.
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

const useCustomerTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedCustomer, setSelectedCustomer] = useState<Customer | null>(null);
  const [deleteCustomer, { isLoading: isDeleting }] = useDeleteCustomerMutation();

  // Fetch customers from backend API
  const {
    data: customersResponse,
    isLoading,
    error
  } = useGetCustomersQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const customers = customersResponse?.customers || [];

  // Handle deactivation
  const handleDeactivate = (customer: Customer) => {
    setSelectedCustomer(customer);
    setShowModal(true);
  };

  const handleConfirmDeactivate = async () => {
    if (!selectedCustomer) return;

    try {
      await deleteCustomer(selectedCustomer.id).unwrap();
      setShowModal(false);
      setSelectedCustomer(null);
    } catch (error) {
      console.error('Failed to deactivate customer:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedCustomer(null);
  };

  const columns = [
    {
      accessorKey: 'denomination',
      header: 'Cliente',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
        const displayName = getPartyDisplayName(original);
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div>
              <h6 className="mb-0">
                <Link
                  to={`/billing/customers/${original.id}`}
                  className="stretched-link text-900"
                >
                  {displayName}
                </Link>
              </h6>
              <small className="text-muted">
                {original.isCompany ? 'Azienda' : 'Persona fisica'}
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
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
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
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
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
      accessorKey: 'codiceDestinatario',
      header: 'Codice SDI',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
        if (original.codiceDestinatario) {
          return (
            <span className="font-monospace text-900">
              {original.codiceDestinatario}
            </span>
          );
        }
        if (original.pecDestinatario) {
          return (
            <span className="text-muted" title={original.pecDestinatario}>
              PEC
            </span>
          );
        }
        return <span className="text-muted">-</span>;
      }
    },
    {
      accessorKey: 'isPA',
      header: 'Tipo',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
        if (original.isPA) {
          return <SubtleBadge bg="info">P.A.</SubtleBadge>;
        }
        return <SubtleBadge bg="secondary">Privato</SubtleBadge>;
      }
    },
    {
      accessorKey: 'isActive',
      header: 'Stato',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'fs-9 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
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
      cell: ({ row: { original } }: { row: { original: Customer } }) => {
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
                <Dropdown.Item
                  as={Link}
                  to={`/billing/customers/${original.id}`}
                >
                  Visualizza
                </Dropdown.Item>
                <Dropdown.Item
                  as={Link}
                  to={`/billing/customers/${original.id}/edit`}
                >
                  Modifica
                </Dropdown.Item>
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
    data: customers,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    DeactivationModal: () => (
      <CustomerDeactivationModal
        show={showModal}
        onHide={handleCloseModal}
        customer={selectedCustomer}
        onConfirm={handleConfirmDeactivate}
        isLoading={isDeleting}
      />
    )
  };
};

export default useCustomerTable;
