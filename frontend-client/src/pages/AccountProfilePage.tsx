import { useState, type ChangeEvent } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { useMe } from "@/auth/useMe";
import { UserAvatar } from "@/components/UserAvatar";
import {
  commitAvatarUpload,
  presignAvatarUpload,
  putAvatarBlob,
  setAvatarSource,
  type AvatarSource,
} from "@/api/avatar";
import type { MeResponse } from "@/api/auth";

// /account/profile — self-service avatar surface for Tier-2 clients.
// Three modes mirror the operator console's AvatarSettings card:
//
//   1. Upload — file picker → client-side crop-to-square + downscale
//      to 512px PNG via canvas → presign → direct PUT to S3 → commit.
//   2. Pick from linked OAuth provider — buttons per linked provider,
//      Apple disabled (Apple's OIDC never returns a picture).
//   3. Reset to initials — wipes uploaded blob, SPA renders initials.
//
// Pure Tailwind + TanStack Query. Mutations invalidate ['me'] and
// also push the response into the cache for an immediate render.

const PROVIDER_LABELS: Record<string, string> = {
  google: "Google",
  apple: "Apple",
  github: "GitHub",
  discord: "Discord",
};

type OAuthAvatarSource = Exclude<AvatarSource, "uploaded" | "initials">;

const PROVIDER_TO_SOURCE: Record<string, OAuthAvatarSource> = {
  google: "oauth_google",
  apple: "oauth_apple",
  github: "oauth_github",
  discord: "oauth_discord",
};

const MAX_BYTES = 2 * 1024 * 1024;
const TARGET_SIZE = 512;
const ALLOWED_MIMES = ["image/png", "image/jpeg", "image/webp"];

async function cropAndResize(file: File): Promise<Blob> {
  const url = URL.createObjectURL(file);
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const i = new Image();
      i.onload = () => resolve(i);
      i.onerror = () => reject(new Error("image_decode_failed"));
      i.src = url;
    });
    const side = Math.min(img.naturalWidth, img.naturalHeight);
    const sx = (img.naturalWidth - side) / 2;
    const sy = (img.naturalHeight - side) / 2;
    const canvas = document.createElement("canvas");
    canvas.width = TARGET_SIZE;
    canvas.height = TARGET_SIZE;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("canvas_unavailable");
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(img, sx, sy, side, side, 0, 0, TARGET_SIZE, TARGET_SIZE);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (blob) =>
          blob ? resolve(blob) : reject(new Error("canvas_blob_failed")),
        "image/png",
      );
    });
  } finally {
    URL.revokeObjectURL(url);
  }
}

