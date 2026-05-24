import { useState } from 'react';
import {
  Button,
  Card,
  OverlayTrigger,
  Spinner,
  Tooltip
} from 'react-bootstrap';
import { useTranslation } from 'react-i18next';
import { toast } from 'react-toastify';
import OrkestraCardHeader from 'components/common/OrkestraCardHeader';
import UserAvatar from 'components/common/UserAvatar';
import {
  AvatarSource,
  OAuthProvider,
  useCommitAvatarUploadMutation,
  useGetCurrentUserQuery,
  usePresignAvatarUploadMutation,
  useSetAvatarSourceMutation
} from 'store/api/authApi';

// AvatarSettings — the self-service avatar card on /user/settings.
//
// Three paths:
//
//   1. Upload — dropzone → client-side square-crop + downscale to
//      512px → presign → direct PUT to S3-compat storage → commit.
//      Keeps payload small + bypasses the backend body for the bytes.
//
//   2. Pick from linked OAuth provider — buttons for each provider the
//      user has linked. Apple is rendered disabled with a tooltip
//      because Apple's OIDC userinfo never carries a picture URL.
//
//   3. Reset to initials — wipes any uploaded blob and tells the SPA
//      to render initials over a deterministic color.
//
// Every path resolves to a `setAvatarSource` or `commitAvatarUpload`
// mutation that returns the updated user; the cache patch in those
// mutations re-renders ProfileDropdown + ProfileBanner instantly.

const PROVIDER_LABELS: Record<OAuthProvider, string> = {
  google: 'Google',
  apple: 'Apple',
  github: 'GitHub',
  discord: 'Discord'
};

const PROVIDER_TO_SOURCE: Record<OAuthProvider, AvatarSource> = {
  google: 'oauth_google',
  apple: 'oauth_apple',
  github: 'oauth_github',
  discord: 'oauth_discord'
};

const MAX_BYTES = 2 * 1024 * 1024;
const TARGET_SIZE = 512;
const ALLOWED_MIMES = ['image/png', 'image/jpeg', 'image/webp'];

// Crop the source image to a centered square and downscale to
// TARGET_SIZE on the longest side. Returns a PNG Blob; backend allows
// png/jpeg/webp but png keeps round-trip lossless for the typical
// case (avatars come from screenshots or OAuth pictures).
async function cropAndResize(file: File): Promise<Blob> {
  const url = URL.createObjectURL(file);
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const i = new Image();
      i.onload = () => resolve(i);
      i.onerror = () => reject(new Error('image_decode_failed'));
      i.src = url;
    });
    const side = Math.min(img.naturalWidth, img.naturalHeight);
    const sx = (img.naturalWidth - side) / 2;
    const sy = (img.naturalHeight - side) / 2;
    const canvas = document.createElement('canvas');
    canvas.width = TARGET_SIZE;
    canvas.height = TARGET_SIZE;
    const ctx = canvas.getContext('2d');
    if (!ctx) throw new Error('canvas_unavailable');
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(img, sx, sy, side, side, 0, 0, TARGET_SIZE, TARGET_SIZE);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        blob =>
          blob ? resolve(blob) : reject(new Error('canvas_blob_failed')),
        'image/png'
      );
    });
  } finally {
    URL.revokeObjectURL(url);
  }
}

