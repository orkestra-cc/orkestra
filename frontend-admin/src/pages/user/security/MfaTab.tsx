import MfaSettings from '../settings/mfa/MfaSettings';

// MfaTab is a thin pass-through to the existing MfaSettings card so
// the security page benefits from any future improvements to the
// shared component. We render it directly — the surrounding tab
// chrome already provides the heading.
const MfaTab = () => <MfaSettings />;

export default MfaTab;
