import React, { useState } from 'react';
import { Link } from 'react-router';
import paths from 'routes/paths';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Badge, Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { useGetCranesQuery, useUpdateCraneMutation, CraneResponse } from 'store/api/craneApi';
import FalconCloseButton from 'components/common/FalconCloseButton';
import { GiCrane } from 'react-icons/gi';

// Confirmation Modal Component
interface CraneActivationModalProps {
  show: boolean;
  onHide: () => void;
  crane: CraneResponse | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const CraneActivationModal: React.FC<CraneActivationModalProps> = ({ show, onHide, crane, onConfirm, isLoading }) => {
  if (!crane) return null;

  const isActivating = !crane.isActive;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>
          {isActivating ? 'Activate Crane' : 'Deactivate Crane'}
        </Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Are you sure you want to {isActivating ? 'activate' : 'deactivate'} the crane{' '}
          <strong>{crane.nome}</strong> (Serial: {crane.matricola})?
        </p>
        {!isActivating && (
          <p className="text-warning mb-0">
            The crane will no longer be available for assignment until it is reactivated.
          </p>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="secondary" onClick={onHide} disabled={isLoading}>
          Cancel
        </Button>
        <Button
          variant={isActivating ? 'success' : 'warning'}
          onClick={onConfirm}
          disabled={isLoading}
        >
          {isLoading ? 'Please wait...' : isActivating ? 'Activate' : 'Deactivate'}
        </Button>
      </Modal.Footer>
    </Modal>
  );
};

const useCraneTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedCrane, setSelectedCrane] = useState<CraneResponse | null>(null);
  const [updateCrane, { isLoading: isUpdating }] = useUpdateCraneMutation();

  // Fetch cranes from backend API
  const {
    data: cranesResponse,
    isLoading,
    error
  } = useGetCranesQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const cranes = cranesResponse?.cranes || [];

  // Handle activation/deactivation
  const handleToggleActivation = (crane: CraneResponse) => {
    setSelectedCrane(crane);
    setShowModal(true);
  };

  const handleConfirmToggle = async () => {
    if (!selectedCrane) return;

    try {
      await updateCrane({
        id: selectedCrane.id,
        data: { isActive: !selectedCrane.isActive }
      }).unwrap();
      setShowModal(false);
      setSelectedCrane(null);
    } catch (error) {
      console.error('Failed to update crane:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedCrane(null);
  };

  // Check if verification is expiring soon (within 30 days)
  const isVerificationExpiring = (date?: string) => {
    if (!date) return false;
    const verificationDate = new Date(date);
    const today = new Date();
    const daysDiff = Math.ceil((verificationDate.getTime() - today.getTime()) / (1000 * 60 * 60 * 24));
    return daysDiff <= 30 && daysDiff >= 0;
  };

  // Check if verification is expired
  const isVerificationExpired = (date?: string) => {
    if (!date) return false;
    const verificationDate = new Date(date);
    const today = new Date();
    return verificationDate < today;
  };

  const columns = [
    // Hidden searchable columns for better search
    {
      accessorKey: 'matricola',
      header: '',
      meta: {
        headerProps: { style: { display: 'none' } },
        cellProps: { style: { display: 'none' } }
      },
      enableGlobalFilter: true
    },
    {
      accessorKey: 'nome',
      header: 'Crane',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
        const { nome, matricola } = original;
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div className="avatar-2xl rounded-circle bg-soft-warning d-flex align-items-center justify-content-center me-2">
              <GiCrane className="text-warning" size={24} />
            </div>
            <div>
              <h6 className="mb-0">
                <Link
                  to={paths.fleetCraneProfile?.replace(':craneId', original.id) || '#'}
                  className="stretched-link text-900"
                >
                  {nome}
                </Link>
              </h6>
              <small className="text-muted">Serial: {matricola}</small>
            </div>
          </Flex>
        );
      }
    },
    {
      accessorKey: 'tipo',
      header: 'Type',
      meta: {
        headerProps: {
          className: 'text-900'
        },
        cellProps: {
          className: 'py-2 pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
        const { tipo } = original;
        return (
          <Badge bg="warning" className="text-dark">
            {tipo}
          </Badge>
        );
      }
    },
    {
      accessorKey: 'verificareSuMezzo',
      header: 'Associated Vehicle',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
        const { verificareSuMezzo, vehicleId } = original;

        if (!verificareSuMezzo) {
          return <span className="text-muted">-</span>;
        }

        if (vehicleId) {
          return (
            <Link to={paths.fleetVehicleProfile?.replace(':vehicleId', vehicleId) || '#'}>
              <Badge bg="info">{verificareSuMezzo}</Badge>
            </Link>
          );
        }

        return <Badge bg="info">{verificareSuMezzo}</Badge>;
      }
    },
    {
      accessorKey: 'isActive',
      header: 'Status',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'fs-9 pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
        const { isActive } = original;
        return (
          <SubtleBadge bg={isActive ? 'success' : 'secondary'} className="me-2">
            {isActive ? 'Active' : 'Inactive'}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'scadenzaVerifica',
      header: 'Verification Expiry',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
        const { scadenzaVerifica } = original;
        if (!scadenzaVerifica) {
          return <span className="text-muted">-</span>;
        }
        const date = new Date(scadenzaVerifica);
        const formattedDate = date.toLocaleDateString('en-GB', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        });
        const isExpiring = isVerificationExpiring(scadenzaVerifica);
        const isExpired = isVerificationExpired(scadenzaVerifica);

        return (
          <div>
            <div className={isExpired ? 'text-danger fw-bold' : isExpiring ? 'text-warning fw-bold' : 'text-900'}>
              {formattedDate}
              {(isExpiring || isExpired) && (
                <FontAwesomeIcon icon="exclamation-triangle" className="ms-1" />
              )}
            </div>
            {isExpired && <small className="text-danger">Expired</small>}
            {!isExpired && isExpiring && <small className="text-warning">Expiring soon</small>}
          </div>
        );
      }
    },
    {
      accessorKey: 'actions',
      header: 'Actions',
      meta: {
        headerProps: { className: 'text-end text-900' }
      },
      cell: ({ row: { original } }: { row: { original: CraneResponse } }) => {
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
                  to={paths.fleetCraneProfile?.replace(':craneId', original.id) || '#'}
                >
                  View Details
                </Dropdown.Item>
                <Dropdown.Item>Edit Crane</Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item
                  className="text-warning"
                  onClick={() => handleToggleActivation(original)}
                >
                  {original.isActive ? 'Deactivate' : 'Activate'}
                </Dropdown.Item>
                <Dropdown.Item className="text-danger">
                  Delete Crane
                </Dropdown.Item>
              </div>
            </Dropdown.Menu>
          </Dropdown>
        );
      }
    }
  ];

  const table = useAdvanceTable({
    columns,
    data: cranes,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    ActivationModal: () => (
      <CraneActivationModal
        show={showModal}
        onHide={handleCloseModal}
        crane={selectedCrane}
        onConfirm={handleConfirmToggle}
        isLoading={isUpdating}
      />
    )
  };
};

export default useCraneTable;