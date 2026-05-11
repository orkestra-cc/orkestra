
import { Button, Card, OverlayTrigger, Tooltip } from 'react-bootstrap';
import FalconCardHeader from 'components/common/FalconCardHeader';
import { Link } from 'react-router';
import Flex from 'components/common/Flex';
import classNames from 'classnames';
import cloudDownload from 'assets/img/icons/cloud-download.svg';
import editAlt from 'assets/img/icons/edit-alt.svg';

interface SharedFile {
  id: string | number;
  img: string;
  name: string;
  user: string;
  time: string;
  border?: boolean;
}

interface SharedFilesProps {
  files: SharedFile[];
  className?: string;
}

interface SharedFileItemProps {
  file: SharedFile;
  isLast: boolean;
}

const SharedFiles = ({ files, className }: SharedFilesProps) => {
  return (
    <Card className={className}>
      <FalconCardHeader
        title="Shared Files"
        titleTag="h6"
        className="py-2"
        light
        endEl={
          <Link className="py-1 fs-10 font-sans-serif" to="#!">
            View All
          </Link>
        }
      />
      <Card.Body className="pb-0">
        {files.map((file, index) => (
          <SharedFileItem
            key={file.id}
            file={file}
            isLast={index === files.length - 1}
          />
        ))}
      </Card.Body>
    </Card>
  );
};

const SharedFileItem = ({ file, isLast }: SharedFileItemProps) => {
  const { img, name, user, time, border } = file;
  return (
    <>
      <Flex alignItems="center" className="mb-3 hover-actions-trigger">
        <div className="file-thumbnail">
          <img
            className={classNames('h-100 w-100 fit-cover rounded-2', {
              border: border
            })}
            src={img}
            alt=""
          />
        </div>
        <div className="ms-3 flex-shrink-1 flex-grow-1">
          <h6 className="mb-1">
            <Link className="stretched-link text-900 fw-semibold" to="#!">
              {name}
            </Link>
          </h6>
          <div className="fs-10">
            <span className="fw-semibold">{user}</span>
            <span className="fw-medium text-600 ms-2">{time}</span>
          </div>
          <div className="hover-actions end-0 top-50 translate-middle-y">
            <OverlayTrigger
              overlay={
                <Tooltip style={{ position: 'fixed' }} id="nextTooltip">
                  Download
                </Tooltip>
              }
            >
              <Button
                variant="tertiary"
                size="sm"
                className="border-300 me-1 text-600"
                as={'a'}
                href={img}
                download
              >
                <img src={cloudDownload} alt="Download" width={15} />
              </Button>
            </OverlayTrigger>
            <OverlayTrigger
              overlay={
                <Tooltip style={{ position: 'fixed' }} id="nextTooltip">
                  Edit
                </Tooltip>
              }
            >
              <Button
                variant="tertiary"
                size="sm"
                className="border-300 text-600"
              >
                <img src={editAlt} alt="Edit" width={15} />
              </Button>
            </OverlayTrigger>
          </div>
        </div>
      </Flex>
      {!isLast && <hr className="text-200" />}
    </>
  );
};

export default SharedFiles;
