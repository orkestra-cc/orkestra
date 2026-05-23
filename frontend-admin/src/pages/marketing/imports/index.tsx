// Imports audit list — read-only summary of every CSV/Excel/Odoo import
// that ran in this tenant. The wizard for kicking new imports lives at
// /marketing/imports/new. Table mechanics live in ./ImportsTable so this
// file stays a thin page shell.

import { Card } from 'react-bootstrap';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { useListMarketingImportsQuery } from 'store/api/marketingApi';
import ImportsTable from './ImportsTable';

const ImportsPage: React.FC = () => {
  const { t } = useTranslation();
  const { refetch } = useListMarketingImportsQuery(undefined);

  return (
    <>
      <div className="mb-3 d-flex justify-content-between align-items-center">
        <div>
          <h3 className="fw-normal mb-1">{t('marketing.imports.title')}</h3>
          <p className="fs-10 text-muted mb-0">
            {t('marketing.imports.list.subtitle')}
          </p>
        </div>
        <div className="d-flex gap-2">
          <Link to="/marketing/imports/new" className="btn btn-primary btn-sm">
            {t('marketing.imports.list.newImport')}
          </Link>
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          <ImportsTable onRefresh={() => refetch()} />
        </Card.Body>
      </Card>
    </>
  );
};

export default ImportsPage;
