import { useEffect, useRef, useState, FormEvent } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { Link, useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import resolveErrorMessage from 'helpers/errorMessage';
import OrkestraLoader from 'components/common/OrkestraLoader';
import { useAppDispatch } from 'store/hooks';
import { setMemberships } from 'store/slices/tenantSlice';
import { logout as logoutAction } from 'store/slices/authSlice';
import { useGetSessionQuery, useLogoutMutation } from 'store/api/authApi';
import {
  setupApi,
  useGetFinalizationAccessQuery,
  useFinalizeSetupMutation,
  type FinalizeSetupInput
} from 'store/api/setupApi';

interface OrgStepProps {
  /**
   * Called once the finalize saga reaches a terminal 200 AND the re-minted
   * access token (carrying the new org_owner membership) is confirmed in
   * the store — never before. `allowAdditional` mirrors the
   * backend-confirmed provisioning mode so FinishStep can summarize it
   * without re-deriving anything.
   */
  onNext: (tenantName: string, allowAdditional: boolean) => void;
}

/**
 * Slugify a human-readable org name the same way the backend expects:
 * lowercase, ASCII letters/digits only, hyphen-separated, trimmed.
 * The tenant module enforces slug uniqueness across the whole tenant
 * collection, so the user can override this if they hit a collision.
 */
const slugify = (input: string): string =>
  input
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '') // strip diacritics
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48) || 'default';

// The backend always sends Retry-After on the 202 (routes.go hardcodes
// "3"); this is only a defensive fallback for a response that omits it.
const DEFAULT_RETRY_AFTER_SECONDS = 3;

interface FinalizeErrorBody {
  code?: string;
  detail?: string;
}
interface FinalizeErrorShape {
  status?: number;
  data?: FinalizeErrorBody;
}

/**
 * Third step of the setup wizard: the initial Tier-1 organization is now
 * mandatory (no Skip). Renders an authenticated-operator "access boundary"
 * first — a restored session, then the finalization-access probe — before
 * ever showing the form, and drives the resumable finalize saga (200 / 202
 * / 403 / 409) once submitted. See backend/internal/shared/setup/service.go
 * (evaluateAccessDetailed) for the exact access-state contract this
 * mirrors, and setupApi.ts's finalizeSetup for the ordered session re-mint
 * this component waits on.
 */
