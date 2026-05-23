import { useTranslation } from 'react-i18next';
import { useAppDispatch, useAppSelector } from 'store/hooks';
import {
  selectImpersonation,
  selectIsImpersonating,
  stopImpersonation
} from 'store/slices/tenantSlice';
import { baseApi, TENANT_SCOPED_TAGS } from 'store/api/baseApi';

/**
 * Thin warning bar that renders only while an operator admin is
 * impersonating another tenant via NineDotMenu / ImpersonateButton. Visible
 * on every authenticated page so the admin cannot forget they're acting
 * inside someone else's scope.
 */
export default function ImpersonationBanner() {
  const { t } = useTranslation();
  const dispatch = useAppDispatch();
  const isImpersonating = useAppSelector(selectIsImpersonating);
  const { tenantId, tenantName } = useAppSelector(selectImpersonation);

  if (!isImpersonating) {
    return null;
  }

  const onStop = () => {
    dispatch(stopImpersonation());
    // Invalidate per-tenant tags only — preserve the session cache so
    // ProtectedRoute doesn't bounce to /login during the re-render.
    dispatch(baseApi.util.invalidateTags([...TENANT_SCOPED_TAGS]));
  };

  return (
    <div
      role="alert"
      className="d-flex align-items-center justify-content-between px-3 py-2 border-bottom"
      style={{
        background: '#fff3cd',
        color: '#664d03',
        fontSize: '0.875rem'
      }}
    >
      <span>
        <strong>
          ⚠{' '}
          {t('banner.impersonation.text', {
            tenant: tenantName ?? t('banner.impersonation.fallback')
          })}
        </strong>
        <span className="text-muted ms-2">({tenantId})</span>
        <span className="ms-2">{t('banner.impersonation.audit')}</span>
      </span>
      <button
        type="button"
        className="btn btn-sm btn-outline-dark"
        onClick={onStop}
      >
        {t('banner.impersonation.stop')}
      </button>
    </div>
  );
}
