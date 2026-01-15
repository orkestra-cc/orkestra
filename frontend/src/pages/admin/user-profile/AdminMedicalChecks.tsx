import React, { useState } from 'react';
import { Card, Badge, Button, Form, Row, Col } from 'react-bootstrap';
import { User, MedicalCheck, useUpdateUserMutation } from 'store/api/userApi';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import { FaEdit, FaSave, FaTimes } from 'react-icons/fa';

interface AdminMedicalChecksProps {
  user: User;
}

const AdminMedicalChecks: React.FC<AdminMedicalChecksProps> = ({ user }) => {
  const [isEditing, setIsEditing] = useState(false);
  const [updateUser, { isLoading }] = useUpdateUserMutation();

  const [formData, setFormData] = useState({
    medicalChecks:
      user.medicalChecks?.map((check: MedicalCheck) => ({
        id: check.id,
        type: check.type,
        notes: check.notes || '',
        expiry: check.expiry
          ? new Date(check.expiry).toISOString().split('T')[0]
          : '',
        booked: check.booked
          ? new Date(check.booked).toISOString().split('T')[0]
          : '',
        where: check.where || '',
        doctor: check.doctor || ''
      })) || []
  });

  // Helper function to format date
  const formatDate = (dateString: string | undefined) => {
    if (!dateString) return 'Not specified';
    return new Date(dateString).toLocaleDateString('en-GB', {
      year: 'numeric',
      month: 'long',
      day: 'numeric'
    });
  };

  // Helper function to check if date is expired
  const isExpired = (dateString: string | undefined) => {
    if (!dateString) return false;
    return new Date(dateString) < new Date();
  };

  // Helper function to check if date is expiring soon (within 30 days)
  const isExpiringSoon = (dateString: string | undefined) => {
    if (!dateString) return false;
    const date = new Date(dateString);
    const now = new Date();
    const thirtyDaysFromNow = new Date();
    thirtyDaysFromNow.setDate(now.getDate() + 30);
    return date > now && date < thirtyDaysFromNow;
  };

  // Helper function to get expiry badge
  const getExpiryBadge = (dateString: string | undefined) => {
    if (!dateString) return null;

    if (isExpired(dateString)) {
      return (
        <Badge bg="danger" className="ms-2">
          Expired
        </Badge>
      );
    }
    if (isExpiringSoon(dateString)) {
      return (
        <Badge bg="warning" className="ms-2">
          Expiring Soon
        </Badge>
      );
    }
    return (
      <Badge bg="success" className="ms-2">
        Valid
      </Badge>
    );
  };

  const handleEdit = () => {
    setIsEditing(true);
  };

  const handleCancel = () => {
    setIsEditing(false);
    // Reset form data to original values
    setFormData({
      medicalChecks:
        user.medicalChecks?.map((check: MedicalCheck) => ({
          id: check.id,
          type: check.type,
          notes: check.notes || '',
          expiry: check.expiry
            ? new Date(check.expiry).toISOString().split('T')[0]
            : '',
          booked: check.booked
            ? new Date(check.booked).toISOString().split('T')[0]
            : '',
          where: check.where || '',
          doctor: check.doctor || ''
        })) || []
    });
  };

  const handleSave = async () => {
    try {
      const dataToSubmit: any = {};

      // Add medical checks
      if (formData.medicalChecks.length > 0) {
        dataToSubmit.medicalChecks = formData.medicalChecks.map(
          (check: any) => ({
            id: check.id,
            type: check.type,
            notes: check.notes || undefined,
            expiry: check.expiry
              ? new Date(check.expiry).toISOString()
              : undefined,
            booked: check.booked
              ? new Date(check.booked).toISOString()
              : undefined,
            where: check.where || undefined,
            doctor: check.doctor || undefined
          })
        );
      }

      await updateUser({
        id: user.id,
        data: dataToSubmit
      }).unwrap();

      setIsEditing(false);
    } catch (error) {
      console.error('Failed to update user medical checks:', error);
    }
  };

  const handleMedicalCheckChange = (
    id: string,
    field: keyof MedicalCheck,
    value: string
  ) => {
    setFormData(prev => ({
      ...prev,
      medicalChecks: prev.medicalChecks.map((check: any) =>
        check.id === id ? { ...check, [field]: value } : check
      )
    }));
  };

  const handleAddMedicalCheck = () => {
    const newCheck = {
      id: crypto.randomUUID(),
      type: '',
      notes: '',
      expiry: '',
      booked: '',
      where: '',
      doctor: ''
    };
    setFormData(prev => ({
      ...prev,
      medicalChecks: [...prev.medicalChecks, newCheck]
    }));
  };

  const handleDeleteMedicalCheck = (id: string) => {
    setFormData(prev => ({
      ...prev,
      medicalChecks: prev.medicalChecks.filter((check: any) => check.id !== id)
    }));
  };

  return (
    <Card className="mb-3">
      <Card.Header className="bg-body-tertiary d-flex justify-content-between align-items-center">
        <h5 className="mb-0">
          <FontAwesomeIcon
            icon={'file-alt' as IconProp}
            className="me-2"
          />
          Medical Checks
        </h5>
        <div>
          {!isEditing ? (
            <Button variant="falcon-default" size="sm" onClick={handleEdit}>
              <FaEdit className="me-1" /> Edit
            </Button>
          ) : (
            <>
              <Button
                variant="success"
                size="sm"
                className="me-2"
                onClick={handleSave}
                disabled={isLoading}
              >
                <FaSave className="me-1" /> Save
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleCancel}
                disabled={isLoading}
              >
                <FaTimes className="me-1" /> Cancel
              </Button>
            </>
          )}
        </div>
      </Card.Header>
      <Card.Body>
        {formData.medicalChecks.length > 0 || isEditing ? (
          <>
            {formData.medicalChecks.map((check: any, index: number) => (
              <div key={check.id}>
                {/* Medical Check Section */}
                <Row
                  className={`${index < formData.medicalChecks.length - 1 ? 'border-bottom pb-3 mb-3' : ''}`}
                >
                  <Col xs={12} className="mb-3">
                    <div className="d-flex justify-content-between align-items-start">
                      <div className="flex-grow-1">
                        <small className="text-700 d-block mb-1">
                          Check Type
                        </small>
                        {isEditing ? (
                          <Form.Control
                            type="text"
                            value={check.type}
                            onChange={e =>
                              handleMedicalCheckChange(
                                check.id,
                                'type',
                                e.target.value
                              )
                            }
                            size="sm"
                            placeholder="e.g. Periodic medical check"
                          />
                        ) : (
                          <div className="fw-semi-bold">
                            <FontAwesomeIcon
                              icon={'file-alt' as IconProp}
                              className="me-2 text-muted"
                            />
                            {check.type || 'Not specified'}
                            {check.expiry && getExpiryBadge(check.expiry)}
                          </div>
                        )}
                      </div>
                      {isEditing && (
                        <Button
                          variant="falcon-danger"
                          size="sm"
                          onClick={() => handleDeleteMedicalCheck(check.id)}
                          className="ms-2"
                        >
                          <FontAwesomeIcon icon={'trash' as IconProp} />
                        </Button>
                      )}
                    </div>
                  </Col>

                  <Col md={6} className="mb-3">
                    <small className="text-700 d-block mb-1">Expiry</small>
                    {isEditing ? (
                      <Form.Control
                        type="date"
                        value={check.expiry}
                        onChange={e =>
                          handleMedicalCheckChange(
                            check.id,
                            'expiry',
                            e.target.value
                          )
                        }
                        size="sm"
                      />
                    ) : (
                      <div className="fw-semi-bold">
                        <FontAwesomeIcon
                          icon={'calendar-alt' as IconProp}
                          className="me-2 text-muted"
                        />
                        {formatDate(check.expiry)}
                      </div>
                    )}
                  </Col>

                  <Col md={6} className="mb-3">
                    <small className="text-700 d-block mb-1">
                      Booking Date
                    </small>
                    {isEditing ? (
                      <Form.Control
                        type="date"
                        value={check.booked}
                        onChange={e =>
                          handleMedicalCheckChange(
                            check.id,
                            'booked',
                            e.target.value
                          )
                        }
                        size="sm"
                      />
                    ) : (
                      <div className="fw-semi-bold">
                        <FontAwesomeIcon
                          icon={'calendar-check' as IconProp}
                          className="me-2 text-muted"
                        />
                        {check.booked
                          ? formatDate(check.booked)
                          : 'Not booked'}
                      </div>
                    )}
                  </Col>

                  <Col md={6} className="mb-3">
                    <small className="text-700 d-block mb-1">Location</small>
                    {isEditing ? (
                      <Form.Control
                        type="text"
                        value={check.where}
                        onChange={e =>
                          handleMedicalCheckChange(
                            check.id,
                            'where',
                            e.target.value
                          )
                        }
                        size="sm"
                        placeholder="e.g. Local clinic"
                      />
                    ) : (
                      <div className="fw-semi-bold">
                        <FontAwesomeIcon
                          icon={'map-marker-alt' as IconProp}
                          className="me-2 text-muted"
                        />
                        {check.where || 'Not specified'}
                      </div>
                    )}
                  </Col>

                  <Col md={6} className="mb-3">
                    <small className="text-700 d-block mb-1">Doctor</small>
                    {isEditing ? (
                      <Form.Control
                        type="text"
                        value={check.doctor}
                        onChange={e =>
                          handleMedicalCheckChange(
                            check.id,
                            'doctor',
                            e.target.value
                          )
                        }
                        size="sm"
                        placeholder="e.g. Dr. Smith"
                      />
                    ) : (
                      <div className="fw-semi-bold">
                        <FontAwesomeIcon
                          icon={'user' as IconProp}
                          className="me-2 text-muted"
                        />
                        {check.doctor || 'Not specified'}
                      </div>
                    )}
                  </Col>

                  <Col xs={12}>
                    <small className="text-700 d-block mb-1">Notes</small>
                    {isEditing ? (
                      <Form.Control
                        as="textarea"
                        rows={2}
                        value={check.notes}
                        onChange={e =>
                          handleMedicalCheckChange(
                            check.id,
                            'notes',
                            e.target.value
                          )
                        }
                        size="sm"
                        placeholder="Additional notes..."
                      />
                    ) : (
                      <div className="text-600">
                        {check.notes || 'No notes'}
                      </div>
                    )}
                  </Col>
                </Row>
              </div>
            ))}
            {isEditing && (
              <div className="mt-3">
                <Button
                  variant="falcon-default"
                  size="sm"
                  onClick={handleAddMedicalCheck}
                >
                  <FontAwesomeIcon icon={'plus' as IconProp} className="me-1" />
                  Add Medical Check
                </Button>
              </div>
            )}
          </>
        ) : (
          <p className="text-600 text-center mb-0">
            No medical checks registered
          </p>
        )}
      </Card.Body>
    </Card>
  );
};

export default AdminMedicalChecks;
