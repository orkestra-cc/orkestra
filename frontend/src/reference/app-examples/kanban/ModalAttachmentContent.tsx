import { useState } from 'react';
import { attachments } from 'data/kanban';
import Flex from 'components/common/Flex';
import classNames from 'classnames';
import Background from 'components/common/Background';
import { Link } from 'react-router';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { Modal, CloseButton } from 'react-bootstrap';
import FalconLightBoxGallery from 'components/common/FalconLightBoxGallery';

interface Attachment {
  id: string;
  image?: string;
  type?: string;
  title: string;
  date: string;
  src?: string;
}

interface AttachmentItemProps {
  setImgIndex: (index: number) => void;
  attachment: Attachment;
  isLast: boolean;
  index: number;
}

const AttachmentItem = ({ setImgIndex, attachment, isLast, index }: AttachmentItemProps) => {
  const [nestedModal, setNestedModal] = useState(false);

  return (
    <>
      <Flex alignItems="center" className={classNames({ 'mb-3': !isLast })}>
        <div className="bg-attachment me-3">
          {attachment.image ? (
            <>
              {attachment.type !== 'video' ? (
                <div
                  className="cursor-pointer"
                  onClick={() => setImgIndex(index)}
                >
                  <Background image={attachment.image || ''} />
                </div>
              ) : (
                <>
                  <Link to="#!" onClick={() => setNestedModal(true)}>
                    <Background image={attachment.image} className="rounded" />
                    <div className="icon-play">
                      <FontAwesomeIcon icon="play" />
                    </div>
                  </Link>
                </>
              )}
            </>
          ) : (
            <span className="text-uppercase fw-bold">{attachment.type}</span>
          )}
        </div>

        <div className="flex-1 fs-11">
          <h6 className="mb-1">
            {attachment.image ? (
              <>
                {attachment.type !== 'video' ? (
                  <Link
                    onClick={() => setImgIndex(index)}
                    to="#!"
                    className="text-decoration-none"
                  >
                    {attachment.title}
                  </Link>
                ) : (
                  <>
                    <Link to="#!" onClick={() => setNestedModal(true)}>
                      {attachment.title}
                    </Link>
                  </>
                )}
              </>
            ) : (
              <Link to="#!" className="text-decoration-none">
                {attachment.title}
              </Link>
            )}
          </h6>
          <Link to="#!" className="link-600 fw-semibold">
            Edit
          </Link>
          <span className="mx-1">|</span>
          <Link to="#!" className="link-600 fw-semibold">
            Remove
          </Link>
          <p className="mb-0">Uploaded at {attachment.date}</p>
        </div>
      </Flex>

      <Modal
        show={nestedModal}
        size="xl"
        onHide={() => setNestedModal(false)}
        className="video-popup"
        contentClassName="bg-transparent border-0"
        backdropClassName="video-popup-backdrop"
        centered
      >
        <CloseButton
          variant="white"
          onClick={() => setNestedModal(false)}
          className="video-popup-close-btn"
        />
        <Modal.Body className="p-0 rounded overflow-hidden">
          <video
            poster={attachment.image}
            className="d-block"
            width="100%"
            height="100%"
            controls
            autoPlay
          >
            <source src={attachment.src} type="video/mp4" />
          </video>
        </Modal.Body>
      </Modal>
    </>
  );
};

const ModalAttachmentContent = () => {
  const images = (attachments as Attachment[]).map(item => item.image || '');

  return (
    <>
      <FalconLightBoxGallery images={images}>
        {(setImgIndex: (index: number) => void) => (
          <>
            {(attachments as Attachment[]).map((attachment: Attachment, index: number) => (
              <AttachmentItem
                setImgIndex={setImgIndex}
                key={attachment.id}
                attachment={attachment}
                index={index}
                isLast={attachments.length - 1 === index}
              />
            ))}
          </>
        )}
      </FalconLightBoxGallery>
    </>
  );
};

export default ModalAttachmentContent;
