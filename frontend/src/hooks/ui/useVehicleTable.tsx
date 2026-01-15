import React, { useState } from 'react';
import { Link } from 'react-router';
import paths from 'routes/paths';
import useAdvanceTable from './useAdvanceTable';
import Flex from 'components/common/Flex';
import SubtleBadge from 'components/common/SubtleBadge';
import { Badge, Dropdown, Modal, Button } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  useGetVehiclesQuery,
  useUpdateVehicleMutation,
  VehicleResponse
} from 'store/api/vehicleApi';
import FalconCloseButton from 'components/common/FalconCloseButton';
import { FaTruck, FaTrailer } from 'react-icons/fa';

// Confirmation Modal Component
interface VehicleActivationModalProps {
  show: boolean;
  onHide: () => void;
  vehicle: VehicleResponse | null;
  onConfirm: () => void;
  isLoading: boolean;
}

const VehicleActivationModal: React.FC<VehicleActivationModalProps> = ({
  show,
  onHide,
  vehicle,
  onConfirm,
  isLoading
}) => {
  if (!vehicle) return null;

  const isActivating = !vehicle.isActive;

  return (
    <Modal show={show} onHide={onHide} centered>
      <Modal.Header>
        <Modal.Title>
          {isActivating ? 'Activate Vehicle' : 'Deactivate Vehicle'}
        </Modal.Title>
        <FalconCloseButton onClick={onHide} />
      </Modal.Header>
      <Modal.Body>
        <p>
          Are you sure you want to {isActivating ? 'activate' : 'deactivate'} the
          vehicle <strong>{vehicle.nome}</strong> (License Plate: {vehicle.targa})?
        </p>
        {!isActivating && (
          <p className="text-warning mb-0">
            The vehicle will no longer be available for assignment until it is
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

const useVehicleTable = (options?: any) => {
  const [showModal, setShowModal] = useState(false);
  const [selectedVehicle, setSelectedVehicle] =
    useState<VehicleResponse | null>(null);
  const [updateVehicle, { isLoading: isUpdating }] = useUpdateVehicleMutation();

  // Fetch vehicles from backend API
  const {
    data: vehiclesResponse,
    isLoading,
    error
  } = useGetVehiclesQuery({
    pageSize: options?.perPage || 10,
    page: 1
  });

  // Transform the data for the table
  const vehicles = vehiclesResponse?.vehicles || [];

  // Handle activation/deactivation
  const handleToggleActivation = (vehicle: VehicleResponse) => {
    setSelectedVehicle(vehicle);
    setShowModal(true);
  };

  const handleConfirmToggle = async () => {
    if (!selectedVehicle) return;

    try {
      await updateVehicle({
        id: selectedVehicle.id,
        data: { isActive: !selectedVehicle.isActive }
      }).unwrap();
      setShowModal(false);
      setSelectedVehicle(null);
    } catch (error) {
      console.error('Failed to update vehicle:', error);
    }
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setSelectedVehicle(null);
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
    // Hidden searchable columns for better search
    {
      accessorKey: 'targa',
      header: '',
      meta: {
        headerProps: { style: { display: 'none' } },
        cellProps: { style: { display: 'none' } }
      },
      enableGlobalFilter: true
    },
    {
      accessorKey: 'nome',
      header: 'Vehicle',
      meta: {
        headerProps: { className: 'ps-2 text-900', style: { height: '46px' } },
        cellProps: {
          className: 'py-2 white-space-nowrap pe-3 pe-xxl-4 ps-2'
        }
      },
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
        const { nome, targa, tipo } = original;
        return (
          <Flex alignItems="center" className="position-relative py-1">
            <div className="avatar-2xl rounded-circle bg-soft-primary d-flex align-items-center justify-content-center me-2">
              {tipo === 'motrice' || tipo === 'trattore' || tipo === 'semovente' ? (
                <FaTruck className="text-primary" size={20} />
              ) : (
                <FaTrailer className="text-primary" size={20} />
              )}
            </div>
            <div>
              <h6 className="mb-0">
                <Link
                  to={
                    paths.fleetVehicleProfile?.replace(
                      ':vehicleId',
                      original.id
                    ) || '#'
                  }
                  className="stretched-link text-900"
                >
                  {nome}
                </Link>
              </h6>
              <small className="text-muted">License Plate: {targa}</small>
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
        const { tipo } = original;
        const tipoColors: Record<string, string> = {
          motrice: 'primary',
          rimorchio: 'info',
          'semi-rimorchio': 'success',
          trattore: 'warning',
          semovente: 'danger'
        };
        const tipoLabels: Record<string, string> = {
          motrice: 'Truck',
          rimorchio: 'Trailer',
          'semi-rimorchio': 'Semi-trailer',
          trattore: 'Tractor',
          semovente: 'Self-propelled'
        };
        return (
          <Badge
            bg={tipoColors[tipo as keyof typeof tipoColors] || 'secondary'}
          >
            {tipoLabels[tipo as keyof typeof tipoLabels] || tipo}
          </Badge>
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
        const { luogo } = original;
        return luogo || <span className="text-muted">-</span>;
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
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
      cell: ({ row: { original } }: { row: { original: VehicleResponse } }) => {
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
                    paths.fleetVehicleProfile?.replace(
                      ':vehicleId',
                      original.id
                    ) || '#'
                  }
                >
                  View Details
                </Dropdown.Item>
                <Dropdown.Item>Edit Vehicle</Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item
                  className="text-warning"
                  onClick={() => handleToggleActivation(original)}
                >
                  {original.isActive ? 'Deactivate' : 'Activate'}
                </Dropdown.Item>
                <Dropdown.Item className="text-danger">
                  Delete Vehicle
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
    data: vehicles,
    isLoading,
    error,
    ...options
  });

  return {
    ...table,
    ActivationModal: () => (
      <VehicleActivationModal
        show={showModal}
        onHide={handleCloseModal}
        vehicle={selectedVehicle}
        onConfirm={handleConfirmToggle}
        isLoading={isUpdating}
      />
    )
  };
};

export default useVehicleTable;
