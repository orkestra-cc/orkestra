import React, { useState } from 'react';
import { Card, Button, Modal, Form, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { VehicleResponse, useUpdateVehicleMutation, useDeleteVehicleMutation } from 'store/api/vehicleApi';
import { useNavigate } from 'react-router';
import paths from 'routes/paths';
import { FaPowerOff, FaTrashAlt, FaCalendarCheck, FaDownload, FaQrcode } from 'react-icons/fa';

interface VehicleActionsProps {
  vehicle: VehicleResponse;
}

const VehicleActions: React.FC<VehicleActionsProps> = ({ vehicle }) => {
  const navigate = useNavigate();
  const [showActivationModal, setShowActivationModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showScheduleModal, setShowScheduleModal] = useState(false);
  const [scheduleDate, setScheduleDate] = useState('');

  const [updateVehicle, { isLoading: isUpdating }] = useUpdateVehicleMutation();
  const [deleteVehicle, { isLoading: isDeleting }] = useDeleteVehicleMutation();

  const handleToggleActivation = async () => {
    try {
      await updateVehicle({
        id: vehicle.id,
        data: { isActive: !vehicle.isActive }
      }).unwrap();
      setShowActivationModal(false);
    } catch (error) {
      console.error('Failed to update vehicle status:', error);
    }
  };

  const handleDelete = async () => {
    try {
      await deleteVehicle(vehicle.id).unwrap();
      setShowDeleteModal(false);
      navigate(paths.fleetVehicles);
    } catch (error) {
      console.error('Failed to delete vehicle:', error);
    }
  };

  const handleScheduleRevision = async () => {
    if (!scheduleDate) return;

    try {
      await updateVehicle({
        id: vehicle.id,
        data: { revisioneProgrammata: new Date(scheduleDate).toISOString() }
      }).unwrap();
      setShowScheduleModal(false);
      setScheduleDate('');
    } catch (error) {
      console.error('Failed to schedule revision:', error);
    }
  };

  const handleExportPDF = () => {
    // In a real app, this would generate a PDF report
    console.log('Exporting vehicle report as PDF...');
  };

  const handlePrintQRCode = () => {
    // In a real app, this would generate and print a QR code
    console.log('Printing QR code for vehicle...');
  };

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">Quick Actions</h5>
        </Card.Header>
        <Card.Body>
          <div className="d-grid gap-2">
            <Button
              variant={vehicle.isActive ? 'warning' : 'success'}
              size="sm"
              onClick={() => setShowActivationModal(true)}
            >
              <FaPowerOff className="me-2" />
              {vehicle.isActive ? 'Deactivate Vehicle' : 'Activate Vehicle'}
            </Button>

            <Button
              variant="primary"
              size="sm"
              onClick={() => setShowScheduleModal(true)}
            >
              <FaCalendarCheck className="me-2" />
              Schedule Inspection
            </Button>

            <Button
              variant="outline-secondary"
              size="sm"
              onClick={handleExportPDF}
            >
              <FaDownload className="me-2" />
              Export PDF Report
            </Button>

            <Button
              variant="outline-secondary"
              size="sm"
              onClick={handlePrintQRCode}
            >
              <FaQrcode className="me-2" />
              Print QR Code
            </Button>

            <hr />

            <Button
              variant="outline-danger"
              size="sm"
              onClick={() => setShowDeleteModal(true)}
            >
              <FaTrashAlt className="me-2" />
              Delete Vehicle
            </Button>
          </div>
        </Card.Body>
      </Card>

      {/* Activation/Deactivation Modal */}
      <Modal show={showActivationModal} onHide={() => setShowActivationModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>
            {vehicle.isActive ? 'Deactivate Vehicle' : 'Activate Vehicle'}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p>
            Are you sure you want to {vehicle.isActive ? 'deactivate' : 'activate'} the vehicle{' '}
            <strong>{vehicle.nome}</strong> (License Plate: {vehicle.targa})?
          </p>
          {vehicle.isActive && (
            <Alert variant="warning">
              <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
              The vehicle will no longer be available for driver assignment.
            </Alert>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowActivationModal(false)}>
            Cancel
          </Button>
          <Button
            variant={vehicle.isActive ? 'warning' : 'success'}
            onClick={handleToggleActivation}
            disabled={isUpdating}
          >
            {isUpdating ? 'Please wait...' : vehicle.isActive ? 'Deactivate' : 'Activate'}
          </Button>
        </Modal.Footer>
      </Modal>

      {/* Delete Modal */}
      <Modal show={showDeleteModal} onHide={() => setShowDeleteModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Delete Vehicle</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Alert variant="danger">
            <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
            <strong>Warning!</strong> This action cannot be undone.
          </Alert>
          <p>
            Are you sure you want to permanently delete the vehicle{' '}
            <strong>{vehicle.nome}</strong> (License Plate: {vehicle.targa})?
          </p>
          <p className="text-muted small">
            All data associated with this vehicle, including maintenance history,
            will be permanently deleted.
          </p>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowDeleteModal(false)}>
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={handleDelete}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting...' : 'Delete Permanently'}
          </Button>
        </Modal.Footer>
      </Modal>

      {/* Schedule Revision Modal */}
      <Modal show={showScheduleModal} onHide={() => setShowScheduleModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Schedule Inspection</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p>
            Select the date for the next inspection of the vehicle{' '}
            <strong>{vehicle.nome}</strong>:
          </p>
          <Form.Group className="mb-3">
            <Form.Label>Scheduled Inspection Date</Form.Label>
            <Form.Control
              type="date"
              value={scheduleDate}
              onChange={(e) => setScheduleDate(e.target.value)}
              min={new Date().toISOString().split('T')[0]}
            />
          </Form.Group>
          {vehicle.scadenzaRevisione && (
            <Alert variant="info">
              <FontAwesomeIcon icon="info-circle" className="me-2" />
              Current expiry: {new Date(vehicle.scadenzaRevisione).toLocaleDateString('en-GB')}
            </Alert>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowScheduleModal(false)}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleScheduleRevision}
            disabled={!scheduleDate || isUpdating}
          >
            {isUpdating ? 'Saving...' : 'Schedule'}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default VehicleActions;