import React, { useState } from 'react';
import { Link } from 'react-router';
import paths from 'routes/paths';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Badge, Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  useGetTachographsQuery,
  useUpdateTachographMutation,
  TachographResponse
} from 'store/api/tachographApi';
import FalconCloseButton from 'components/common/FalconCloseButton';

// Confirmation Modal Component
interface TachographActivationModalProps {
  show: boolean;
  onHide: () => void;
  tachograph: TachographResponse | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const TachographActivationModal: React.FC<TachographActivationModalProps> = ({
  show,
  onHide,
  tachograph,
  onConfirm,
  isLoading
}) => {
  if (!tachograph) return null;

  const isActivating = !tachograph.isActive;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>
          {isActivating ? 'Activate Tachograph' : 'Deactivate Tachograph'}
        </Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Are you sure you want to {isActivating ? 'activate' : 'deactivate'} the
          tachograph <strong>{tachograph.nome}</strong> (License Plate:{' '}
          {tachograph.targa})?
        </p>
        {!isActivating && (
          <p className="text-warning mb-0">
            The tachograph will no longer be available until it is
            reactivated.
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

const useTachographTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedTachograph, setSelectedTachograph] =
    useState<TachographResponse | null>(null);
  const [updateTachograph, { isLoading: isUpdating }] =
    useUpdateTachographMutation();

  // Fetch tachographs from backend API
  const {
    data: tachographsResponse,
    isLoading,
    error
  } = useGetTachographsQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const tachographs = tachographsResponse?.tachographs || [];

  // Handle activation/deactivation
  const handleToggleActivation = (tachograph: TachographResponse) => {
    setSelectedTachograph(tachograph);
    setShowModal(true);
  };

  const handleConfirmToggle = async () => {
    if (!selectedTachograph) return;

    try {
      await updateTachograph({
        id: selectedTachograph.id,
        data: { isActive: !selectedTachograph.isActive }
      }).unwrap();
      setShowModal(false);
      setSelectedTachograph(null);
    } catch (error) {
      console.error('Failed to update tachograph:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedTachograph(null);
  };

  // Check if revision is expiring soon (within 30 days)
  const isRevisionExpiring = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    const daysDiff = Math.ceil(
      (revisionDate.getTime() - today.getTime()) / (1000 * 60 * 60 * 24)
    );
    return daysDiff <= 30 && daysDiff >= 0;
  };

  // Check if revision is expired
  const isRevisionExpired = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    return revisionDate.getTime() < today.getTime();
  };

  const columns = [
    {
      accessorKey: 'nome',
      header: 'Tachograph',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { nome } = original;
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div className="avatar-2xl rounded-circle bg-soft-info d-flex align-items-center justify-content-center me-2">
              <FontAwesomeIcon icon="gauge-high" className="text-info fs-3" />
            </div>
            <div>
              <h6 className="mb-0">
                <Link
                  to={
                    paths.fleetTachographProfile?.replace(
                      ':tachographId',
                      original.id
                    ) || '#'
                  }
                  className="stretched-link text-900"
                >
                  {nome}
                </Link>
              </h6>
            </div>
          </Flex>
        );
      }
    },
    {
      accessorKey: 'luogo',
      header: 'Location',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { luogo } = original;
        return luogo || <span className="text-muted">-</span>;
      }
    },
    {
      accessorKey: 'targa',
      header: 'Associated Vehicle',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      enableGlobalFilter: true,
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { targa, vehicleId } = original;

        if (!targa) {
          return <span className="text-muted">-</span>;
        }

        if (vehicleId) {
          return (
            <Link
              to={
                paths.fleetVehicleProfile?.replace(':vehicleId', vehicleId) ||
                '#'
              }
            >
              <Badge bg="info">{targa}</Badge>
            </Link>
          );
        }

        return <Badge bg="info">{targa}</Badge>;
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
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { isActive } = original;
        return (
          <SubtleBadge bg={isActive ? 'success' : 'secondary'} className="me-2">
            {isActive ? 'Active' : 'Inactive'}
          </SubtleBadge>
        );
      }
    },
    {
      accessorKey: 'scadenzaRevisione',
      header: 'Inspection Expiry',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { scadenzaRevisione } = original;
        if (!scadenzaRevisione) {
          return <span className="text-muted">-</span>;
        }
        const date = new Date(scadenzaRevisione);
        const formattedDate = date.toLocaleDateString('en-GB', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        });
        const isExpiring = isRevisionExpiring(scadenzaRevisione);
        const isExpired = isRevisionExpired(scadenzaRevisione);

        return (
          <div>
            <div
              className={
                isExpired
                  ? 'text-danger fw-bold'
                  : isExpiring
                    ? 'text-warning fw-bold'
                    : 'text-900'
              }
            >
              {formattedDate}
              {(isExpiring || isExpired) && (
                <FontAwesomeIcon icon="exclamation-triangle" className="ms-1" />
              )}
            </div>
            {isExpired && <small className="text-danger">Expired</small>}
          </div>
        );
      }
    },
    {
      accessorKey: 'revisioneProgrammata',
      header: 'Scheduled Inspection',
      meta: {
        headerProps: { className: 'text-900' },
        cellProps: {
          className: 'pe-4'
        }
      },
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
        const { revisioneProgrammata } = original;
        if (!revisioneProgrammata) {
          return <span className="text-muted">-</span>;
        }
        const date = new Date(revisioneProgrammata);
        return date.toLocaleDateString('en-GB', {
          year: 'numeric',
          month: 'short',
          day: 'numeric'
        });
      }
    },
    {
      accessorKey: 'actions',
      header: 'Actions',
      meta: {
        headerProps: { className: 'text-end text-900' }
      },
      cell: ({
        row: { original }
      }: {
        row: { original: TachographResponse };
      }) => {
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
                  to={
                    paths.fleetTachographProfile?.replace(
                      ':tachographId',
                      original.id
                    ) || '#'
                  }
                >
                  View Details
                </Dropdown.Item>
                <Dropdown.Item>Edit Tachograph</Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item>Schedule Inspection</Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item
                  className="text-warning"
                  onClick={() => handleToggleActivation(original)}
                >
                  {original.isActive ? 'Deactivate' : 'Activate'}
                </Dropdown.Item>
                <Dropdown.Item className="text-danger">
                  Delete Tachograph
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
    data: tachographs,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    ActivationModal: () => (
      <TachographActivationModal
        show={showModal}
        onHide={handleCloseModal}
        tachograph={selectedTachograph}
        onConfirm={handleConfirmToggle}
        isLoading={isUpdating}
      />
    )
  };
};

export default useTachographTable;
