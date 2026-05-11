package services

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var (
	ErrPasswordTooShort      = errors.New("password is shorter than the configured minimum")
	ErrPasswordTooLong       = errors.New("password is longer than the configured maximum")
	ErrPasswordContainsEmail = errors.New("password must not contain your email")
	ErrPasswordBreached      = errors.New("password has appeared in a known data breach")
	ErrPasswordInvalidHash   = errors.New("invalid password hash format")
	ErrPasswordMismatch      = errors.New("password does not match")
	ErrPasswordMissingUpper  = errors.New("password must contain an uppercase letter")
	ErrPasswordMissingLower  = errors.New("password must contain a lowercase letter")
	ErrPasswordMissingDigit  = errors.New("password must contain a digit")
	ErrPasswordMissingSymbol = errors.New("password must contain a symbol")
)

// PasswordService hashes, verifies, and validates passwords using argon2id.
// Hash format is PHC-compatible:
//
//	$argon2id$v=19$m=<mem>,t=<time>,p=<par>$<salt_b64>$<hash_b64>
//
// Parameters follow OWASP 2025 recommendations: 19 MiB memory, 2 iterations,
// 1 parallel lane, 16-byte salt, 32-byte output.
type PasswordService interface {
	Hash(plaintext string) (string, error)
	Verify(plaintext, encoded string) (bool, error)
	ValidatePolicy(ctx context.Context, plaintext, email string) error
	NeedsRehash(encoded string) bool
	// DummyHash returns a precomputed argon2id hash used to run Verify
	// against when a user is not found. This makes the wall-clock time
	// for "user not found" match "user found, wrong password".
	DummyHash() string
	// SetPolicy wires the admin-managed AuthPolicyService for live
	// length / complexity / HIBP overrides. Optional — implementations
	// that ignore the call must preserve the legacy hardcoded policy.
	SetPolicy(p *AuthPolicyService)
}

type argon2Params struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = argon2Params{
	memory:      19 * 1024, // 19 MiB
	iterations:  2,
	parallelism: 1,
	saltLength:  16,
	keyLength:   32,
}

type passwordService struct {
	params     argon2Params
	dummyHash  string
	hibpClient *http.Client
	hibpEnable bool
	logger     *slog.Logger
	// policy is the admin-managed length / complexity / HIBP source.
	// Nil falls back to the pre-policy hardcoded values (min=10,
	// max=128, no complexity, hibpEnable from constructor).
	policy *AuthPolicyService
}

// NewPasswordService constructs the service. Set hibpEnabled=false for
// air-gapped deployments or when the outbound HTTPS call is undesirable.
// Pass nil for policy in tests / setup wizard contexts where the
// ConfigService is not yet available; the service then falls back to
// the legacy hardcoded length bounds and the constructor hibpEnabled
// flag.
func NewPasswordService(logger *slog.Logger, hibpEnabled bool) PasswordService {
	// Precompute a dummy hash once at startup so the "user not found"
	// path can call Verify against it in constant time.
	dummy, err := hashPassword("__orkestra_dummy_password__", defaultParams)
	if err != nil {
		// Fall back to a literal that still exercises the decode path.
		dummy = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}
	return &passwordService{
		params:     defaultParams,
		dummyHash:  dummy,
		hibpClient: &http.Client{Timeout: 3 * time.Second},
		hibpEnable: hibpEnabled,
		logger:     logger,
	}
}

// SetPolicy wires the admin-managed AuthPolicyService so ValidatePolicy
// can read live length / complexity / HIBP toggles. Optional — if not
// called, the service preserves the legacy hardcoded behaviour.
func (s *passwordService) SetPolicy(p *AuthPolicyService) {
	s.policy = p
}

func (s *passwordService) Hash(plaintext string) (string, error) {
	return hashPassword(plaintext, s.params)
}

func (s *passwordService) Verify(plaintext, encoded string) (bool, error) {
	params, salt, hash, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(plaintext), salt, params.iterations, params.memory, params.parallelism, params.keyLength)
	if subtle.ConstantTimeCompare(hash, candidate) == 1 {
		return true, nil
	}
	return false, nil
}

