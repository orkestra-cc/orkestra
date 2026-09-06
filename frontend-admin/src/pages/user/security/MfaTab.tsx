import MfaSettings from '../settings/mfa/MfaSettings';

// MfaTab is a thin pass-through to MfaSettings so the security page
// benefits from any future improvements to the shared component. It is
// rendered directly: the tab strip already names the pane, and the two
// distinct sub-surfaces inside it (authenticator app, passkeys) carry
// their own SectionCard rather than one wrapper around the whole pane.
const MfaTab = () => <MfaSettings />;

export default MfaTab;
