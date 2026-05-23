import { useState, ChangeEvent } from 'react';
import { Card, Form, Spinner } from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import { useUpdateCurrentUserMutation } from 'store/api/authApi';
import { useAppSelector } from 'store/hooks';
import { selectUser } from 'store/slices/authSlice';
import { createCookie } from 'helpers/utils';

// Phase 5 — Language picker in user preferences.
//
// Optimistically flips i18next + the orkestra_admin_lang cookie, then
// PATCHes /v1/auth/operator/me. On failure we revert both sides and
// surface a toast. useLanguageSync stays the source of truth on
// subsequent loads (driven by the persisted user.language).
const LANG_COOKIE_NAME = 'orkestra_admin_lang';
const LANG_COOKIE_TTL_MS = 30 * 24 * 60 * 60 * 1000;

const SUPPORTED_LANGUAGES = ['en', 'it'] as const;
type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];

const isSupported = (value: string): value is SupportedLanguage =>
  (SUPPORTED_LANGUAGES as readonly string[]).includes(value);

const LanguageSettings: React.FC = () => {
  const { t, i18n } = useTranslation();
  const user = useAppSelector(selectUser);
  const [updateCurrentUser, { isLoading }] = useUpdateCurrentUserMutation();

  const initial: SupportedLanguage = isSupported(user?.language ?? '')
    ? (user!.language as SupportedLanguage)
    : isSupported(i18n.language)
      ? (i18n.language as SupportedLanguage)
      : 'en';

  const [value, setValue] = useState<SupportedLanguage>(initial);

  const handleChange = async (e: ChangeEvent<HTMLSelectElement>) => {
    const next = e.target.value;
    if (!isSupported(next) || next === value) return;

    const previous = value;
    setValue(next);
    await i18n.changeLanguage(next);
    createCookie(LANG_COOKIE_NAME, next, LANG_COOKIE_TTL_MS);

    try {
      await updateCurrentUser({ language: next }).unwrap();
      toast.success(t('settings.language.toastSaved'));
    } catch {
      setValue(previous);
      await i18n.changeLanguage(previous);
      createCookie(LANG_COOKIE_NAME, previous, LANG_COOKIE_TTL_MS);
      toast.error(t('settings.language.toastFailed'));
    }
  };

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.language.title')} />
      <Card.Body className="bg-body-tertiary">
        <p className="fs-9 text-muted mb-3">
          {t('settings.language.description')}
        </p>
        <Form.Group controlId="settings-language-select">
          <Form.Label className="fs-9 fw-semibold mb-2">
            {t('settings.language.selectLabel')}
          </Form.Label>
          <div className="d-flex align-items-center gap-2">
            <Form.Select
              size="sm"
              value={value}
              onChange={handleChange}
              disabled={isLoading}
              aria-label={t('settings.language.selectLabel')}
            >
              <option value="en">{t('settings.language.optionEn')}</option>
              <option value="it">{t('settings.language.optionIt')}</option>
            </Form.Select>
            {isLoading && (
              <span
                className="d-inline-flex align-items-center gap-1 text-muted fs-10"
                aria-live="polite"
              >
                <Spinner animation="border" size="sm" />
                {t('settings.language.saving')}
              </span>
            )}
          </div>
        </Form.Group>
      </Card.Body>
    </Card>
  );
};

export default LanguageSettings;
