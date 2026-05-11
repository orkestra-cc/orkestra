import { useState } from 'react';
import { Button, Dropdown, Form } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import FalconComponentCard from 'components/common/FalconComponentCard';
import { getSize } from 'helpers/utils';
import Flex from 'components/common/Flex';
import { useDropzone } from 'react-dropzone';
import * as yup from 'yup';
import { yupResolver } from '@hookform/resolvers/yup';
import { useForm } from 'react-hook-form';
import classNames from 'classnames';
import CardDropdown from 'components/common/CardDropdown';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';

interface FileWithPreview extends File {
  preview: string;
  path?: string;
}

const exampleCode = `import { useDropzone } from 'react-dropzone';
import Flex from 'components/common/Flex';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';

function Example() {
  const { acceptedFiles, getRootProps, getInputProps } = useDropzone();

  const files = acceptedFiles.map(file => (
    <li key={file.path}>
      {file.path} - {file.size} bytes
    </li>
  ));

  return (
    <>
      <div {...getRootProps({ className: 'dropzone-area py-6' })}>
        <input {...getInputProps()} />
        <Flex justifyContent="center">
          <img src={cloudUpload} alt="" width={25} className="me-2" />
          <p className="fs-9 mb-0 text-700">Drop your files here</p>
        </Flex>
      </div>
      <div className="mt-3">
        {acceptedFiles.length > 0 && (
          <>
            <h6>Files</h6>
            <ul>{files}</ul>
          </>
        )}
      </div>
    </>
  );
}`;

const singleFileCode = `import { useDropzone } from 'react-dropzone';
import Flex from 'components/common/Flex';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';

function SingleFileExample() {
  const { acceptedFiles, getRootProps, getInputProps } = useDropzone();

  const files = acceptedFiles.map(file => (
    <li key={file.path}>
      {file.path} - {file.size} bytes
    </li>
  ));

  return (
    <>
      <div {...getRootProps({ className: 'dropzone-area py-6' })}>
        <input {...getInputProps({multiple:false})} />
        <Flex justifyContent="center">
          <img src={cloudUpload} alt="" width={25} className="me-2" />
          <p className="fs-9 mb-0 text-700">Drop your file here</p>
        </Flex>
      </div>
      <div className="mt-3">
        {acceptedFiles.length > 0 && (
          <>
            <h6>File</h6>
            <ul>{files}</ul>
          </>
        )}
      </div>
    </>
  );
}`;

const previewCode = `import { useDropzone } from 'react-dropzone';
import Flex from 'components/common/Flex';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';
import CardDropdown from 'components/common/CardDropdown';
import Images from 'components/bootstrap-components/Image';
import { getSize } from 'helpers/utils';
import { Dropdown } from 'react-bootstrap';


function PreviewExample() {
  const [files, setFiles] = React.useState([]);

  const { getRootProps, getInputProps } = useDropzone({
    accept: {
      'image/*': ['.jpeg', '.png', '.jpg']
    },
    onDrop: acceptedFiles => {
      setFiles(
        acceptedFiles.map(file =>
          Object.assign(file, {
            preview: URL.createObjectURL(file)
          })
        )
      );
    }
  });

  const handleRemove = path => {
    setFiles(files.filter(file => file.path !== path));
  };

  return (
    <>
      <div {...getRootProps({ className: 'dropzone-area py-6' })}>
        <input {...getInputProps()} />
        <Flex justifyContent="center">
          <img src={cloudUpload} alt="" width={25} className="me-2" />
          <p className="fs-9 mb-0 text-700">Drop your images here</p>
        </Flex>
      </div>

      <div className="mt-3">
        {files.map(file => (
          <Flex
            alignItems="center"
            className="py-3 border-bottom btn-reveal-trigger"
            key={file.path}
          >
            <Image rounded width={40} height={40} src={file.preview} alt={file.path} />
            <Flex justifyContent="between" alignItems="center" className="ms-3 flex-1">
              <div>
                <h6>{file.path}</h6>
                <Flex className="position-relative" alignItems="center">
                  <p className="mb-0 fs-10 text-400 line-height-1">
                    <strong>
                      {getSize(file.size)}
                    </strong>
                  </p>
                </Flex>
              </div>
            </Flex>
            <CardDropdown>
              <div className="py-2">
                <Dropdown.Item className="text-danger" onClick={() => handleRemove(file.path)}>
                  Remove
                </Dropdown.Item>
              </div>
            </CardDropdown>
          </Flex>
        ))}
      </div>
    </>
  );
}`;

