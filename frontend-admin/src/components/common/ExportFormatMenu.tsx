// ExportFormatMenu — the "Export ▾ → CSV | Excel" dropdown, optionally
// split in several scopes (e.g. "current view" vs "everything"). Owns only
// the busy state; the caller owns data, file writing and error toasts, which
// differ per use (client-side writers in CRM segments, a server stream in
// forms). Promoted from crm/segments/SegmentExportMenu at its second use.

import { useState } from 'react';
import { Dropdown } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';

export type ExportMenuFormat = 'csv' | 'xlsx';

export interface ExportScope {
  key: string;
  /** Already translated, counts included ("Vista corrente (12)"). */
  label: string;
  disabled?: boolean;
}

export interface ExportFormatMenuProps {
  scopes: ExportScope[];
  onExport: (scope: string, format: ExportMenuFormat) => Promise<void>;
  disabled?: boolean;
  size?: 'sm';
  labels: { menu: string; exporting: string; csv: string; xlsx: string };
  /** Test hook / analytics id on the toggle. */
  id?: string;
}

const FORMATS: ExportMenuFormat[] = ['csv', 'xlsx'];

const ExportFormatMenu: React.FC<ExportFormatMenuProps> = ({
  scopes,
  onExport,
  disabled,
  size,
  labels,
  id
}) => {
  const [busy, setBusy] = useState(false);

  const run = async (scope: string, format: ExportMenuFormat) => {
    setBusy(true);
    try {
      await onExport(scope, format);
    } catch {
      // The caller owns the error (toast copy differs per use/error code);
      // this primitive only needs `busy` to reset in `finally` below.
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dropdown>
      <Dropdown.Toggle
        id={id}
        variant="orkestra-default"
        size={size}
        disabled={disabled || busy}
      >
        <FontAwesomeIcon icon="file-export" className="me-1" />
        {busy ? labels.exporting : labels.menu}
      </Dropdown.Toggle>
      <Dropdown.Menu align="end">
        {scopes.map((scope, i) => (
          <div key={scope.key}>
            {scopes.length > 1 && (
              <>
                {i > 0 && <Dropdown.Divider />}
                <Dropdown.Header>{scope.label}</Dropdown.Header>
              </>
            )}
            {FORMATS.map(format => (
              <Dropdown.Item
                key={format}
                as="button"
                type="button"
                disabled={scope.disabled}
                onClick={() => run(scope.key, format)}
              >
                {labels[format]}
              </Dropdown.Item>
            ))}
          </div>
        ))}
      </Dropdown.Menu>
    </Dropdown>
  );
};

export default ExportFormatMenu;