const AvatarSettings: React.FC = () => {
  const { t } = useTranslation();
  const { data: user } = useGetCurrentUserQuery();
  const [presign] = usePresignAvatarUploadMutation();
  const [commit] = useCommitAvatarUploadMutation();
  const [setSource] = useSetAvatarSourceMutation();
  const [uploading, setUploading] = useState(false);
  const [switching, setSwitching] = useState<AvatarSource | null>(null);

  const providers = user?.oauthProviders ?? [];

  const handleUpload = async (file: File | undefined) => {
    if (!file) return;
    if (!ALLOWED_MIMES.includes(file.type)) {
      toast.error(t('settings.avatar.errors.invalidMime'));
      return;
    }
    if (file.size > MAX_BYTES * 4) {
      // Reject obviously-massive files before we waste cycles cropping.
      // The post-crop blob is the one that's actually checked against
      // the 2 MiB backend cap.
      toast.error(t('settings.avatar.errors.tooLarge'));
      return;
    }

    setUploading(true);
    try {
      const blob = await cropAndResize(file);
      if (blob.size > MAX_BYTES) {
        toast.error(t('settings.avatar.errors.tooLarge'));
        return;
      }
      const presignResult = await presign({
        contentType: 'image/png',
        sizeBytes: blob.size
      }).unwrap();

      // Direct PUT to S3 — not through RTK Query (the URL is signed
      // with no auth header, and the body is binary not JSON).
      const putResponse = await fetch(presignResult.url, {
        method: 'PUT',
        headers: presignResult.headers,
        body: blob,
        credentials: 'omit'
      });
      if (!putResponse.ok) {
        toast.error(t('settings.avatar.errors.uploadFailed'));
        return;
      }

      await commit({ key: presignResult.key }).unwrap();
      toast.success(t('settings.avatar.toasts.uploaded'));
    } catch (err) {
      const code =
        (err as { data?: { detail?: string } })?.data?.detail ??
        t('settings.avatar.errors.uploadFailed');
      toast.error(code);
    } finally {
      setUploading(false);
    }
  };

  const handleSwitchSource = async (source: AvatarSource) => {
    setSwitching(source);
    try {
      await setSource({ source }).unwrap();
      toast.success(t('settings.avatar.toasts.updated'));
    } catch (err) {
      const code = (err as { data?: { detail?: string } })?.data?.detail;
      toast.error(code ?? t('settings.avatar.errors.updateFailed'));
    } finally {
      setSwitching(null);
    }
  };

  const previewUser = user ?? undefined;

  return (
    <Card className="mb-3">
      <OrkestraCardHeader title={t('settings.avatar.title')} />
      <Card.Body className="bg-body-tertiary">
        <p className="fs-9 text-muted mb-3">
          {t('settings.avatar.description')}
        </p>

        <div className="d-flex align-items-center gap-3 mb-3">
          <UserAvatar user={previewUser} size="4xl" />
          <div className="flex-1">
            <div className="fw-semibold">
              {previewUser?.fullName || previewUser?.email || '—'}
            </div>
            <small className="text-muted">
              {t(
                `settings.avatar.sources.${
                  previewUser?.avatarSource ?? 'initials'
                }`,
                { defaultValue: previewUser?.avatarSource ?? '' }
              )}
            </small>
          </div>
        </div>

        {/* Upload */}
        <div className="mb-3">
          <div className="fw-semibold fs-9 mb-2">
            {t('settings.avatar.upload.label')}
          </div>
          <label
            htmlFor="avatar-upload-input"
            className="btn btn-outline-primary btn-sm me-2"
            style={{ cursor: uploading ? 'wait' : 'pointer' }}
          >
            {uploading ? (
              <>
                <Spinner animation="border" size="sm" className="me-2" />
                {t('settings.avatar.upload.uploading')}
              </>
            ) : (
              t('settings.avatar.upload.choose')
            )}
          </label>
          <input
            id="avatar-upload-input"
            type="file"
            accept={ALLOWED_MIMES.join(',')}
            disabled={uploading}
            className="d-none"
            onChange={e => handleUpload(e.target.files?.[0])}
          />
          <small className="text-muted d-block mt-1">
            {t('settings.avatar.upload.hint')}
          </small>
        </div>

        {/* OAuth provider picker */}
        {providers.length > 0 && (
          <div className="mb-3">
            <div className="fw-semibold fs-9 mb-2">
              {t('settings.avatar.oauth.label')}
            </div>
            <div className="d-flex flex-wrap gap-2">
              {providers.map(p => {
                const provider = p.provider as OAuthProvider;
                const isApple = provider === 'apple';
                const source = PROVIDER_TO_SOURCE[provider];
                const picture = (p.metadata?.picture as string) || undefined;
                const isActiveSource = previewUser?.avatarSource === source;
                const btn = (
                  <Button
                    key={provider}
                    variant={isActiveSource ? 'primary' : 'outline-secondary'}
                    size="sm"
                    disabled={isApple || switching !== null || uploading}
                    onClick={() => handleSwitchSource(source)}
                    className="d-inline-flex align-items-center gap-2"
                  >
                    {picture ? (
                      <img
                        src={picture}
                        alt=""
                        width={20}
                        height={20}
                        className="rounded-circle"
                      />
                    ) : null}
                    <span>{PROVIDER_LABELS[provider]}</span>
                    {switching === source && (
                      <Spinner animation="border" size="sm" />
                    )}
                  </Button>
                );
                if (isApple) {
                  return (
                    <OverlayTrigger
                      key={provider}
                      placement="top"
                      overlay={
                        <Tooltip>
                          {t('settings.avatar.oauth.appleDisabled')}
                        </Tooltip>
                      }
                    >
                      <span className="d-inline-block">{btn}</span>
                    </OverlayTrigger>
                  );
                }
                return btn;
              })}
            </div>
          </div>
        )}

        {/* Reset to initials */}
        <div>
          <div className="fw-semibold fs-9 mb-2">
            {t('settings.avatar.initials.label')}
          </div>
          <Button
            variant="outline-secondary"
            size="sm"
            disabled={
              switching !== null ||
              uploading ||
              previewUser?.avatarSource === 'initials'
            }
            onClick={() => handleSwitchSource('initials')}
          >
            {switching === 'initials' && (
              <Spinner animation="border" size="sm" className="me-2" />
            )}
            {t('settings.avatar.initials.reset')}
          </Button>
          <small className="text-muted d-block mt-1">
            {t('settings.avatar.initials.hint')}
          </small>
        </div>
      </Card.Body>
    </Card>
  );
};

export default AvatarSettings;