const validationCode = `import React, { useState } from 'react';
import { useDropzone } from 'react-dropzone';
import { Button, Dropdown, Form } from 'react-bootstrap';
import { yupResolver } from '@hookform/resolvers/yup';
import { useForm } from 'react-hook-form';
import classNames from 'classnames';
import { getSize } from 'helpers/utils';
import Flex from 'components/common/Flex';
import CardDropdown from 'components/common/CardDropdown';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';


function FileUploadValidation() {
  const schema = yup
    .object({
      uploadedFiles: yup.array().required('This field is required.')
    })
    .required();
  const {
    handleSubmit,
    setValue,
    formState: { errors },
    clearErrors
  } = useForm({
    resolver: yupResolver(schema)
  });
  const [files, setFiles] = useState([]);

  const { getRootProps, getInputProps } = useDropzone({
    accept: {
      'image/*': ['.jpeg', '.png', '.jpg']
    },
    onDrop: acceptedFiles => {
      if (errors.uploadedFiles) {
        clearErrors('uploadedFiles');
      }
      setValue('uploadedFiles', acceptedFiles);
      setFiles([
        ...files,
        ...acceptedFiles.map(file =>
          Object.assign(file, {
            preview: URL.createObjectURL(file)
          })
        )
      ]);
    }
  });

  const handleRemove = path => {
    const filteredFiles = files.filter(file => file.path !== path);
    setFiles(filteredFiles);
    setValue('uploadedFiles', filteredFiles.length ? filteredFiles : null);
  };

  const onSubmit = data => {
    console.log(data);
  };

  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <div
        {...getRootProps({
          className: \`\${classNames('dropzone-area py-6', {
            'is-invalid': errors.uploadedFiles
          })}\`
        })}
      >
        <input name="uploadedFiles" {...getInputProps()} />
        <Flex justifyContent="center">
          <img src={cloudUpload} alt="" width={25} className="me-2" />
          <p className="fs-9 mb-0 text-700">Drop your images here</p>
        </Flex>
      </div>
      <div className="invalid-feedback">{errors.uploadedFiles?.message}</div>
      <div>
        {files.map(file => (
          <Flex
            alignItems="center"
            className="py-3 border-bottom btn-reveal-trigger"
            key={file.path}
          >
            <img
              className="rounded"
              width={40}
              height={40}
              src={file.preview}
              alt={file.path}
            />

            <Flex
              justifyContent="between"
              alignItems="center"
              className="ms-3 flex-1"
            >
              <div>
                <h6>{file.path}</h6>
                <Flex className="position-relative" alignItems="center">
                  <p className="mb-0 fs-10 text-400 line-height-1">
                    <strong>{getSize(file.size)}</strong>
                  </p>
                </Flex>
              </div>
            </Flex>
            <CardDropdown>
              <div className="py-2">
                <Dropdown.Item
                  className="text-danger"
                  onClick={() => handleRemove(file.path)}
                >
                  Remove
                </Dropdown.Item>
              </div>
            </CardDropdown>
          </Flex>
        ))}
      </div>
      <Button variant="falcon-primary" type="submit" className="me-2" size="sm">
        Submit
      </Button>
    </Form>
  );
};
`;

