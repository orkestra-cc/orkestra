import { useState } from 'react';
import {
  Button,
  Card,
  Dropdown,
  Form,
  OverlayTrigger,
  Tooltip
} from 'react-bootstrap';
import 'tinymce/skins/ui/oxide/skin.css';
import Flex from 'components/common/Flex';
import CardDropdown from 'components/common/CardDropdown';
import IconButton from 'components/common/IconButton';
import ComposeAttachment from './ComposeAttachment';
import TinymceEditor from 'components/common/TinymceEditor';
import { useForm, FieldValues } from 'react-hook-form';
import { toast } from 'react-toastify';

interface Attachment {
  id: string | number;
  name: string;
  size: number;
  type: string;
}

interface ComposeProps {
  setShowForm?: (show: boolean) => void;
  title?: string;
  bodyClassName?: string;
}

const rawAttachments: Attachment[] = [
  { id: 1, name: 'winter.jpg', size: 893952, type: 'image/jpg' },
  { id: 2, name: 'coffee.zip', size: 350208, type: 'application/zip' }
];

const Compose = ({ setShowForm, title, bodyClassName }: ComposeProps) => {
  const { register, handleSubmit, setValue } = useForm();

  const [attachments, setAttachments] = useState(rawAttachments);

  const handleAddAttachment = (files: FileList | null) => {
    if (!files || files.length === 0) return;
    const fileArray: Attachment[] = [];
    Array.from(files).forEach(file => {
      const { name, size, type } = file;

      const newFile: Attachment = {
        id: name + 1 + Date.now(),
        name,
        size,
        type
      };
      fileArray.push(newFile);
    });
    setAttachments([...attachments, ...fileArray]);
  };

  const handleDetachAttachment = (id: string | number) => {
    setAttachments(attachments.filter(attachment => attachment.id !== id));
  };

  const onSubmitData = (data: FieldValues) => {
    setAttachments([]);
    toast(
      <div className="flex-1 py-2">
        <h6>Subject: {data.subject}</h6>
        <hr />
        <p className="mb-0">An email is successfully sent to recipient.</p>
      </div>
    );
  };

  return (
    <Card
      as={Form}
      onSubmit={handleSubmit(onSubmitData)}
      className={bodyClassName}
    >
      <Card.Header className="bg-body-tertiary">
        <h5 className="mb-0">{title ? title : 'New message'}</h5>
      </Card.Header>
      <Card.Body className="p-0">
        {setShowForm && (
          <div className="border border-top-0 border-200">
            <Form.Control
              type="email"
              placeholder="From"
              className="border-0 outline-none px-x1 rounded-0"
              {...register('form')}
            />
          </div>
        )}
        <div className="border border-top-0 border-200">
          <Form.Control
            type="email"
            placeholder="To"
            className="border-0 outline-none px-x1 rounded-0"
            {...register('to')}
          />
        </div>
        <div className="border border-y-0 border-200">
          <Form.Control
            type="text"
            placeholder="Subject"
            className="border-0 outline-none px-x1 rounded-0"
            {...register('subject')}
          />
        </div>
        <div className="border border-0 border-200">
          <TinymceEditor
            handleChange={newValue => setValue('content', newValue)}
          />
        </div>
        {attachments.length > 0 && (
          <div className="bg-body-tertiary px-x1 py-3">
            <Flex direction="column" inline>
              {attachments.map(attachment => (
                <ComposeAttachment
                  {...attachment}
                  key={attachment.id}
                  handleDetachAttachment={handleDetachAttachment}
                />
              ))}
            </Flex>
          </div>
        )}
      </Card.Body>
      <Card.Footer>
        <Flex alignItems="center" justifyContent="between">
          <Flex>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              className="px-4 px-sm-5 me-2"
            >
              Send
            </Button>
            <Form.Group controlId="email-attachment">
              <Form.Control
                type="file"
                multiple
                className="d-none"
                onChange={(e) => handleAddAttachment((e.target as HTMLInputElement).files)}
              />
              <OverlayTrigger
                overlay={
                  <Tooltip style={{ position: 'fixed' }} id="previousTooltip">
                    Attach files
                  </Tooltip>
                }
              >
                <div>
                  <IconButton
                    as={Form.Label}
                    className="me-2 mb-0"
                    variant="tertiary"
                    size="sm"
                    icon="paperclip"
                    iconClassName="fs-8"
                    transform="down-2"
                  />
                </div>
              </OverlayTrigger>
            </Form.Group>
            <Form.Group controlId="email-image">
              <Form.Control
                type="file"
                className="d-none"
                accept="image/*"
                multiple
                onChange={(e) => handleAddAttachment((e.target as HTMLInputElement).files)}
              />
              <OverlayTrigger
                overlay={
                  <Tooltip style={{ position: 'fixed' }} id="previousTooltip">
                    Attach images
                  </Tooltip>
                }
              >
                <div>
                  <IconButton
                    as={Form.Label}
                    className="me-2 mb-0"
                    variant="tertiary"
                    size="sm"
                    icon="image"
                    iconClassName="fs-8"
                    transform="down-2"
                  />
                </div>
              </OverlayTrigger>
            </Form.Group>
          </Flex>
          <Flex>
            <CardDropdown icon="ellipsis-v">
              <div className="py-2">
                <Dropdown.Item href="#!">Print</Dropdown.Item>
                <Dropdown.Item href="#!">Check spelling</Dropdown.Item>
                <Dropdown.Item href="#!">Plain text mode</Dropdown.Item>
                <Dropdown.Divider />
                <Dropdown.Item className="text-danger">Archive</Dropdown.Item>
              </div>
            </CardDropdown>
            <OverlayTrigger
              overlay={
                <Tooltip style={{ position: 'fixed' }} id="previousTooltip">
                  Delete
                </Tooltip>
              }
            >
              <div>
                <IconButton
                  className="ms-2 mb-0"
                  variant="tertiary"
                  size="sm"
                  icon="trash"
                  onClick={() => {
                    setAttachments([]);
                    setShowForm && setShowForm(false);
                  }}
                />
              </div>
            </OverlayTrigger>
          </Flex>
        </Flex>
      </Card.Footer>
    </Card>
  );
};

export default Compose;
