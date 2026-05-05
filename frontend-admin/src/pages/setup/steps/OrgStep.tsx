import { useState, useMemo, FormEvent } from 'react';
import { Alert, Button, Form, Spinner } from 'react-bootstrap';
import { useCreateOrgMutation } from 'store/api/tenantApi';
import { useAppDispatch } from 'store/hooks';
import { setMemberships } from 'store/slices/tenantSlice';

interface OrgStepProps {
  /**
   * Full name of the administrator created in the previous step. Used to
   * pre-fill the default org name ("{first name}'s Workspace") so the
   * most common case is a single click.
   */
  adminFullName: string;
  onNext: (orgName: string) => void;
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

/**
 * Third step of the setup wizard: creates the first organization and
 * enrolls the fresh administrator as its owner. Calls POST /v1/tenants
 * (already a global, non-org-scoped endpoint) via the existing
 * tenantApi.createOrg mutation — no new backend surface.
 *
 * Plan is hardcoded to "enterprise" because this runs during a
 * self-hosted bootstrap where there's no billing reason to withhold
 * features from the operator's own deployment. If you later need to
 * support a "free tier bootstrap" for a SaaS build, promote this to
 * an env-var-driven default.
 */
const OrgStep = ({ adminFullName, onNext }: OrgStepProps) => {
  const dispatch = useAppDispatch();
  const [createOrg, { isLoading }] = useCreateOrgMutation();

  const defaultName = useMemo(() => {
    const firstName = adminFullName.trim().split(/\s+/)[0] || 'Default';
    return `${firstName}'s Workspace`;
  }, [adminFullName]);

  const [name, setName] = useState(defaultName);
  const [slug, setSlug] = useState(slugify(defaultName));
  const [slugTouched, setSlugTouched] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleNameChange = (next: string) => {
    setName(next);
    // Auto-derive the slug until the user explicitly edits it. After that,
    // keep whatever they typed so we don't clobber their input.
    if (!slugTouched) {
      setSlug(slugify(next));
    }
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);

    const trimmedName = name.trim();
    if (!trimmedName) {
      setError('Organization name is required.');
      return;
    }
    if (!slug || !/^[a-z0-9-]+$/.test(slug)) {
      setError('Slug must contain only lowercase letters, digits, and hyphens.');
      return;
    }

    try {
      const created = await createOrg({ name: trimmedName, slug, plan: 'enterprise' }).unwrap();
      // Seed tenant state with the freshly created tenant so the remaining
      // wizard steps (and the post-wizard dashboard navigation) run with a
      // valid X-Tenant-ID. createOrg's onQueryStarted already refreshes the
      // JWT so the new membership is in claims.Memberships; this hydrates
      // the Redux side so baseApi stops sending a stale header.
      dispatch(
        setMemberships([
          {
            tenantId: created.id,
            name: created.name,
            slug: created.slug,
            plan: created.plan,
            roles: ['owner'],
            isOwner: true,
          },
        ])
      );
      onNext(trimmedName);
    } catch (err: unknown) {
      const anyErr = err as { status?: number; data?: { detail?: string } };
      if (anyErr?.status === 409) {
        setError(
          anyErr?.data?.detail ||
            `An organization with slug "${slug}" already exists. Pick a different slug.`
        );
      } else {
        setError(anyErr?.data?.detail || 'Could not create the organization. Please try again.');
      }
    }
  };

  return (
    <Form onSubmit={handleSubmit} noValidate>
      <div className="mb-4">
        <h5 className="mb-1">Create your first organization</h5>
        <p className="text-muted fs-10 mb-0">
          Orkestra is multi-tenant — every feature lives inside an
          organization. We&apos;ll create one now and enroll you as the
          owner. You can rename it or add more organizations later from the
          admin UI.
        </p>
      </div>

      {error && (
        <Alert variant="danger" className="mb-3" onClose={() => setError(null)} dismissible>
          {error}
        </Alert>
      )}

      <Form.Group className="mb-3">
        <Form.Label>Organization name</Form.Label>
        <Form.Control
          type="text"
          value={name}
          onChange={(e) => handleNameChange(e.target.value)}
          required
        />
        <Form.Text className="text-muted">
          Human-readable label shown throughout the admin UI.
        </Form.Text>
      </Form.Group>

      <Form.Group className="mb-4">
        <Form.Label>Slug</Form.Label>
        <Form.Control
          type="text"
          value={slug}
          onChange={(e) => {
            setSlug(e.target.value.toLowerCase());
            setSlugTouched(true);
          }}
          required
        />
        <Form.Text className="text-muted">
          URL-safe identifier. Auto-derived from the name until you edit it.
          Lowercase letters, digits, and hyphens only.
        </Form.Text>
      </Form.Group>

      <div className="d-flex justify-content-end">
        <Button type="submit" variant="primary" disabled={isLoading}>
          {isLoading ? (
            <>
              <Spinner animation="border" size="sm" className="me-2" />
              Creating...
            </>
          ) : (
            'Create organization'
          )}
        </Button>
      </div>
    </Form>
  );
};

export default OrgStep;
