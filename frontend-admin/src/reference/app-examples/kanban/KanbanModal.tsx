
import { CloseButton, Col, Modal, Row, Dropdown } from 'react-bootstrap';
import Background from 'components/common/Background';
import { Link } from 'react-router';
import ModalMediaContent from './ModalMediaContent';
import GroupMember from './GroupMember';
import { members } from 'data/kanban';
import ModalLabelContent from './ModalLabelContent';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import ModalAttachmentContent from './ModalAttachmentContent';
import ModalCommentContent from './ModalCommentContent';
import ModalActivityContent from './ModalActivityContent';
import { useKanbanContext } from 'providers/KanbanProvider';
import ModalSidebar from './ModalSidebar';

const KanbanModal = () => {
  const kanbanModal = useKanbanContext().kanbanModal || { show: false, modalContent: {} };
  const toggleKanbanModal = useKanbanContext().toggleKanbanModal;

  const handleClose = () => {
    toggleKanbanModal();
  };

  return (
    <Modal
      show={kanbanModal.show}
      size="lg"
      onHide={handleClose}
      contentClassName="border-0"
      dialogClassName="mt-6"
    >
      {kanbanModal.modalContent?.image && (
        <div className="position-relative overflow-hidden py-8">
          <Background
            image={kanbanModal.modalContent.image}
            className="rounded-top-lg"
          />
        </div>
      )}
      <div className="position-absolute top-0 end-0 mt-3 me-3 z-1">
        <CloseButton
          onClick={handleClose}
          className="btn btn-sm btn-circle d-flex flex-center transition-base"
        />
      </div>
      <Modal.Body className="p-0">
        <div className="bg-body-tertiary rounded-top-lg px-4 py-3">
          <h4 className="mb-1">Add a new illustration to the landing page</h4>
          <p className="fs-11 mb-0">
            Added by{' '}
            <Link to="#!" className="text-600 fw-semibold">
              Antony
            </Link>
          </p>
        </div>
        <div className="p-4">
          <Row>
            <Col lg={9}>
              <ModalMediaContent title="Reviewers" icon="user" transform="" headingClass="" headingContent={null}>
                <GroupMember
                  users={members}
                  addMember
                  showMember={6}
                  avatarSize="xl"
                  className=""
                />
              </ModalMediaContent>

              <ModalMediaContent title="Labels" icon="tag" transform="" headingClass="" headingContent={null}>
                <ModalLabelContent />
              </ModalMediaContent>

              <ModalMediaContent title="Description" icon="align-left" transform="" headingClass="" headingContent={null}>
                <p className="text-word-break fs-10">
                  The illustration must match to the contrast of the theme. The
                  illustraion must described the concept of the design. To know
                  more about the theme visit the page.{' '}
                  <Link to="/" target="_blank" rel="noopener noreferrer">
                    https://falconreact.prium.me/
                  </Link>
                </p>
              </ModalMediaContent>

              <ModalMediaContent
                title="Attachments"
                icon="paperclip"
                transform=""
                headingClass="d-flex justify-content-between"
                headingContent={
                  <Dropdown>
                    <Dropdown.Toggle
                      variant="falcon-default"
                      size="sm"
                      className="dropdown-caret-none fs-11"
                    >
                      <FontAwesomeIcon icon="plus" />
                    </Dropdown.Toggle>
                    <Dropdown.Menu>
                      <Dropdown.Item>Computer</Dropdown.Item>
                      <Dropdown.Item>Google Drive</Dropdown.Item>
                      <Dropdown.Divider />
                      <Dropdown.Item>Attach Link</Dropdown.Item>
                    </Dropdown.Menu>
                  </Dropdown>
                }
              >
                <ModalAttachmentContent />
              </ModalMediaContent>

              <ModalMediaContent
                title="Comments"
                icon={['far', 'comment']}
                transform=""
                headingClass="mb-3"
                headingContent={null}
              >
                <ModalCommentContent />
              </ModalMediaContent>

              <ModalMediaContent
                title="Activity"
                icon="list-ul"
                transform=""
                headingClass="mb-3"
                headingContent={null}
                isHr={false}
              >
                <ModalActivityContent />
              </ModalMediaContent>
            </Col>
            <Col lg={3}>
              <ModalSidebar />
            </Col>
          </Row>
        </div>
      </Modal.Body>
    </Modal>
  );
};

export default KanbanModal;
