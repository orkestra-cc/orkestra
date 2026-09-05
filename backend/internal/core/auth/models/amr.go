package models

// Authentication-method-reference markers (RFC 8176, plus two local
// extensions). Every predicate that reasons about MFA authority is
// defined over the two sets below and nowhere else.
//
// Before this file the marker list was written out five times — three
// predicates in shared/middleware, plus the refresh recomputation in
// auth/services — and two of those copies deliberately disagreed about
// device_trust. A set that lives in one place cannot drift; a set copied
// into a package that cannot import the other one always will.
const (
	// AMRPassword and AMROAuth are BASE markers: they describe how the
	// session began. Neither proves a second factor, and neither is
	// derived from a credential that can be removed, so neither is
	// affected by the MFA epoch and both survive a refresh untouched.
	AMRPassword = "pwd"
	AMROAuth    = "oauth"

	// AMROTP, AMRWebAuthn and AMRMFA name a genuine SECOND FACTOR.
	// "mfa" is the generic marker some issuance paths use when the
	// concrete method is not interesting to the consumer.
	AMROTP      = "otp"
	AMRWebAuthn = "webauthn"
	AMRMFA      = "mfa"

	// AMRReauth is a fresh password reconfirm minted by
	// /v1/auth/{tier}/me/password-confirm. It proves PRESENCE within the
	// last few minutes and nothing more: it is not a second factor
	// (audit finding M-1), and it is not a property of the session, so
	// it never survives a token refresh.
	AMRReauth = "reauth"
)

// IsSecondFactorAMR reports whether the marker names a real second
// factor. This is the set RequireMFA and Cedar's principal.mfa_enrolled
// gate on, and the set the sidecar JWTValidator.RequireMFA has always
// used.
//
// DeviceTrustAMR is deliberately absent: a device-trust passthrough is
// stamped ALONGSIDE the factor value that earned the trust
// (["pwd","otp","device_trust"]), so matching it here would let the
// annotation alone stand in for the factor.
func IsSecondFactorAMR(marker string) bool {
	switch marker {
	case AMROTP, AMRWebAuthn, AMRMFA:
		return true
	}
	return false
}

// IsEpochBoundAMR reports whether the marker's authority derives from an
// MFA credential the user must still hold — the second factors plus the
// device-trust annotation, which was granted on the strength of one.
//
// These are exactly the markers the MFA epoch governs: when a token's
// "mfae" claim is behind the user's current MFAEpoch, the factor set that
// produced them is gone and every one of them is stripped.
func IsEpochBoundAMR(marker string) bool {
	return IsSecondFactorAMR(marker) || marker == DeviceTrustAMR
}

// HasSecondFactorAMR reports whether any marker in amr names a second
// factor.
func HasSecondFactorAMR(amr []string) bool {
	for _, v := range amr {
		if IsSecondFactorAMR(v) {
			return true
		}
	}
	return false
}

// HasEpochBoundAMR reports whether any marker in amr is governed by the
// MFA epoch. A token for which this is false has no MFA authority to
// lose, so the epoch cannot change the answer for it — which is what
// lets the resolver skip the user lookup on the common request path.
func HasEpochBoundAMR(amr []string) bool {
	for _, v := range amr {
		if IsEpochBoundAMR(v) {
			return true
		}
	}
	return false
}

// WithoutEpochBoundAMR returns amr with every epoch-governed marker
// removed, leaving the base markers (and "reauth", which the epoch does
// not govern) intact. Never mutates the input: the caller's slice is the
// token's own claim, read by other consumers on the same request.
func WithoutEpochBoundAMR(amr []string) []string {
	out := make([]string, 0, len(amr))
	for _, v := range amr {
		if IsEpochBoundAMR(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}
