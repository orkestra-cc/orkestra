#!/bin/bash

# JWT Key Generation Script
# Generates RSA key pair for JWT signing with RS256 algorithm

set -e

# Configuration
KEY_SIZE=2048
KEYS_DIR="$(dirname "$0")/../docker/keys"
PRIVATE_KEY_FILE="$KEYS_DIR/jwt-private.pem"
PUBLIC_KEY_FILE="$KEYS_DIR/jwt-public.pem"

echo "🔐 Generating JWT RSA Key Pair..."

# Create keys directory if it doesn't exist
mkdir -p "$KEYS_DIR"

# Generate private key
echo "📝 Generating private key ($KEY_SIZE bits)..."
openssl genrsa -out "$PRIVATE_KEY_FILE" $KEY_SIZE

# Extract public key from private key
echo "🔑 Extracting public key..."
openssl rsa -in "$PRIVATE_KEY_FILE" -pubout -out "$PUBLIC_KEY_FILE"

# Set proper permissions
echo "🔒 Setting secure permissions..."
chmod 600 "$PRIVATE_KEY_FILE"  # Read/write for owner only
chmod 644 "$PUBLIC_KEY_FILE"   # Read for owner, group, others

# Verify keys
echo "✅ Verifying key pair..."
if openssl rsa -in "$PRIVATE_KEY_FILE" -check -noout > /dev/null 2>&1; then
    echo "✓ Private key is valid"
else
    echo "❌ Private key is invalid"
    exit 1
fi

if openssl rsa -in "$PUBLIC_KEY_FILE" -pubin -noout > /dev/null 2>&1; then
    echo "✓ Public key is valid"
else
    echo "❌ Public key is invalid"
    exit 1
fi

echo ""
echo "🎉 JWT keys generated successfully!"
echo "📁 Private key: $PRIVATE_KEY_FILE"
echo "📁 Public key:  $PUBLIC_KEY_FILE"
echo ""
echo "⚠️  SECURITY NOTES:"
echo "   - Keep the private key secure and never commit to version control"
echo "   - The public key can be safely distributed"
echo "   - For production, consider using a secure key management service"
echo ""
echo "Next steps:"
echo "1. Update .env file with key paths"
echo "2. Restart the backend service"
echo "3. All existing JWT tokens will be invalidated"