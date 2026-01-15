import React, { useState } from 'react';
import { Card, Button, Modal, Form, Alert } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { CraneResponse, useUpdateCraneMutation, useDeleteCraneMutation } from 'store/api/craneApi';
import { useNavigate } from 'react-router';
import paths from 'routes/paths';
import { FaPowerOff, FaTrashAlt, FaDownload, FaQrcode, FaClipboardCheck } from 'react-icons/fa';

interface CraneActionsProps {
  crane: CraneResponse;
}

const CraneActions: React.FC<CraneActionsProps> = ({ crane }) => {
  const navigate = useNavigate();
  const [showActivationModal, setShowActivationModal] = useState(false);
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showScheduleModal, setShowScheduleModal] = useState(false);
  const [scheduleDate, setScheduleDate] = useState('');

  const [updateCrane, { isLoading: isUpdating }] = useUpdateCraneMutation();
  const [deleteCrane, { isLoading: isDeleting }] = useDeleteCraneMutation();

  const handleToggleActivation = async () => {
    try {
      await updateCrane({
        id: crane.id,
        data: { isActive: !crane.isActive }
      }).unwrap();
      setShowActivationModal(false);
    } catch (error) {
      console.error('Failed to update crane status:', error);
    }
  };

  const handleDelete = async () => {
    try {
      await deleteCrane(crane.id).unwrap();
      setShowDeleteModal(false);
      navigate(paths.fleetCranes);
    } catch (error) {
      console.error('Failed to delete crane:', error);
    }
  };

  const handleScheduleVerification = async () => {
    if (!scheduleDate) return;

    try {
      await updateCrane({
        id: crane.id,
        data: { scadenzaVerifica: new Date(scheduleDate).toISOString() }
      }).unwrap();
      setShowScheduleModal(false);
      setScheduleDate('');
    } catch (error) {
      console.error('Failed to schedule verification:', error);
    }
  };

  const handleExportPDF = () => {
    // In a real app, this would generate a PDF report
    console.log('Exporting crane report as PDF...');
  };

  const handlePrintQRCode = () => {
    // In a real app, this would generate and print a QR code
    console.log('Printing QR code for crane...');
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
              variant={crane.isActive ? 'warning' : 'success'}
              size="sm"
              onClick={() => setShowActivationModal(true)}
            >
              <FaPowerOff className="me-2" />
              {crane.isActive ? 'Deactivate Crane' : 'Activate Crane'}
            </Button>

            <Button
              variant="primary"
              size="sm"
              onClick={() => setShowScheduleModal(true)}
            >
              <FaClipboardCheck className="me-2" />
              Schedule Verification
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

            <hr className="my-2" />

            <Button
              variant="danger"
              size="sm"
              onClick={() => setShowDeleteModal(true)}
            >
              <FaTrashAlt className="me-2" />
              Delete Crane
            </Button>
          </div>
        </Card.Body>
      </Card>

      {/* Activation Modal */}
      <Modal show={showActivationModal} onHide={() => setShowActivationModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>
            {crane.isActive ? 'Deactivate Crane' : 'Activate Crane'}
          </Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p>
            Are you sure you want to {crane.isActive ? 'deactivate' : 'activate'} the crane{' '}
            <strong>{crane.nome}</strong> (Serial Number: {crane.matricola})?
          </p>
          {!crane.isActive && (
            <Alert variant="info">
              Activating the crane will make it available for use and verification.
            </Alert>
          )}
          {crane.isActive && (
            <Alert variant="warning">
              Deactivating the crane will make it unavailable for use.
            </Alert>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowActivationModal(false)}>
            Cancel
          </Button>
          <Button
            variant={crane.isActive ? 'warning' : 'success'}
            onClick={handleToggleActivation}
            disabled={isUpdating}
          >
            {isUpdating ? 'Please wait...' : crane.isActive ? 'Deactivate' : 'Activate'}
          </Button>
        </Modal.Footer>
      </Modal>

      {/* Delete Modal */}
      <Modal show={showDeleteModal} onHide={() => setShowDeleteModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Delete Crane</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <Alert variant="danger">
            <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
            Warning! This action is irreversible.
          </Alert>
          <p>
            Are you sure you want to permanently delete the crane{' '}
            <strong>{crane.nome}</strong> (Serial Number: {crane.matricola})?
          </p>
          <p>All associated data will be lost.</p>
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
            {isDeleting ? 'Deleting...' : 'Delete Crane'}
          </Button>
        </Modal.Footer>
      </Modal>

      {/* Schedule Verification Modal */}
      <Modal show={showScheduleModal} onHide={() => setShowScheduleModal(false)} centered>
        <Modal.Header closeButton>
          <Modal.Title>Schedule Verification</Modal.Title>
        </Modal.Header>
        <Modal.Body>
          <p>
            Schedule the next verification for crane{' '}
            <strong>{crane.nome}</strong>.
          </p>
          <Form.Group>
            <Form.Label>Verification Date</Form.Label>
            <Form.Control
              type="date"
              value={scheduleDate}
              onChange={(e) => setScheduleDate(e.target.value)}
              min={new Date().toISOString().split('T')[0]}
            />
          </Form.Group>
        </Modal.Body>
        <Modal.Footer>
          <Button variant="secondary" onClick={() => setShowScheduleModal(false)}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleScheduleVerification}
            disabled={!scheduleDate || isUpdating}
          >
            {isUpdating ? 'Saving...' : 'Schedule'}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default CraneActions;