import React, { useState } from 'react';
import { Link } from 'react-router';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  useGetSuppliersQuery,
  useDeleteSupplierMutation
} from 'store/api/billingApi';
import type { Supplier } from 'types/billing';
import { getPartyDisplayName, REGIME_FISCALE_LABELS } from 'types/billing';
import OrkestraCloseButton from 'components/common/OrkestraCloseButton';

// Deactivation Modal Component
interface SupplierDeactivationModalProps {
  show: boolean;
  onHide: () => void;
  supplier: Supplier | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const SupplierDeactivationModal: React.FC<SupplierDeactivationModalProps> = ({
  show,
  onHide,
  supplier,
  onConfirm,
  isLoading
}) => {
  if (!supplier) return null;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>Disattiva Fornitore</Modal.Title>
        <OrkestraCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Sei sicuro di voler disattivare il fornitore{' '}
          <strong>{getPartyDisplayName(supplier)}</strong>?
        </p>
        <p className="text-warning mb-0">
          Il fornitore non sarà più visualizzato nelle liste di selezione.
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

const useSupplierTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedSupplier, setSelectedSupplier] = useState<Supplier | null>(
    null
  );
  const [deleteSupplier, { isLoading: isDeleting }] =
    useDeleteSupplierMutation();

  // Fetch suppliers from backend API
  const {
    data: suppliersResponse,
    isLoading,
    error
  } = useGetSuppliersQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const suppliers = suppliersResponse?.suppliers || [];

  // Handle deactivation
  const handleDeactivate = (supplier: Supplier) => {
    setSelectedSupplier(supplier);
    setShowModal(true);
  };

  const handleConfirmDeactivate = async () => {
    if (!selectedSupplier) return;

    try {
      await deleteSupplier(selectedSupplier.id).unwrap();
      setShowModal(false);
      setSelectedSupplier(null);
    } catch (error) {
      console.error('Failed to deactivate supplier:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedSupplier(null);
  };

  const columns = [
    {
      accessorKey: 'denomination',
      header: 'Fornitore',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
        const displayName = getPartyDisplayName(original);
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div>
              <h6 className="mb-0">
                <Link
                  to={`/billing/suppliers/${original.id}`}
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
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
        return (
          <div>
            <div className="text-900 font-monospace">
              {original.fiscalIdCode}
            </div>
            {original.codiceFiscale &&
              original.codiceFiscale !== original.fiscalIdCode && (
                <small className="text-muted font-monospace">
                  {original.codiceFiscale}
                </small>
              )}
          </div>
        );
      }
    },
    {
      accessorKey: 'regimeFiscale',
      header: 'Regime',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
        if (!original.regimeFiscale) {
          return <span className="text-muted">-</span>;
        }
        return (
          <div>
            <span className="font-monospace">{original.regimeFiscale}</span>
            <small className="d-block text-muted">
              {REGIME_FISCALE_LABELS[original.regimeFiscale]}
            </small>
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
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
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
      accessorKey: 'iban',
      header: 'IBAN',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'py-2 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
        if (!original.iban) {
          return <span className="text-muted">-</span>;
        }
        // Show last 4 digits masked
        const masked = `****${original.iban.slice(-4)}`;
        return (
          <span className="font-monospace text-muted" title={original.iban}>
            {masked}
          </span>
        );
      }
    },
    {
      accessorKey: 'isActive',
      header: 'Stato',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: { className: 'fs-9 pe-4' }
      },
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
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
      cell: ({ row: { original } }: { row: { original: Supplier } }) => {
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
                  to={`/billing/suppliers/${original.id}`}
                >
                  Visualizza
                </Dropdown.Item>
                <Dropdown.Item
                  as={Link}
                  to={`/billing/suppliers/${original.id}/edit`}
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
    data: suppliers,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    DeactivationModal: () => (
      <SupplierDeactivationModal
        show={showModal}
        onHide={handleCloseModal}
        supplier={selectedSupplier}
        onConfirm={handleConfirmDeactivate}
        isLoading={isDeleting}
      />
    )
  };
};

export default useSupplierTable;
