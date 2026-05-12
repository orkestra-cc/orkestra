import Dropzone, { DropzoneOptions, FileWithPath } from 'react-dropzone';
import classNames from 'classnames';
import { isIterableArray } from 'helpers/utils';
import Flex from './Flex';
import cloudUpload from 'assets/img/icons/cloud-upload.svg';

export interface CustomFile {
  id?: string;
  path?: string;
  base64?: string;
  src?: string;
  size?: number;
  type?: string;
}

interface OrkestraDropzoneProps extends Omit<DropzoneOptions, 'onDrop'> {
  className?: string;
  onChange: (files: CustomFile[]) => void;
  files?: CustomFile[];
  preview?: boolean;
  placeholder?: React.ReactNode;
  [key: string]: any;
}

const getSize = (size: number) => {
  if (size < 1024) {
    return (
      <>
        <strong>{size}</strong> Byte
      </>
    );
  } else if (size < 1024 * 1024) {
    return (
      <>
        <strong>{(size / 1024).toFixed(2)}</strong> KB
      </>
    );
  } else {
    return (
      <>
        <strong>{(size / (1024 * 1024)).toFixed(2)}</strong> MB
      </>
    );
  }
};

const OrkestraDropzone = ({
  placeholder = <img src={cloudUpload} alt="" width={25} className="me-2" />,
  className,
  onChange,
  files = [],
  preview = false,
  ...rest
}: OrkestraDropzoneProps) => (
  <>
    <Dropzone
      {...rest}
      onDrop={(acceptedFiles: FileWithPath[]) => {
        const stringFiles: CustomFile[] = [];
        if (acceptedFiles.length) {
          acceptedFiles.map(file => {
            const reader = new FileReader();
            reader.readAsDataURL(file);
            reader.onload = () => {
              stringFiles.push({
                base64: reader.result as string,
                size: file.size,
                path: file.path as string,
                type: file.type
              });
              onChange([...stringFiles]);
            };
            return true;
          });
        }
      }}
    >
      {({ getRootProps, getInputProps }) => (
        <div
          {...getRootProps({
            className: classNames('dropzone-area', className)
          })}
        >
          <input {...getInputProps()} />
          {placeholder}
        </div>
      )}
    </Dropzone>
    {preview && isIterableArray(files) && (
      <div className="border-top mt-3">
        {files.map(({ id, path, base64, size }) => (
          <Flex
            alignItems="center"
            className="py-3 border-bottom btn-reveal-trigger"
            key={id}
          >
            <img className="img-fluid" width={38} src={base64} alt={path} />
            <Flex justifyContent="between" alignItems="center" className="ms-3">
              <div>
                <h6 data-dz-name="">{path}</h6>
                <Flex className="position-relative" alignItems="center">
                  <p className="mb-0 fs-10 text-400 line-height-1">
                    {size !== undefined && getSize(size)}
                  </p>
                </Flex>
              </div>
              {/* <UncontrolledDropdown className="font-sans-serif">
                <DropdownToggle color="link" size="sm" className="text-600 btn-reveal">
                  <FontAwesomeIcon icon="ellipsis-h" />
                </DropdownToggle>
                <DropdownMenu className="border py-0" right>
                  <div className="bg-white py-2">
                    <DropdownItem
                      className="text-danger"
                      onClick={() => onChange(files.filter(file => file.id !== id))}
                    >
                      Remove File
                    </DropdownItem>
                  </div>
                </DropdownMenu>
              </UncontrolledDropdown> */}
            </Flex>
          </Flex>
        ))}
      </div>
    )}
  </>
);

export default OrkestraDropzone;
