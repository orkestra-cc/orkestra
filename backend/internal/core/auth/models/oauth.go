package models

import (
	userModels "github.com/orkestra/backend/internal/core/user/models"
)

type OAuthCallbackRequest struct {
	Code  string `json:"code" query:"code" validate:"required"`
	State string `json:"state" query:"state" validate:"required"`
	Error string `json:"error,omitempty" query:"error"`
}

type AppleCallbackRequest struct {
	Code    string `json:"code" form:"code" validate:"required"`
	State   string `json:"state" form:"state" validate:"required"`
	IDToken string `json:"id_token" form:"id_token"`
	User    string `json:"user" form:"user"`
	Error   string `json:"error,omitempty" form:"error"`
}

type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
}

type AppleUserInfo struct {
	Sub            string `json:"sub"`
	Email          string `json:"email"`
	EmailVerified  string `json:"email_verified"`
	IsPrivateEmail string `json:"is_private_email"`
	RealUserStatus int    `json:"real_user_status"`
	Name           *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name,omitempty"`
}

type GoogleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
}

type AppleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
}

type OAuthConfig struct {
	Provider     OAuthProvider
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OAuthLoginRequest struct {
	RedirectURL string `json:"redirectUrl,omitempty"`
	DeviceID    string `json:"deviceId,omitempty"`
}

type OAuthLoginResponse struct {
	AuthURL string `json:"authUrl"`
	State   string `json:"state"`
}

type DiscordUserInfo struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Verified      bool   `json:"verified"`
	GlobalName    string `json:"global_name"`
	Avatar        string `json:"avatar"`
	Discriminator string `json:"discriminator"`
	Locale        string `json:"locale"`
}

type DiscordTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

type GitHubUserInfo struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Location  string `json:"location"`
	Bio       string `json:"bio"`
}

type GitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type AuthCallbackOutputBody struct {
	AccessToken  string           `json:"accessToken"`
	RefreshToken string           `json:"refreshToken"`
	TokenType    string           `json:"tokenType"`
	ExpiresIn    int              `json:"expiresIn"`
	User         *userModels.User `json:"user"`
}

// OAuthProviderTokens represents OAuth tokens received from providers
type OAuthProviderTokens struct {
	AccessToken           string   `json:"accessToken"`
	RefreshToken          string   `json:"refreshToken,omitempty"`
	TokenType             string   `json:"tokenType,omitempty"`
	ExpiresIn             int      `json:"expiresIn,omitempty"`             // Seconds until access token expires
	RefreshTokenExpiresIn int      `json:"refreshTokenExpiresIn,omitempty"` // Seconds until refresh token expires (if provided)
	Scopes                []string `json:"scopes,omitempty"`
	IDToken               string   `json:"idToken,omitempty"` // For Apple/Google ID tokens
}