func (s *passwordService) NeedsRehash(encoded string) bool {
	params, _, _, err := decodeHash(encoded)
	if err != nil {
		return true
	}
	return params.memory != s.params.memory ||
		params.iterations != s.params.iterations ||
		params.parallelism != s.params.parallelism ||
		params.keyLength != s.params.keyLength
}

func (s *passwordService) DummyHash() string {
	return s.dummyHash
}

// ValidatePolicy enforces the password policy. Length bounds and the
// required-character-class flags come from the admin-managed
// AuthPolicyService when wired; without a policy the service falls
// back to the legacy hardcoded bounds (10..128, no complexity).
// HIBP behaviour: when a policy is wired, the policy's
// breachedPasswordCheck toggle owns the decision; without a policy the
// constructor hibpEnabled flag is consulted instead.
func (s *passwordService) ValidatePolicy(ctx context.Context, plaintext, email string) error {
	// Policy lookups go through the nil-tolerant accessor so unwired
	// callers (setup wizard, tests) get the legacy defaults.
	pol := s.policy.PasswordPolicy(ctx)

	count := utf8.RuneCountInString(plaintext)
	if count < pol.MinLength {
		return ErrPasswordTooShort
	}
	if count > pol.MaxLength {
		return ErrPasswordTooLong
	}
	if email != "" {
		localPart := strings.ToLower(strings.Split(email, "@")[0])
		if len(localPart) >= 4 && strings.Contains(strings.ToLower(plaintext), localPart) {
			return ErrPasswordContainsEmail
		}
	}
	if err := checkCharacterClasses(plaintext, pol); err != nil {
		return err
	}

	// Policy controls HIBP when wired; fall back to constructor flag
	// only for unwired callers (setup wizard, tests).
	hibpOn := s.hibpEnable
	if s.policy != nil {
		hibpOn = pol.BreachedPasswordCheck
	}
	if hibpOn {
		breached, err := s.checkHIBP(ctx, plaintext)
		if err != nil {
			s.logger.Warn("HIBP check failed, continuing", slog.String("error", err.Error()))
			return nil
		}
		if breached {
			return ErrPasswordBreached
		}
	}
	return nil
}

// checkCharacterClasses enforces the required-class flags. Each flag
// is independent — operators can require any combination, or none.
func checkCharacterClasses(plaintext string, pol PasswordPolicy) error {
	if !pol.RequireUpper && !pol.RequireLower && !pol.RequireDigit && !pol.RequireSymbol {
		return nil
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range plaintext {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			// Anything that isn't a letter or digit counts as a
			// symbol — we deliberately avoid pinning a specific
			// allowlist so passphrase users with unusual punctuation
			// still satisfy the requirement.
			hasSymbol = true
		}
	}
	if pol.RequireUpper && !hasUpper {
		return ErrPasswordMissingUpper
	}
	if pol.RequireLower && !hasLower {
		return ErrPasswordMissingLower
	}
	if pol.RequireDigit && !hasDigit {
		return ErrPasswordMissingDigit
	}
	if pol.RequireSymbol && !hasSymbol {
		return ErrPasswordMissingSymbol
	}
	return nil
}

// checkHIBP queries the HaveIBeenPwned range API with k-anonymity.
// Only the first 5 hex chars of the SHA-1 hash leave the server.
func (s *passwordService) checkHIBP(ctx context.Context, plaintext string) (bool, error) {
	sum := sha1.Sum([]byte(plaintext))
	hashHex := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hashHex[:5], hashHex[5:]

	url := "https://api.pwnedpasswords.com/range/" + prefix
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Add-Padding", "true")
	req.Header.Set("User-Agent", "orkestra-auth")

	resp, err := s.hibpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("hibp: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], suffix) {
			// Any non-zero count means the hash is in the corpus.
			if count, _ := strconv.Atoi(parts[1]); count > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

// hashPassword produces a PHC-formatted argon2id hash.
func hashPassword(plaintext string, p argon2Params) (string, error) {
	salt := make([]byte, p.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plaintext), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		p.memory,
		p.iterations,
		p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return encoded, nil
}

// decodeHash parses a PHC-formatted argon2id string.
func decodeHash(encoded string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	if version != argon2.Version {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	p := argon2Params{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argon2Params{}, nil, nil, ErrPasswordInvalidHash
	}
	p.saltLength = uint32(len(salt))
	p.keyLength = uint32(len(hash))
	return p, salt, hash, nil
}
