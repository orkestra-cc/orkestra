// src/reference/components/ui/ExportFormatMenus.tsx
// Reference showcase for the shared ExportFormatMenu primitive
// (components/common/ExportFormatMenu): the "Export ▾ → CSV | Excel"
// dropdown, optionally split into several scopes (a "current view" vs
// "everything" section pair). Production precedent: pages/crm/segments/
// SegmentExportMenu.tsx (single scope, its second use — the tab in
// pages/forms/detail/ — motivated promoting the presentation here). The
// primitive owns only the busy state; the caller owns data, file writing
// and error toasts.

import { Card } from 'react-bootstrap';
import PageHeader from 'components/common/PageHeader';
import ExportFormatMenu from 'components/common/ExportFormatMenu';

const wait = () => new Promise<void>(r => setTimeout(r, 800));

const labels = {
  menu: 'Esporta',
  exporting: 'Esportazione…',
  csv: 'CSV',
  xlsx: 'Excel'
};

const ExportFormatMenus = () => (
  <>
    <PageHeader
      title="Export Format Menu"
      description="Dropdown CSV / Excel, a uno o più ambiti. Il chiamante scrive il file; la primitiva gestisce solo lo stato busy."
      className="mb-3"
    />
    <Card className="mb-3">
      <Card.Header>
        <h5 className="mb-0">Ambito singolo</h5>
      </Card.Header>
      <Card.Body>
        <p className="text-body-tertiary fs-10 mb-3">
          Un solo scope in <code>scopes</code> → nessun{' '}
          <code>Dropdown.Header</code>. Questo è il caso di{' '}
          <code>SegmentExportMenu</code> nel CRM.
        </p>
        <ExportFormatMenu
          scopes={[{ key: 'all', label: '' }]}
          onExport={wait}
          labels={labels}
        />
      </Card.Body>
    </Card>
    <Card>
      <Card.Header>
        <h5 className="mb-0">Due ambiti con conteggi</h5>
      </Card.Header>
      <Card.Body>
        <p className="text-body-tertiary fs-10 mb-3">
          Più scope → ciascuno rende un <code>Dropdown.Header</code> col
          proprio conteggio; uno scope può essere <code>disabled</code>{' '}
          (righe della vista corrente = 0).
        </p>
        <ExportFormatMenu
          scopes={[
            { key: 'view', label: 'Vista corrente (12)' },
            { key: 'all', label: 'Tutte le iscrizioni (340)' }
          ]}
          onExport={wait}
          labels={labels}
          size="sm"
        />
      </Card.Body>
    </Card>
  </>
);

export default ExportFormatMenus;
