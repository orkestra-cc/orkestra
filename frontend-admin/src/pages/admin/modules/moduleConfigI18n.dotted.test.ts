import { describe, it, expect } from 'vitest';
import i18n from '../../../i18n';

// notification (and tenant, Task 3) carry dotted config keys. The resolver
// builds e.g. `moduleConfig.notification.fields.email.smtp.host.label`, and
// i18next's default `.` separator walks it into the nested bundle. This guards
// that the authored shape actually resolves rather than returning the raw key.
describe('dotted module-config i18n keys resolve', () => {
  it('resolves a nested notification field label', () => {
    expect(
      i18n.t('moduleConfig.notification.fields.email.smtp.host.label')
    ).toBe('SMTP host');
  });

  it('resolves a notification group label + desc', () => {
    expect(i18n.t('moduleConfig.notification.groups.delivery.label')).toBe(
      'Delivery'
    );
    expect(i18n.t('moduleConfig.notification.groups.delivery.desc')).toContain(
      'The default transport'
    );
  });

  it('resolves the sender-profiles group and record-list field (ADR-0019)', () => {
    expect(i18n.t('moduleConfig.notification.groups.senders.label')).toBe(
      'Sender profiles'
    );
    expect(i18n.t('moduleConfig.notification.fields.email.senders.label')).toBe(
      'Sender profiles'
    );
    expect(
      i18n.t('moduleConfig.notification.fields.email.senders.desc')
    ).toContain('the most specific pattern wins');
  });

  it('resolves a nested tenant field label + group', () => {
    expect(
      i18n.t('moduleConfig.tenant.fields.provisioning.internal.mode.label')
    ).toBe('Internal tenant creation');
    expect(
      i18n.t('moduleConfig.tenant.groups.provisioning.external.label')
    ).toBe('External provisioning (Tier-2)');
  });
});
