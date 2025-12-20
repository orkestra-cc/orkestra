# OAuth Authentication Setup Guide

This guide will walk you through setting up OAuth 2.0 authentication using Google and Apple Sign In.

## Prerequisites

- Access to Google Cloud Console
- Apple Developer account (for Apple Sign In)
- Orkestra backend running on your domain

## Google OAuth Setup

### Step 1: Access Google Cloud Console

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Sign in with your Google account
3. Create a new project or select an existing one

### Step 2: Enable Required APIs

1. Navigate to **APIs & Services** → **Library**
2. Search for and enable:
   - Google+ API
   - Google Identity Toolkit API

### Step 3: Configure OAuth Consent Screen

1. Go to **APIs & Services** → **OAuth consent screen**
2. Choose **External** user type (unless for internal organization)
3. Fill in the required information:
   - **App name**: Orkestra
   - **User support email**: Your support email
   - **App logo**: Upload your logo (optional)
   - **Application home page**: `https://yourdomain.com`
   - **Authorized domains**: Add your domain(s)
   - **Developer contact information**: Your email
4. Add scopes:
   - `openid`
   - `email`
   - `profile`
5. Save and continue

### Step 4: Create OAuth 2.0 Credentials

1. Navigate to **APIs & Services** → **Credentials**
2. Click **Create Credentials** → **OAuth client ID**
3. Select **Web application** as the application type
4. Configure:
   - **Name**: Orkestra Web Client
   - **Authorized JavaScript origins**:
     - `https://yourdomain.com`
     - `http://localhost:8080` (for development)
   - **Authorized redirect URIs**:
     - `https://yourdomain.com/auth/google/callback`
     - `http://localhost:3000/auth/google/callback` (for development)
5. Click **Create**
6. Save the **Client ID** and **Client Secret**

### Step 5: Mobile App Configuration (if needed)

1. Create another OAuth client ID
2. Select **iOS** or **Android** as the application type
3. For iOS:
   - Enter your Bundle ID
4. For Android:
   - Enter your Package name
   - Enter your SHA-1 certificate fingerprint
5. Save the configuration

## Apple Sign In Setup

### Step 1: Access Apple Developer Account

