import React, { ReactNode, FC } from 'react';
import * as ReactBootstrap from 'react-bootstrap';
import { LiveEditor, LiveError, LivePreview, LiveProvider } from 'react-live';
import { themes } from 'prism-react-renderer';
import classNames from 'classnames';

type ChildrenPosition = 'top' | 'bottom';

interface OrkestraCardBodyProps {
  code?: string;
  scope?: Record<string, unknown>;
  language?: string;
  hidePreview?: boolean;
  children?: ReactNode;
  noInline?: boolean;
  noLight?: boolean;
  className?: string;
  childrenPosition?: ChildrenPosition;
}

const OrkestraCardBody: FC<OrkestraCardBodyProps> = ({
  code,
  scope,
  language = 'jsx',
  hidePreview,
  children,
  noInline,
  noLight,
  className,
  childrenPosition
}) => {
  return (
    <ReactBootstrap.Card.Body
      className={classNames(className, {
        'bg-body-tertiary': !noLight
      })}
    >
      <LiveProvider
        theme={themes.okaidia}
        language={language}
        scope={{ ...ReactBootstrap, ...React, ...scope }}
        code={code}
        disabled={hidePreview}
        noInline={noInline}
        transformCode={code => code.replace(/^import.*$/gm, '')}
      >
        <ReactBootstrap.Tab.Content>
          <ReactBootstrap.Tab.Pane eventKey="preview">
            {childrenPosition !== 'bottom' && children}
            {!hidePreview && <LivePreview />}
            {childrenPosition === 'bottom' && children}
          </ReactBootstrap.Tab.Pane>
          <ReactBootstrap.Tab.Pane
            eventKey="code"
            className="overflow-auto scrollbar"
          >
            <div style={{ maxHeight: '25rem' }}>
              <div dir="ltr">
                <LiveEditor className="rounded" />
              </div>
              {!hidePreview && <LiveError />}
            </div>
          </ReactBootstrap.Tab.Pane>
        </ReactBootstrap.Tab.Content>
      </LiveProvider>
    </ReactBootstrap.Card.Body>
  );
};

export default OrkestraCardBody;
