import { avatarColor, initialsFor } from "@/lib/avatarColor";

// UserAvatar is the canonical avatar renderer for the client SPA. It
// respects the backend's avatar-source preference uniformly: a
// non-empty `avatar` URL renders as <img>, an empty URL renders
// initials over a deterministic per-user color.
//
// Pure Tailwind — by design separate from the operator console's
// UserAvatar (which leans on Bootstrap classes) per
// frontend-client/CLAUDE.md "Don't import from ../frontend-admin".

export interface UserAvatarProfile {
  id?: string;
  email?: string;
  fullName?: string;
  username?: string;
  avatar?: string;
}

export type UserAvatarSize = "xs" | "sm" | "md" | "lg" | "xl" | "2xl";

// Tailwind doesn't let us compose class names from a runtime size
// string, so the size map is exhaustive. Bumps stay grep-able.
const SIZE_CLASSES: Record<UserAvatarSize, string> = {
  xs: "h-7 w-7 text-[10px]",
  sm: "h-9 w-9 text-xs",
  md: "h-11 w-11 text-sm",
  lg: "h-14 w-14 text-base",
  xl: "h-20 w-20 text-xl",
  "2xl": "h-28 w-28 text-3xl",
};

interface UserAvatarProps {
  user?: UserAvatarProfile | null;
  size?: UserAvatarSize;
  className?: string;
}

export function UserAvatar({ user, size = "md", className }: UserAvatarProps) {
  const sizeClass = SIZE_CLASSES[size];
  const ringClass = "rounded-full ring-1 ring-slate-200";
  const wrapperClass = [sizeClass, ringClass, "overflow-hidden", className]
    .filter(Boolean)
    .join(" ");

  if (user?.avatar) {
    return (
      <img
        src={user.avatar}
        alt={user.fullName ?? user.email ?? ""}
        className={`${wrapperClass} object-cover`}
        // Decorative when name is empty; img inherits aria-hidden via
        // empty alt per WCAG. Keeps the avatar out of the AT tab order.
      />
    );
  }

  const seed =
    user?.id || user?.email || user?.fullName || user?.username || "";
  const initials = initialsFor(user?.fullName || user?.username, user?.email);
  const { background, color } = avatarColor(seed);

  return (
    <div
      className={`${wrapperClass} inline-flex items-center justify-center font-semibold select-none`}
      style={{ backgroundColor: background, color }}
      role="img"
      aria-label={user?.fullName || user?.email || initials}
    >
      {initials}
    </div>
  );
}
