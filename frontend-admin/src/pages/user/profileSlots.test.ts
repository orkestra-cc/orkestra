import { describe, it, expect } from 'vitest';
import {
  USER_ADMIN_PROFILE_SECTIONS,
  USER_PROFILE_SECTIONS,
  USER_SETTINGS_SECTIONS,
  type UserProfileSectionProps,
  type UserSettingsSectionProps
} from './profileSlots';

// Contributors declare these ids and shapes in their own tree; this test pins
// the contract so a rename here fails loudly instead of silently dropping
// every contributed section.
describe('user profile slot contract', () => {
  it('publishes the three core slot ids', () => {
    expect(USER_PROFILE_SECTIONS).toBe('user.profile.sections');
    expect(USER_ADMIN_PROFILE_SECTIONS).toBe('user.adminProfile.sections');
    expect(USER_SETTINGS_SECTIONS).toBe('user.settings.sections');
  });

  it('keeps the props shape contributors mirror', () => {
    const profile: UserProfileSectionProps = {
      userId: 'u-1',
      user: {
        id: 'u-1',
        email: 'u@x',
        username: 'u',
        fullName: 'U',
        role: 'operator',
        providers: [],
        isActive: true,
        emailVerified: true,
        createdAt: '',
        updatedAt: ''
      },
      viewer: 'self'
    };
    const settings: UserSettingsSectionProps = { userId: 'u-1' };
    expect(Object.keys(profile).sort()).toEqual(['user', 'userId', 'viewer']);
    expect(Object.keys(settings)).toEqual(['userId']);
  });
});