const OrgStep = ({ onNext }: OrgStepProps) => {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const dispatch = useAppDispatch();

  // --- Auth boundary: restored session, then finalization access ---

  const {
    data: sessionData,
    isLoading: isSessionLoading,
    isFetching: isSessionFetching,
    requestId: sessionRequestId
  } = useGetSessionQuery();
  const isAuthenticated = !isSessionLoading && !!sessionData?.accessToken;

  // Sticky once true: the auth/access boundary below decides ONCE whether
  // this operator may even see the form. A session re-mint deliberately
  // simulated (or genuinely) failing mid-submission flips `sessionData` to
  // null the same way a first-load "never signed in" does — without this,
  // that transient blip would bounce a mid-flow operator back to the
  // sign-in prompt (and drop the finalization-access subscription, whose
  // `keepUnusedDataFor: 0` purges its cached data the instant it's
  // skipped) instead of landing in the recoverable `refreshingSession`
  // state the submit loop already handles.
  const hasAuthenticatedRef = useRef(false);
  if (isAuthenticated) hasAuthenticatedRef.current = true;
  const hasEverAuthenticated = hasAuthenticatedRef.current;

  const {
    data: access,
    isLoading: isAccessLoading,
    isError: isAccessError,
    error: accessError,
    refetch: refetchAccess
  } = useGetFinalizationAccessQuery(undefined, { skip: !hasEverAuthenticated });

  // If the probe itself reports setup.already_completed (409 — the phase
  // flipped to 'complete' concurrently), force a status refetch so the
  // gate above this wizard reacts and redirects. This never renders the
  // form; the neutral "unavailable" screen below covers this render pass.
  useEffect(() => {
    if (!isAccessError) return;
    const status = (accessError as FinalizeErrorShape | undefined)?.status;
    if (status === 409) {
      dispatch(
        setupApi.endpoints.getSetupStatus.initiate(undefined, {
          forceRefetch: true
        })
      );
    }
  }, [isAccessError, accessError, dispatch]);

  const [logoutMutation] = useLogoutMutation();

  const handleLogoutAndNavigate = async (returnToSetup: boolean) => {
    try {
      await logoutMutation().unwrap();
    } catch {
      // Fall through — local state is still cleared below regardless of
      // whether the server-side call succeeded.
    }
    dispatch(logoutAction());
    navigate(
      '/login',
      returnToSetup
        ? { replace: true, state: { from: location } }
        : { replace: true }
    );
  };
  const handleSwitchAccount = () => {
    void handleLogoutAndNavigate(true);
  };
  const handleLogout = () => {
    void handleLogoutAndNavigate(false);
  };

  // --- Form state (shape per the task brief) ---

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugTouched, setSlugTouched] = useState(false);
  const [allowAdditional, setAllowAdditional] = useState<boolean | null>(null);
  const [phaseState, setPhaseState] = useState<
    'idle' | 'submitting' | 'inProgress' | 'refreshingSession' | 'done'
  >('idle');
  const [formError, setFormError] = useState<string | null>(null);

  const [finalizeSetup] = useFinalizeSetupMutation();

  // The IDENTICAL payload every automatic (202) or manual (refreshingSession)
  // retry resubmits — captured once at the first submit, never re-derived
  // from the (locked) form fields on a later attempt.
  const pendingPayloadRef = useRef<FinalizeSetupInput | null>(null);
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Always mirrors the latest sessionRequestId so runFinalize — which may be
  // an older render's closure by the time an automatic retry fires — reads
  // the current value rather than a stale one captured at definition time.
  const sessionRequestIdRef = useRef(sessionRequestId);
  sessionRequestIdRef.current = sessionRequestId;

  // Session-re-mint watcher. finalizeSetup's onQueryStarted forces a
  // GET /v1/auth/session refetch on a 200 BEFORE invalidating anything —
  // but that happens on its own async continuation, independent of when
  // this component's own `.unwrap()` resolves (RTK Query does not await
  // onQueryStarted before settling the dispatched promise). The refetch it
  // triggers shares this component's own useGetSessionQuery subscription
  // above (same cache entry), so we detect it by watching for a NEW
  // requestId to become current (a fresh fetch was dispatched) that has
  // finished (`!isFetching`) — success (a fresh accessToken landed) or
  // failure (getSession's queryFn never rejects; it resolves
  // `{ data: null }` on 401/authenticated:false, leaving the token
  // unchanged). requestId — not a timestamp — is what makes this
  // collision-free: two fetches completing within the same millisecond
  // would defeat a Date.now()-based comparison, but never share an id.
  const reMintRef = useRef<{
    waiting: boolean;
    baselineRequestId: string | undefined;
    tenantId: string;
    tenantName: string;
    tenantSlug: string;
    allow: boolean;
  } | null>(null);

  useEffect(() => {
    const pending = reMintRef.current;
    if (!pending || !pending.waiting) return;
    if (sessionRequestId === pending.baselineRequestId) return; // same fetch as before submit
    if (isSessionFetching) return; // the new fetch hasn't settled yet
    pending.waiting = false;
    if (sessionData?.accessToken) {
      dispatch(
        setMemberships([
          {
            tenantId: pending.tenantId,
            name: pending.tenantName,
            slug: pending.tenantSlug,
            plan: 'enterprise',
            roles: ['org_owner'],
            isOwner: true
          }
        ])
      );
      setPhaseState('done');
      onNext(pending.tenantName, pending.allow);
    } else {
      setPhaseState('refreshingSession');
    }
  }, [sessionRequestId, isSessionFetching, sessionData, dispatch, onNext]);

  useEffect(
    () => () => {
      if (retryTimeoutRef.current) clearTimeout(retryTimeoutRef.current);
    },
    []
  );

  const handleNameChange = (next: string) => {
    setName(next);
    // Auto-derive the slug until the user explicitly edits it. After that,
    // keep whatever they typed so we don't clobber their input.
    if (!slugTouched) {
      setSlug(slugify(next));
    }
  };

  const handleFinalizeError = (err: unknown) => {
    setPhaseState('idle');
    pendingPayloadRef.current = null;
    const anyErr = err as FinalizeErrorShape | undefined;
    const code = anyErr?.data?.code;

    if (anyErr?.status === 409 && code === 'tenant.slug_already_in_use') {
      setFormError(
        resolveErrorMessage(
          anyErr.data,
          t('setup.org.errorSlugConflict', { slug })
        )
      );
      return;
    }
    if (
      anyErr?.status === 409 &&
      code === 'setup.finalization_already_started'
    ) {
      setFormError(t('setup.org.errorFinalizationConflict'));
      return;
    }
    if (anyErr?.status === 409 && code === 'setup.already_completed') {
      setFormError(t('setup.org.errorAlreadyCompleted'));
      dispatch(
        setupApi.endpoints.getSetupStatus.initiate(undefined, {
          forceRefetch: true
        })
      );
      return;
    }
    if (
      anyErr?.status === 403 &&
      (code === 'setup.finalizer_bound_to_another_admin' ||
        code === 'setup.recovery_requires_super_admin')
    ) {
      // Access was revoked between the probe and this submit (e.g. someone
      // else claimed the binding). Re-probe so the access-driven render
      // below picks up the fresh reason and shows the right locked screen.
      refetchAccess();
      return;
    }
    setFormError(
      resolveErrorMessage(anyErr?.data, t('setup.org.errorGeneric'))
    );
  };

  const scheduleRetry = (payload: FinalizeSetupInput, seconds: number) => {
    retryTimeoutRef.current = setTimeout(() => {
      dispatch(
        setupApi.endpoints.getSetupStatus.initiate(undefined, {
          forceRefetch: true
        })
      );
      void runFinalize(payload);
    }, seconds * 1000);
  };

  const runFinalize = async (payload: FinalizeSetupInput) => {
    setFormError(null);
    setPhaseState('submitting');
    // Captured BEFORE the mutation is even triggered — and therefore
    // strictly before onQueryStarted could possibly dispatch its own
    // getSession re-fetch, which only happens after this response comes
    // back. Capturing it any later races onQueryStarted: it can (and in
    // practice does) already have that fetch in flight — sometimes already
    // sharing this exact requestId — by the time this function's own
    // `await` resolves.
    const baselineRequestId = sessionRequestIdRef.current;
    try {
      const result = await finalizeSetup(payload).unwrap();
      if (result.state === 'setup.finalization_in_progress') {
        setPhaseState('inProgress');
        scheduleRetry(
          payload,
          result.retryAfterSeconds ?? DEFAULT_RETRY_AFTER_SECONDS
        );
        return;
      }
      // Terminal 200 (fresh completion or a replay of one). Arm the
      // re-mint watcher; it resolves 'done' or 'refreshingSession' once
      // the shared getSession subscription above settles.
      reMintRef.current = {
        waiting: true,
        baselineRequestId,
        tenantId: result.tenantId ?? '',
        tenantName: result.tenantName ?? payload.tenantName,
        tenantSlug: result.tenantSlug ?? payload.tenantSlug,
        allow:
          result.allowAdditionalInternalTenants ??
          payload.allowAdditionalInternalTenants
      };
      // phaseState stays 'submitting' — the effect above takes it from here.
    } catch (err: unknown) {
      handleFinalizeError(err);
    }
  };

  const canSubmit =
    name.trim() !== '' && slug.trim() !== '' && allowAdditional !== null;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit || phaseState !== 'idle') return;
    const payload: FinalizeSetupInput = {
      tenantName: name.trim(),
      tenantSlug: slug.trim(),
      allowAdditionalInternalTenants: allowAdditional as boolean
    };
    pendingPayloadRef.current = payload;
    void runFinalize(payload);
  };

  const handleRetryRefreshingSession = () => {
    if (pendingPayloadRef.current) void runFinalize(pendingPayloadRef.current);
  };

  const isLocked = phaseState !== 'idle';

  // --- Render ---

  if (isSessionLoading) {
    return <OrkestraLoader />;
  }

  if (!hasEverAuthenticated) {
    return (
      <Alert variant="info" className="mb-0">
        <Alert.Heading className="fs-8">
          {t('setup.org.access.signInTitle')}
        </Alert.Heading>
        <p className="mb-3">{t('setup.org.access.signInBody')}</p>
        <Link
          to="/login"
          state={{ from: location }}
          className="btn btn-primary btn-sm"
        >
          {t('setup.org.access.signInCta')}
        </Link>
      </Alert>
    );
  }

  if (isAccessLoading) {
    return <OrkestraLoader />;
  }

  if (isAccessError || !access) {
    return (
      <Alert variant="warning" className="mb-0">
        <Alert.Heading className="fs-8">
          {t('setup.org.access.unavailableTitle')}
        </Alert.Heading>
        <p className="mb-3">{t('setup.org.access.unavailableBody')}</p>
        <Button
          variant="outline-warning"
          size="sm"
          onClick={() => refetchAccess()}
        >
          {t('setup.gate.retry')}
        </Button>
      </Alert>
    );
  }

  if (access.reason === 'bound_to_another_admin') {
    return (
      <Alert variant="warning" className="mb-0">
        <Alert.Heading className="fs-8">
          {t('setup.org.access.boundToAnotherAdminTitle')}
        </Alert.Heading>
        <p className="mb-3">{t('setup.org.access.boundToAnotherAdminBody')}</p>
        <div className="d-flex gap-2">
          <Button
            variant="outline-warning"
            size="sm"
            onClick={handleSwitchAccount}
          >
            {t('setup.org.access.switchAccount')}
          </Button>
          <Button variant="outline-secondary" size="sm" onClick={handleLogout}>
            {t('setup.org.access.logout')}
          </Button>
        </div>
      </Alert>
    );
  }

  if (access.reason === 'recovery_requires_super_admin') {
    return (
      <Alert variant="warning" className="mb-0">
        <Alert.Heading className="fs-8">
          {t('setup.org.access.recoveryRequiresSuperAdminTitle')}
        </Alert.Heading>
        <p className="mb-3">
          {t('setup.org.access.recoveryRequiresSuperAdminBody')}
        </p>
        <div className="d-flex gap-2">
          <Button
            variant="outline-warning"
            size="sm"
            onClick={handleSwitchAccount}
          >
            {t('setup.org.access.switchAccount')}
          </Button>
          <Button variant="outline-secondary" size="sm" onClick={handleLogout}>
            {t('setup.org.access.logout')}
          </Button>
        </div>
      </Alert>
    );
  }

  if (!access.canFinalize && !access.canClaimRecovery) {
    // Unreached given the backend's contract (every branch returns one of
    // canFinalize / canClaimRecovery / a known reason) — fail closed rather
    // than show an actionable form for a shape we don't recognize.
    return (
      <Alert variant="warning" className="mb-0">
        <Alert.Heading className="fs-8">
          {t('setup.org.access.unavailableTitle')}
        </Alert.Heading>
        <p className="mb-3">{t('setup.org.access.unavailableBody')}</p>
        <Button
          variant="outline-warning"
          size="sm"
          onClick={() => refetchAccess()}
        >
          {t('setup.gate.retry')}
        </Button>
      </Alert>
    );
  }

  return (
    <Form onSubmit={handleSubmit} noValidate>
      <div className="mb-4">
        <h5 className="mb-1">{t('setup.org.title')}</h5>
        <p className="text-muted fs-10 mb-0">{t('setup.org.intro')}</p>
      </div>

      {access.canClaimRecovery && (
        <Alert variant="warning" className="mb-3">
          <Alert.Heading className="fs-9">
            {t('setup.org.access.recoveryWarningTitle')}
          </Alert.Heading>
          <p className="mb-0">{t('setup.org.access.recoveryWarningBody')}</p>
        </Alert>
      )}

      {formError && (
        <Alert
          variant="danger"
          className="mb-3"
          onClose={() => setFormError(null)}
          dismissible
        >
          {formError}
        </Alert>
      )}

      {phaseState === 'inProgress' && (
        <Alert variant="info" className="mb-3">
          {t('setup.org.inProgressBody')}
        </Alert>
      )}

      {phaseState === 'refreshingSession' && (
        <Alert variant="warning" className="mb-3">
          <p className="mb-2">{t('setup.org.refreshingSessionError')}</p>
          <Button
            variant="outline-warning"
            size="sm"
            onClick={handleRetryRefreshingSession}
          >
            {t('setup.gate.retry')}
          </Button>
        </Alert>
      )}

      <Form.Group className="mb-3" controlId="setup-org-name">
        <Form.Label>{t('setup.org.labelName')}</Form.Label>
        <Form.Control
          type="text"
          value={name}
          onChange={e => handleNameChange(e.target.value)}
          disabled={isLocked}
          required
        />
        <Form.Text className="text-muted">{t('setup.org.nameHelp')}</Form.Text>
      </Form.Group>

      <Form.Group className="mb-4" controlId="setup-org-slug">
        <Form.Label>{t('setup.org.labelSlug')}</Form.Label>
        <Form.Control
          type="text"
          value={slug}
          onChange={e => {
            setSlug(e.target.value.toLowerCase());
            setSlugTouched(true);
          }}
          disabled={isLocked}
          required
        />
        <Form.Text className="text-muted">{t('setup.org.slugHelp')}</Form.Text>
      </Form.Group>

      <Form.Group className="mb-4">
        <Form.Label>{t('setup.org.provisioning.label')}</Form.Label>
        <p className="text-muted fs-10 mb-2">
          {t('setup.org.provisioning.help')}
        </p>

        <Form.Check
          type="radio"
          name="provisioning-mode"
          id="provisioning-allow"
          label={t('setup.org.provisioning.allowLabel')}
          checked={allowAdditional === true}
          onChange={() => setAllowAdditional(true)}
          disabled={isLocked}
        />
        <p className="text-muted fs-10 ms-4 mb-2">
          {t('setup.org.provisioning.allowHelp')}
        </p>

        <Form.Check
          type="radio"
          name="provisioning-mode"
          id="provisioning-single"
          label={t('setup.org.provisioning.singleLabel')}
          checked={allowAdditional === false}
          onChange={() => setAllowAdditional(false)}
          disabled={isLocked}
        />
        <p className="text-muted fs-10 ms-4 mb-0">
          {t('setup.org.provisioning.singleHelp')}
        </p>
      </Form.Group>

      <div className="d-flex justify-content-end">
        <Button
          type="submit"
          variant="primary"
          disabled={!canSubmit || isLocked}
        >
          {phaseState === 'submitting' || phaseState === 'inProgress' ? (
            <>
              <Spinner animation="border" size="sm" className="me-2" />
              {t('setup.org.submitting')}
            </>
          ) : (
            t('setup.org.submit')
          )}
        </Button>
      </div>
    </Form>
  );
};

export default OrgStep;
