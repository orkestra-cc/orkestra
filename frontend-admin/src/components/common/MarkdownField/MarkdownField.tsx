// MarkdownField — a textarea with a Markdown mini-toolbar and a Write /
// Preview toggle (GitHub-style). Purely controlled: the parent owns the
// string, whether through react-hook-form's Controller or plain useState.
//
// The preview goes through react-markdown WITHOUT rehype-raw, so raw HTML in
// the text is shown as text, never mounted — the same posture the public
// renderer of a Markdown field must keep. It also runs remark-breaks: a
// single newline becomes a <br> (GitHub-comment behaviour) because the
// operators writing here are not Markdown authors and a bare Enter that
// silently joins two lines is the first thing they trip on. Any public
// renderer of the same text must apply the same two rules, or preview and
// page disagree. The toolbar transforms live in markdownToolbar.ts so they
// stay unit-testable without a DOM.
//
// Write/Preview is local state on purpose: it is a widget inside one form
// field, not a page tab, so the url-tabs rule (tabs sync with the URL) does
// not apply — two Markdown fields on one page would otherwise fight over
// the same search param.

import { useEffect, useRef, useState } from 'react';
import { ButtonGroup, Form, Nav } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import classNames from 'classnames';
import type { IconProp } from '@fortawesome/fontawesome-svg-core';

import IconButton from 'components/common/IconButton';
import {
  applyMarkdownAction,
  type MarkdownAction,
  type TextSelection
} from './markdownToolbar';

interface MarkdownFieldProps {
  id: string;
  value: string;
  onChange: (next: string) => void;
  onBlur?: () => void;
  rows?: number;
  size?: 'sm' | 'lg';
  isInvalid?: boolean;
  disabled?: boolean;
}

type Mode = 'write' | 'preview';

const TOOLBAR: ReadonlyArray<{ action: MarkdownAction; icon: IconProp }> = [
  { action: 'bold', icon: 'bold' },
  { action: 'italic', icon: 'italic' },
  { action: 'heading', icon: 'heading' },
  { action: 'link', icon: 'link' },
  { action: 'list', icon: 'list-ul' },
  { action: 'quote', icon: 'quote-left' }
];

const MarkdownField: React.FC<MarkdownFieldProps> = ({
  id,
  value,
  onChange,
  onBlur,
  rows = 6,
  size,
  isInvalid = false,
  disabled = false
}) => {
  const { t } = useTranslation();
  const [mode, setMode] = useState<Mode>('write');
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  // Selection to restore once the controlled value has re-rendered — set by
  // a toolbar click, consumed by the effect below.
  const pendingSelection = useRef<TextSelection | null>(null);

  useEffect(() => {
    const sel = pendingSelection.current;
    const ta = textareaRef.current;
    if (!sel || !ta) return;
    pendingSelection.current = null;
    ta.focus();
    ta.setSelectionRange(sel.start, sel.end);
  }, [value]);

  const apply = (action: MarkdownAction) => {
    const ta = textareaRef.current;
    const sel: TextSelection = ta
      ? { start: ta.selectionStart, end: ta.selectionEnd }
      : { start: value.length, end: value.length };
    const next = applyMarkdownAction(value, action, sel);
    pendingSelection.current = { start: next.start, end: next.end };
    onChange(next.text);
  };

  return (
    <div>
      <div
        className={classNames('border rounded', {
          'border-danger': isInvalid
        })}
      >
        <div className="d-flex align-items-center justify-content-between flex-wrap gap-1 px-2 py-1 border-bottom bg-body-tertiary rounded-top">
          <ButtonGroup size="sm" aria-label={t('common.markdownField.toolbar')}>
            {TOOLBAR.map(({ action, icon }) => (
              <IconButton
                key={action}
                type="button"
                variant="link"
                className="text-body px-2 py-0"
                icon={icon}
                title={t(`common.markdownField.${action}`)}
                aria-label={t(`common.markdownField.${action}`)}
                disabled={disabled || mode !== 'write'}
                onClick={() => apply(action)}
              />
            ))}
          </ButtonGroup>
          <Nav
            variant="pills"
            role="tablist"
            className="fs-10"
            activeKey={mode}
            onSelect={k => setMode((k as Mode) ?? 'write')}
          >
            <Nav.Item>
              <Nav.Link eventKey="write" className="py-0 px-2">
                {t('common.markdownField.write')}
              </Nav.Link>
            </Nav.Item>
            <Nav.Item>
              <Nav.Link eventKey="preview" className="py-0 px-2">
                {t('common.markdownField.preview')}
              </Nav.Link>
            </Nav.Item>
          </Nav>
        </div>
        {mode === 'write' ? (
          <Form.Control
            id={id}
            as="textarea"
            ref={textareaRef}
            rows={rows}
            size={size}
            value={value}
            disabled={disabled}
            isInvalid={isInvalid}
            className="border-0 rounded-0 rounded-bottom shadow-none"
            onChange={e => onChange(e.target.value)}
            onBlur={onBlur}
          />
        ) : (
          <div
            className="px-3 py-2 markdown-preview"
            data-testid={`${id}-preview`}
          >
            {value.trim() ? (
              <ReactMarkdown remarkPlugins={[remarkBreaks]}>
                {value}
              </ReactMarkdown>
            ) : (
              <span className="text-body-tertiary fst-italic">
                {t('common.markdownField.empty')}
              </span>
            )}
          </div>
        )}
      </div>
      <Form.Text className="text-body-tertiary">
        {t('common.markdownField.hint')}
      </Form.Text>
    </div>
  );
};

export default MarkdownField;
