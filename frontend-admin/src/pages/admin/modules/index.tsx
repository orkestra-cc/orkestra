import { useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Card, Col, Row, Tab, Tabs } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { useGetModulesQuery } from 'store/api/moduleApi';
import ModuleTable from './ModuleTable';

const ModuleManagementPage: React.FC = () => {
  const { t } = useTranslation();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get('tab') || 'core';
  const { data: modules } = useGetModulesQuery();

  const stats = useMemo(() => {
    if (!modules) return null;
    return {
      total: modules.length,
      running: modules.filter(m => m.status === 'running').length,
      failed: modules.filter(m => m.status === 'failed').length,
      disabled: modules.filter(m => m.status === 'disabled').length,
      stopped: modules.filter(m => m.status === 'stopped').length
    };
  }, [modules]);

  return (
    <Row className="g-3">
      <Col xxl={12}>
        <Card className="mb-3">
          <Card.Body className="py-3 px-4">
            <div className="d-flex align-items-center justify-content-between flex-wrap">
              <div>
                <h5 className="mb-0">{t('adminModules.pageTitle')}</h5>
              </div>
              {stats && (
                <div className="d-flex gap-3 fs-10 text-600">
                  <span>
                    <strong className="text-900">
                      {t('adminModules.modulesCount', { count: stats.total })}
                    </strong>
                  </span>
                  <span>
                    <span
                      className="rounded-circle bg-success d-inline-block me-1"
                      style={{ width: 8, height: 8 }}
                    />
                    {stats.running} {t('adminModules.status.running')}
                  </span>
                  {stats.failed > 0 && (
                    <span>
                      <span
                        className="rounded-circle bg-danger d-inline-block me-1"
                        style={{ width: 8, height: 8 }}
                      />
                      {stats.failed} {t('adminModules.status.failed')}
                    </span>
                  )}
                  {stats.disabled > 0 && (
                    <span>
                      <span
                        className="rounded-circle bg-400 d-inline-block me-1"
                        style={{ width: 8, height: 8 }}
                      />
                      {stats.disabled} {t('adminModules.status.disabled')}
                    </span>
                  )}
                  {stats.stopped > 0 && (
                    <span>
                      <span
                        className="rounded-circle bg-warning d-inline-block me-1"
                        style={{ width: 8, height: 8 }}
                      />
                      {stats.stopped} {t('adminModules.status.stopped')}
                    </span>
                  )}
                </div>
              )}
            </div>
          </Card.Body>
        </Card>

        <Tabs
          id="module-management-tabs"
          activeKey={tab}
          onSelect={k => {
            if (!k) return;
            setSearchParams(
              prev => {
                prev.set('tab', k);
                return prev;
              },
              { replace: true }
            );
          }}
          className="mb-3"
        >
          <Tab eventKey="core" title={t('adminModules.tabs.core')}>
            <ModuleTable scope="core" title={t('adminModules.tabs.core')} />
          </Tab>
          <Tab eventKey="addons" title={t('adminModules.tabs.addons')}>
            <ModuleTable scope="addons" title={t('adminModules.tabs.addons')} />
          </Tab>
        </Tabs>
      </Col>
    </Row>
  );
};

export default ModuleManagementPage;