1. Go to [Apple Developer](https://developer.apple.com/)
2. Sign in with your Apple ID
3. Ensure you have an active Developer Program membership

### Step 2: Register App ID

1. Navigate to **Certificates, Identifiers & Profiles**
2. Click **Identifiers** → **+** button
3. Select **App IDs** and click **Continue**
4. Select **App** and click **Continue**
5. Fill in:
   - **Description**: Orkestra
   - **Bundle ID**: Choose **Explicit** and enter `com.yourdomain.orkestra`
6. Under **Capabilities**, enable:
   - **Sign In with Apple**
7. Click **Continue** and **Register**

### Step 3: Create Service ID (for Web)

1. In **Identifiers**, click **+** button
2. Select **Services IDs** and click **Continue**
3. Fill in:
   - **Description**: Orkestra Web Service
   - **Identifier**: `com.yourdomain.orkestra.web`
4. Click **Continue** and **Register**
5. Click on the created Service ID to configure
6. Enable **Sign In with Apple**
7. Click **Configure** next to Sign In with Apple
8. Set:
   - **Primary App ID**: Select your App ID from Step 2
   - **Domains and Subdomains**: Add your domain (e.g., `yourdomain.com`)
   - **Return URLs**: Add:
     - `https://yourdomain.com/auth/apple/callback`
     - `http://localhost:3000/auth/apple/callback` (for development)
9. Click **Next**, **Done**, and **Continue**
10. Click **Save**

### Step 4: Create Private Key

1. Navigate to **Keys** → **+** button
2. Enter a **Key Name**: Orkestra Auth Key
3. Enable **Sign In with Apple**
4. Click **Continue** and **Register**
5. **Download the private key** (`.p8` file)
   - **IMPORTANT**: Save this file securely, you can't download it again
6. Note the **Key ID** shown on the page

### Step 5: Gather Required Information

You'll need:

- **Team ID**: Found in your Apple Developer account (top right corner)
- **Service ID**: The identifier created in Step 3 (e.g., `com.yourdomain.orkestra.web`)
- **Key ID**: From Step 4
- **Private Key**: The `.p8` file downloaded in Step 4

## Environment Configuration

### Backend Configuration

Create or update your `.env` file with the following variables:

```bash
# Google OAuth
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret
GOOGLE_REDIRECT_URI=https://yourdomain.com/auth/google/callback

# Apple Sign In
APPLE_TEAM_ID=your-team-id
APPLE_SERVICE_ID=com.yourdomain.orkestra.web
APPLE_KEY_ID=your-key-id
APPLE_PRIVATE_KEY_PATH=/path/to/AuthKey_XXXXXX.p8
APPLE_REDIRECT_URI=https://yourdomain.com/auth/apple/callback

# JWT Configuration
JWT_SECRET=your-secure-jwt-secret
JWT_REFRESH_SECRET=your-secure-refresh-secret
```

### Frontend Configuration

Update your frontend environment variables:

```bash
# .env.production
VITE_API_URL=https://yourdomain.com
VITE_GOOGLE_CLIENT_ID=your-google-client-id
VITE_APPLE_CLIENT_ID=com.yourdomain.orkestra.web

# .env.development
VITE_API_URL=http://localhost:3000
VITE_GOOGLE_CLIENT_ID=your-google-client-id
VITE_APPLE_CLIENT_ID=com.yourdomain.orkestra.web
```

### Mobile Configuration

For Flutter mobile app, update:

**iOS** - `ios/Runner/Info.plist`:

```xml
<key>CFBundleURLTypes</key>
<array>
    <dict>
        <key>CFBundleURLSchemes</key>
        <array>
            <string>com.googleusercontent.apps.YOUR_GOOGLE_IOS_CLIENT_ID</string>
        </array>
    </dict>
</array>
```

**Android** - Add Google services configuration file to `android/app/google-services.json`

## Testing OAuth Integration

### Local Development Testing

1. Ensure your backend is running on `http://localhost:3000`
2. Start your frontend on `http://localhost:8080`
3. Test Google Sign In:
   - Click "Sign in with Google"
   - Complete the Google authentication flow
   - Verify redirect back to your app
4. Test Apple Sign In:
   - Click "Sign in with Apple"
   - Complete the Apple authentication flow
   - Verify redirect back to your app

### Production Testing

1. Deploy your application to your domain
2. Ensure HTTPS is properly configured
3. Test both authentication flows
4. Verify tokens are properly generated and stored

## Security Best Practices

1. **Never commit secrets** to version control
2. **Use environment variables** for all sensitive configuration
3. **Implement HTTPS** for production deployments
4. **Rotate keys periodically**
5. **Implement rate limiting** on authentication endpoints
6. **Log authentication attempts** for security monitoring
7. **Use secure session management** with JWT tokens
8. **Implement refresh token rotation**

## Troubleshooting

### Common Google OAuth Issues

**Error: redirect_uri_mismatch**

- Ensure the redirect URI in your request matches exactly with configured URIs
- Check for trailing slashes and protocol (http vs https)

**Error: invalid_client**

- Verify Client ID and Client Secret are correct
- Check if the OAuth consent screen is properly configured

### Common Apple Sign In Issues

**Error: invalid_grant**

- Ensure the authorization code is used only once
- Verify the redirect URI matches exactly

**Error: invalid_client**

- Check Team ID, Service ID, and Key ID are correct
- Verify the private key file is accessible and valid

**JWT Token Issues**

- Ensure the private key file has correct permissions
- Verify the key hasn't expired
- Check the algorithm matches (ES256 for Apple)

## Support

For additional help:

- Google OAuth: [Google Identity Platform Documentation](https://developers.google.com/identity)
- Apple Sign In: [Sign in with Apple Documentation](https://developer.apple.com/sign-in-with-apple/)
- Orkestra Issues: Contact your system administrator or development team

## Checklist

Before going to production, ensure:

- [ ] Google OAuth credentials created and configured
- [ ] Apple Sign In credentials created and configured
- [ ] Environment variables set for all environments
- [ ] Redirect URIs configured for production domain
- [ ] HTTPS enabled on production
- [ ] OAuth tested in development environment
- [ ] OAuth tested in production environment
- [ ] Security best practices implemented
- [ ] Monitoring and logging configured
- [ ] Documentation shared with team
