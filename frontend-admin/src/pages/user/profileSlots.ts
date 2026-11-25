// Slot contract owned by the core user pages: other modules contribute
// sections to the user's own profile, to the administrator's view of a user
// and to the user's settings page through ModuleManifest.contributions (see
// modules/types.ts). A contributor never imports this file: it declares a
// structurally identical props type in its own tree, and profileSlots.test.ts
// pins the ids and the shape so the two cannot drift silently.
//
// The backend mirrors the catalog declaratively (module.HasProfileSections,
// GET /v1/me/profile-sections, GET /v1/users/{id}/profile-sections) for
// clients without this slot system; the SPA renders from the manifests.
import type { User } from 'store/api/userApi';

/** Sections on the user's own profile (/user/profile). */
export const USER_PROFILE_SECTIONS = 'user.profile.sections';
/** Sections on an administrator's view of a user (/admin/user/profile/:userId). */
export const USER_ADMIN_PROFILE_SECTIONS = 'user.adminProfile.sections';
/** Sections on the user's own settings page (/user/settings). */
export const USER_SETTINGS_SECTIONS = 'user.settings.sections';

/** Props every profile section receives. The host provides the card chrome. */
export interface UserProfileSectionProps {
  /** UUID of the user whose profile is shown. */
  userId: string;
  user: User;
  /** Who is looking: the user themself, or an administrator. */
  viewer: 'self' | 'admin';
}

/** Props every settings section receives. */
export interface UserSettingsSectionProps {
  /** UUID of the signed-in user. */
  userId: string;
}
