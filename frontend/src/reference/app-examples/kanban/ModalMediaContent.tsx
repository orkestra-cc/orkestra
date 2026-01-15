import Flex from 'components/common/Flex';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { IconProp } from '@fortawesome/fontawesome-svg-core';
import classNames from 'classnames';
import { ReactNode } from 'react';

interface ModalMediaContentProps {
  children: ReactNode;
  icon: IconProp;
  transform?: string;
  title: string;
  headingClass?: string;
  headingContent?: ReactNode;
  isHr?: boolean;
}

const ModalMediaContent = ({
  children,
  icon,
  transform = '',
  title,
  headingClass,
  headingContent,
  isHr = true
}: ModalMediaContentProps) => {
  return (
    <Flex>
      <span className="fa-stack ms-n1 me-3">
        <FontAwesomeIcon icon="circle" className="text-200 fa-stack-2x" />
        <FontAwesomeIcon
          icon={icon}
          transform={`shrink-2 ${transform}`}
          className="text-primary fa-stack-1x"
          inverse
        />
      </span>
      <div className="flex-1">
        <Flex className={classNames('mb-2', headingClass)}>
          <h5 className="mb-0 fs-9">{title}</h5>
          {headingContent && headingContent}
        </Flex>
        {children}
        {isHr && <hr className="my-4" />}
      </div>
    </Flex>
  );
};

export default ModalMediaContent;