const FileUploader = () => (
  <>
    <PageHeader
      title="File Uploader"
      description="Falcon-React uses <strong>React Dropzone</strong> for file-uploader. <strong>React Dropzone</strong> provides a simple react hook to create a HTML5-compliant drag'n'drop zone for files."
      className="mb-3"
    >
      <Button
        href="https://react-dropzone.js.org/"
        target="_blank"
        variant="link"
        size="sm"
        className="ps-0"
      >
        React Dropzone Documentation
        <FontAwesomeIcon icon="chevron-right" className="ms-1 fs-11" />
      </Button>
    </PageHeader>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Example" light={false} />
      <FalconComponentCard.Body
        code={exampleCode}
        language="jsx"
        scope={{ useDropzone, Flex, cloudUpload }}
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Single File Upload" light={false} />
      <FalconComponentCard.Body
        code={singleFileCode}
        language="jsx"
        scope={{ useDropzone, Flex, cloudUpload }}
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="Image Preview" light={false} />
      <FalconComponentCard.Body
        code={previewCode}
        language="jsx"
        scope={{ useDropzone, Flex, cloudUpload, CardDropdown, getSize }}
      />
    </FalconComponentCard>

    <FalconComponentCard>
      <FalconComponentCard.Header title="With Validation" light={false} />
      <FalconComponentCard.Body
        code={validationCode}
        language="jsx"
        hidePreview
      >
        <FileUploadValidation />
      </FalconComponentCard.Body>
    </FalconComponentCard>
  </>
);

export default FileUploader;

const FileUploadValidation = () => {
  const schema = yup
    .object({
      uploadedFiles: yup.array().required('This field is required.')
    })
    .required();
  const {
    handleSubmit,
    setValue,
    formState: { errors },
    clearErrors
  } = useForm({
    resolver: yupResolver(schema)
  });
  const [files, setFiles] = useState<FileWithPreview[]>([]);

  const { getRootProps, getInputProps } = useDropzone({
    accept: {
      'image/*': ['.jpeg', '.png', '.jpg']
    },
    onDrop: acceptedFiles => {
      if (errors.uploadedFiles) {
        clearErrors('uploadedFiles');
      }
      setValue('uploadedFiles', acceptedFiles);
      setFiles([
        ...files,
        ...acceptedFiles.map(file =>
          Object.assign(file, {
            preview: URL.createObjectURL(file)
          }) as FileWithPreview
        )
      ]);
    }
  });

  const handleRemove = (path: string) => {
    const filteredFiles = files.filter(file => file.path !== path);
    setFiles(filteredFiles);
    setValue('uploadedFiles', filteredFiles.length ? filteredFiles : []);
  };

  const onSubmit = (data: { uploadedFiles?: File[] }) => {
    console.log(data);
  };

  return (
    <Form onSubmit={handleSubmit(onSubmit)}>
      <div
        {...getRootProps({
          className: `${classNames('dropzone-area py-6', {
            'is-invalid': errors.uploadedFiles
          })}`
        })}
      >
        <input name="uploadedFiles" {...getInputProps()} />
        <Flex justifyContent="center">
          <img src={cloudUpload} alt="" width={25} className="me-2" />
          <p className="fs-9 mb-0 text-700">Drop your images here</p>
        </Flex>
      </div>
      <div className="invalid-feedback">{errors.uploadedFiles?.message}</div>
      <div>
        {files.map(file => (
          <Flex
            alignItems="center"
            className="py-3 border-bottom btn-reveal-trigger"
            key={file.path}
          >
            <img
              className="rounded"
              width={40}
              height={40}
              src={file.preview}
              alt={file.path}
            />

            <Flex
              justifyContent="between"
              alignItems="center"
              className="ms-3 flex-1"
            >
              <div>
                <h6>{file.path}</h6>
                <Flex className="position-relative" alignItems="center">
                  <p className="mb-0 fs-10 text-400 line-height-1">
                    <strong>{getSize(file.size)}</strong>
                  </p>
                </Flex>
              </div>
            </Flex>
            <CardDropdown>
              <div className="py-2">
                <Dropdown.Item
                  className="text-danger"
                  onClick={() => handleRemove(file.path || '')}
                >
                  Remove
                </Dropdown.Item>
              </div>
            </CardDropdown>
          </Flex>
        ))}
      </div>
      <Button variant="primary" type="submit" className="mt-3" size="sm">
        Submit
      </Button>
    </Form>
  );
};
