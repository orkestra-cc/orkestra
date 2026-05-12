import * as React from 'react';
import * as reactBootstrap from 'react-bootstrap';
import { LiveEditor, LiveError, LivePreview, LiveProvider } from 'react-live';
import { themes, PrismTheme } from 'prism-react-renderer';
import classNames from 'classnames';

interface OrkestraEditorProps {
  code: string;
  scope?: any;
  language?: string;
  hidePreview?: boolean;
  theme?: PrismTheme;
  className?: string;
}

const OrkestraEditor = ({
  code,
  scope,
  language = 'markup',
  hidePreview = false,
  theme = themes.okaidia,
  className
}: OrkestraEditorProps) => {
  const importRegex =
    /import(?:["'\s]*([\w*{}\n, ]+)from\s*)["'\s]*([@\w/_-]+)["'\s]*;?/gm;
  const requireRegex =
    /(const|let|var)\s*([\w{}\n, ]+\s*)=\s*require\s*\(["'\s]*([@\w/_-]+)["'\s]*\s*\);?/gm;
  const imports = {
    CardDropdown: 'CardDropdown '
  };

  const transformCode = (code: string) => {
    return code
      .replace(importRegex, (match: string, p1: string, p2: string) => {
        const matchingImport = imports[p2 as keyof typeof imports];
        if (!matchingImport) {
          // leave it alone if we don't have a matching import
          return match;
        }

        return 'var ' + p1 + ' = ' + matchingImport + ';';
      })
      .replace(
        requireRegex,
        (match: string, p1: string, p2: string, p3: string) => {
          const matchingImport = imports[p3 as keyof typeof imports];
          if (!matchingImport) {
            // leave it alone if we don't have a matching import
            return match;
          }

          return p1 + ' ' + p2 + ' = ' + matchingImport + ';';
        }
      );
  };

  return (
    <LiveProvider
      theme={theme}
      language={language}
      scope={{ ...reactBootstrap, ...React, ...scope }}
      code={code}
      disabled={hidePreview}
      transformCode={transformCode}
    >
      {!hidePreview && <LivePreview className="mb-3" />}
      <div dir="ltr">
        <LiveEditor
          className={classNames('rounded border-top border-bottom', className)}
        />
      </div>
      {!hidePreview && <LiveError />}
    </LiveProvider>
  );
};

export default OrkestraEditor;