export function AccountProfilePage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: me, isLoading, isError } = useMe();

  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const handleSuccess = (next: MeResponse, message: string) => {
    queryClient.setQueryData<MeResponse>(["me"], next);
    queryClient.invalidateQueries({ queryKey: ["me"] });
    setStatusMessage(message);
    setErrorMessage(null);
  };

  const handleError = (err: unknown, fallback: string) => {
    const message = err instanceof Error ? err.message : fallback;
    setErrorMessage(message);
    setStatusMessage(null);
  };

  const uploadMutation = useMutation({
    mutationFn: async (file: File) => {
      const blob = await cropAndResize(file);
      if (blob.size > MAX_BYTES) {
        throw new Error(t("accountProfile.errors.tooLarge"));
      }
      const presigned = await presignAvatarUpload({
        contentType: "image/png",
        sizeBytes: blob.size,
      });
      await putAvatarBlob(presigned, blob);
      return commitAvatarUpload({ key: presigned.key });
    },
    onSuccess: (next) =>
      handleSuccess(next, t("accountProfile.toasts.uploaded")),
    onError: (err) => handleError(err, t("accountProfile.errors.uploadFailed")),
  });

  const sourceMutation = useMutation({
    mutationFn: setAvatarSource,
    onSuccess: (next) =>
      handleSuccess(next, t("accountProfile.toasts.updated")),
    onError: (err) => handleError(err, t("accountProfile.errors.updateFailed")),
  });

  const onFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-selecting the same file
    if (!file) return;
    if (!ALLOWED_MIMES.includes(file.type)) {
      setErrorMessage(t("accountProfile.errors.invalidMime"));
      return;
    }
    if (file.size > MAX_BYTES * 4) {
      setErrorMessage(t("accountProfile.errors.tooLarge"));
      return;
    }
    setErrorMessage(null);
    uploadMutation.mutate(file);
  };

  const providers = me?.oauthProviders ?? [];
  const switching = sourceMutation.isPending
    ? sourceMutation.variables?.source
    : null;
  const uploading = uploadMutation.isPending;

  return (
    <section className="mx-auto max-w-3xl px-6 py-16">
      <header className="mb-8 flex items-center justify-between">
        <div>
          <h1 className="mb-2 text-3xl font-semibold tracking-tight">
            {t("accountProfile.title")}
          </h1>
          <p className="text-slate-600">{t("accountProfile.subtitle")}</p>
        </div>
        <Link
          to="/account"
          className="text-sm font-medium text-slate-600 hover:text-slate-900"
        >
          ← {t("account.back")}
        </Link>
      </header>

      {isLoading && <p className="text-slate-500">{t("loading")}</p>}
      {isError && (
        <p
          className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
          role="alert"
        >
          {t("error.generic")}
        </p>
      )}

      {me && (
        <div className="space-y-6 rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
          {/* Preview */}
          <div className="flex items-center gap-4">
            <UserAvatar user={me} size="2xl" />
            <div>
              <div className="text-base font-semibold text-slate-900">
                {me.fullName || me.email}
              </div>
              <div className="text-sm text-slate-600">
                {t(`accountProfile.sources.${me.avatarSource ?? "initials"}`, {
                  defaultValue: me.avatarSource ?? "",
                })}
              </div>
            </div>
          </div>

          {statusMessage && (
            <p
              className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-700"
              role="status"
            >
              {statusMessage}
            </p>
          )}
          {errorMessage && (
            <p
              className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700"
              role="alert"
            >
              {errorMessage}
            </p>
          )}

          {/* Upload */}
          <div>
            <h2 className="mb-2 text-sm font-semibold text-slate-900">
              {t("accountProfile.upload.label")}
            </h2>
            <label
              htmlFor="avatar-upload-input"
              className={`inline-flex cursor-pointer items-center rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50 ${
                uploading ? "cursor-wait opacity-50" : ""
              }`}
            >
              {uploading
                ? t("accountProfile.upload.uploading")
                : t("accountProfile.upload.choose")}
            </label>
            <input
              id="avatar-upload-input"
              type="file"
              accept={ALLOWED_MIMES.join(",")}
              disabled={uploading}
              className="sr-only"
              onChange={onFileChange}
            />
            <p className="mt-1 text-xs text-slate-500">
              {t("accountProfile.upload.hint")}
            </p>
          </div>

          {/* OAuth picker */}
          {providers.length > 0 && (
            <div>
              <h2 className="mb-2 text-sm font-semibold text-slate-900">
                {t("accountProfile.oauth.label")}
              </h2>
              <div className="flex flex-wrap gap-2">
                {providers.map((p) => {
                  const provider = p.provider;
                  const isApple = provider === "apple";
                  const source = PROVIDER_TO_SOURCE[provider];
                  const picture =
                    typeof p.metadata?.picture === "string"
                      ? (p.metadata.picture as string)
                      : undefined;
                  const isActive = me.avatarSource === source;
                  const label = PROVIDER_LABELS[provider] ?? provider;
                  const disabled =
                    isApple || sourceMutation.isPending || uploading;
                  return (
                    <button
                      key={`${provider}-${p.providerId}`}
                      type="button"
                      disabled={disabled}
                      onClick={() => sourceMutation.mutate({ source })}
                      title={
                        isApple
                          ? t("accountProfile.oauth.appleDisabled")
                          : undefined
                      }
                      className={[
                        "inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium transition-colors",
                        isActive
                          ? "border-slate-900 bg-slate-900 text-white"
                          : "border-slate-300 bg-white text-slate-700 hover:bg-slate-50",
                        disabled ? "cursor-not-allowed opacity-50" : "",
                      ].join(" ")}
                    >
                      {picture ? (
                        <img
                          src={picture}
                          alt=""
                          className="h-5 w-5 rounded-full"
                        />
                      ) : null}
                      <span>{label}</span>
                      {switching === source && (
                        <span aria-hidden="true">…</span>
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Reset */}
          <div>
            <h2 className="mb-2 text-sm font-semibold text-slate-900">
              {t("accountProfile.initials.label")}
            </h2>
            <button
              type="button"
              disabled={
                me.avatarSource === "initials" ||
                sourceMutation.isPending ||
                uploading
              }
              onClick={() => sourceMutation.mutate({ source: "initials" })}
              className="inline-flex items-center rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {switching === "initials" && (
                <span className="mr-2" aria-hidden="true">
                  …
                </span>
              )}
              {t("accountProfile.initials.reset")}
            </button>
            <p className="mt-1 text-xs text-slate-500">
              {t("accountProfile.initials.hint")}
            </p>
          </div>
        </div>
      )}
    </section>
  );
}
