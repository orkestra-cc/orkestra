import React, { useState } from 'react';
import { Card, Button, Modal } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { TachographResponse, useUpdateTachographMutation, useDeleteTachographMutation } from 'store/api/tachographApi';
import { useNavigate } from 'react-router';
import paths from 'routes/paths';
import FalconCloseButton from 'components/common/FalconCloseButton';

interface TachographActionsProps {
  tachograph: TachographResponse;
}

const TachographActions: React.FC<TachographActionsProps> = ({ tachograph }) => {
  const navigate = useNavigate();
  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showActivationModal, setShowActivationModal] = useState(false);
  const [updateTachograph, { isLoading: isUpdating }] = useUpdateTachographMutation();
  const [deleteTachograph, { isLoading: isDeleting }] = useDeleteTachographMutation();

  const handleToggleActivation = async () => {
    try {
      await updateTachograph({
        id: tachograph.id,
        data: { isActive: !tachograph.isActive }
      }).unwrap();
      setShowActivationModal(false);
    } catch (error) {
      console.error('Failed to update tachograph status:', error);
    }
  };

  const handleDelete = async () => {
    try {
      await deleteTachograph(tachograph.id).unwrap();
      navigate(paths.fleetTachographs);
    } catch (error) {
      console.error('Failed to delete tachograph:', error);
    }
  };

  const isRevisionExpiring = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    const daysDiff = Math.ceil((revisionDate.getTime() - today.getTime()) / (1000 * 60 * 60 * 24));
    return daysDiff <= 30 && daysDiff >= 0;
  };

  const isRevisionExpired = (date?: string) => {
    if (!date) return false;
    const revisionDate = new Date(date);
    const today = new Date();
    return revisionDate.getTime() < today.getTime();
  };

  return (
    <>
      <Card className="mb-3">
        <Card.Header className="bg-body-tertiary">
          <h5 className="mb-0">
            <FontAwesomeIcon icon="gears" className="me-2 text-primary" />
            Quick Actions
          </h5>
        </Card.Header>
        <Card.Body>
          <div className="d-grid gap-2">
            <Button
              variant={tachograph.isActive ? 'warning' : 'success'}
              onClick={() => setShowActivationModal(true)}
            >
              <FontAwesomeIcon
                icon={tachograph.isActive ? 'ban' : 'check-circle'}
                className="me-2"
              />
              {tachograph.isActive ? 'Deactivate' : 'Activate'} Tachograph
            </Button>

            {(isRevisionExpiring(tachograph.scadenzaRevisione) || isRevisionExpired(tachograph.scadenzaRevisione)) && (
              <Button variant="info">
                <FontAwesomeIcon icon="calendar-alt" className="me-2" />
                Schedule Inspection
              </Button>
            )}

            <Button variant="primary">
              <FontAwesomeIcon icon="print" className="me-2" />
              Print Card
            </Button>

            <Button variant="secondary">
              <FontAwesomeIcon icon="file-export" className="me-2" />
              Export Data
            </Button>

            <hr className="my-2" />

            <Button
              variant="outline-danger"
              size="sm"
              onClick={() => setShowDeleteModal(true)}
            >
              <FontAwesomeIcon icon="trash" className="me-2" />
              Delete Tachograph
            </Button>
          </div>
        </Card.Body>
      </Card>

      {/* Activation Modal */}
      <Modal show={showActivationModal} onHide={() => setShowActivationModal(false)} centered>
        <Modal.Header>
          <Modal.Title>
            {tachograph.isActive ? 'Deactivate' : 'Activate'} Tachograph
          </Modal.Title>
          <FalconCloseButton onClick={() => setShowActivationModal(false)} />
        </Modal.Header>
        <Modal.Body>
          <p>
            Are you sure you want to {tachograph.isActive ? 'deactivate' : 'activate'} the tachograph{' '}
            <strong>{tachograph.nome}</strong> (License Plate: {tachograph.targa})?
          </p>
          {tachograph.isActive && (
            <p className="text-warning mb-0">
              The tachograph will not be available until it is reactivated.
            </p>
          )}
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => setShowActivationModal(false)}
            disabled={isUpdating}
          >
            Cancel
          </Button>
          <Button
            variant={tachograph.isActive ? 'warning' : 'success'}
            onClick={handleToggleActivation}
            disabled={isUpdating}
          >
            {isUpdating ? 'Please wait...' : tachograph.isActive ? 'Deactivate' : 'Activate'}
          </Button>
        </Modal.Footer>
      </Modal>

      {/* Delete Modal */}
      <Modal show={showDeleteModal} onHide={() => setShowDeleteModal(false)} centered>
        <Modal.Header>
          <Modal.Title>Delete Tachograph</Modal.Title>
          <FalconCloseButton onClick={() => setShowDeleteModal(false)} />
        </Modal.Header>
        <Modal.Body>
          <p>
            Are you sure you want to permanently delete the tachograph{' '}
            <strong>{tachograph.nome}</strong> (License Plate: {tachograph.targa})?
          </p>
          <p className="text-danger fw-bold mb-0">
            <FontAwesomeIcon icon="exclamation-triangle" className="me-2" />
            This action cannot be undone!
          </p>
        </Modal.Body>
        <Modal.Footer>
          <Button
            variant="secondary"
            onClick={() => setShowDeleteModal(false)}
            disabled={isDeleting}
          >
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={handleDelete}
            disabled={isDeleting}
          >
            {isDeleting ? (
              <>
                <FontAwesomeIcon icon="spinner" spin className="me-2" />
                Deleting...
              </>
            ) : (
              <>
                <FontAwesomeIcon icon="trash" className="me-2" />
                Delete Permanently
              </>
            )}
          </Button>
        </Modal.Footer>
      </Modal>
    </>
  );
};

export default TachographActions;