// CSV export button for any AdvanceTable. Reads the currently *filtered
// and sorted* row model from the AdvanceTable context (so what the
// operator sees in the table is what lands in the file), maps each row
// to a flat dictionary via the caller-supplied `buildRow`, and triggers
// a browser download via utils/csvExport.
//
// Must be rendered inside an <AdvanceTableProvider>.

import { useTranslation } from 'react-i18next';
import type { Row } from '@tanstack/react-table';
import { useAdvanceTableContext } from 'providers/AdvanceTableProvider';
import IconButton from 'components/common/IconButton';
import {
  arrayToCSV,
  downloadCSV,
  generateTimestampedFilename
} from 'utils/csvExport';

interface ExportCsvButtonProps<T> {
  // Base name passed to generateTimestampedFilename (no extension, no
  // date suffix — they are appended by the util).
  filename: string;
  // Per-row projection. The Record's *insertion order* defines the
  // CSV column order (arrayToCSV walks Object.values), and the keys
  // become the header row.
  buildRow: (row: T) => Record<string, string | number>;
  // Optional explicit header order — used when buildRow may omit keys
  // on some rows (e.g. optional fields). Defaults to the first row's
  // own keys.
  headers?: string[];
  // Optional override for the button label (defaults to table.exportCsv).
  label?: string;
}

const ExportCsvButton = <T,>({
  filename,
  buildRow,
  headers,
  label
}: ExportCsvButtonProps<T>) => {
  const { t } = useTranslation();
  const { getFilteredRowModel } = useAdvanceTableContext();

  const onClick = () => {
    const tableRows = getFilteredRowModel().rows as Row<T>[];
    const rows = tableRows.map(r => buildRow(r.original));
    const cols = headers ?? (rows[0] ? Object.keys(rows[0]) : []);
    const csv = arrayToCSV(rows, cols);
    downloadCSV(csv, generateTimestampedFilename(filename));
  };

  return (
    <IconButton
      variant="orkestra-default"
      size="sm"
      icon="external-link-alt"
      transform="shrink-3"
      iconAlign="middle"
      onClick={onClick}
    >
      <span className="d-none d-sm-inline-block ms-1">
        {label ?? t('table.exportCsv')}
      </span>
    </IconButton>
  );
};

export default ExportCsvButton;
