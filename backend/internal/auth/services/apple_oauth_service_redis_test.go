package services

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// MockRedisClient implements RedisClient interface for testing
type MockRedisClient struct {
	data   map[string]string
	expiry map[string]time.Time
}

func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data:   make(map[string]string),
		expiry: make(map[string]time.Time),
	}
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	m.data[key] = fmt.Sprintf("%v", value)
	if expiration > 0 {
		m.expiry[key] = time.Now().Add(expiration)
	}
	return nil
}

func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	// Check expiry
	if expTime, exists := m.expiry[key]; exists && time.Now().After(expTime) {
		delete(m.data, key)
		delete(m.expiry, key)
		return "", fmt.Errorf("key expired")
	}

	value, exists := m.data[key]
	if !exists {
		return "", fmt.Errorf("key not found")
	}

	return value, nil
}

func (m *MockRedisClient) Del(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		delete(m.data, key)
		delete(m.expiry, key)
	}
	return nil
}

func (m *MockRedisClient) Keys(ctx context.Context, pattern string) ([]string, error) {
	var keys []string
	prefix := "apple:pubkey:"

	// Simple pattern matching for testing
	for key := range m.data {
		if pattern == "*" ||
			(pattern == "apple:pubkey:*" && len(key) >= len(prefix) && key[:len(prefix)] == prefix) {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// TestAppleOAuthService_RedisCacheBasics tests the basic Redis caching functionality
func TestAppleOAuthService_RedisCacheBasics(t *testing.T) {
	config := &OAuthProviderConfig{
		ClientID: "test-client-id",
	}

	mockRedis := NewMockRedisClient()
	service, err := NewAppleOAuthService(config, mockRedis)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	appleService := service.(*appleOAuthService)
	ctx := context.Background()

	// Test initial empty cache stats
	stats, err := appleService.getCacheStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get cache stats: %v", err)
	}

	if stats["total_keys"].(int) != 0 {
		t.Errorf("Empty cache should have 0 total keys")
	}
	if stats["cache_ttl_hours"].(float64) != 24.0 {
		t.Errorf("Default cache TTL should be 24 hours, got %f", stats["cache_ttl_hours"].(float64))
	}

	// Test cache invalidation on empty cache
	err = appleService.invalidateKeyCache(ctx)
	if err != nil {
		t.Errorf("Cache invalidation should not fail on empty cache: %v", err)
	}

	t.Logf("✅ Redis cache basics test completed successfully")
}

// TestAppleOAuthService_RedisKeySerialization tests key serialization/deserialization
func TestAppleOAuthService_RedisKeySerialization(t *testing.T) {
	config := &OAuthProviderConfig{
		ClientID: "test-client-id",
	}

	mockRedis := NewMockRedisClient()
	service, err := NewAppleOAuthService(config, mockRedis)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	appleService := service.(*appleOAuthService)

	// Create a test RSA public key using the existing conversion method
	// These are test values from Apple's documentation
	testN := "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw"
	testE := "AQAB"

	// Test key conversion and serialization
	publicKey, err := appleService.convertJWKToRSAPublicKey(testN, testE)
	if err != nil {
		t.Fatalf("Failed to convert JWK to RSA key: %v", err)
	}

	// Test serialization
	keyData, err := appleService.serializePublicKey(publicKey)
	if err != nil {
		t.Fatalf("Failed to serialize public key: %v", err)
	}

	// Test deserialization
	deserializedKey, err := appleService.deserializePublicKey(keyData)
	if err != nil {
		t.Fatalf("Failed to deserialize public key: %v", err)
	}

	// Verify the keys are equivalent
	originalKey := publicKey
	restoredKey := deserializedKey.(*rsa.PublicKey)

	if originalKey.N.Cmp(restoredKey.N) != 0 {
		t.Errorf("Modulus doesn't match after serialization/deserialization")
	}

	if originalKey.E != restoredKey.E {
		t.Errorf("Exponent doesn't match after serialization/deserialization")
	}

	t.Logf("✅ Key serialization test completed successfully")
}

// TestAppleOAuthService_RedisCacheInvalidation tests cache invalidation
func TestAppleOAuthService_RedisCacheInvalidation(t *testing.T) {
	config := &OAuthProviderConfig{
		ClientID: "test-client-id",
	}

	mockRedis := NewMockRedisClient()
	service, err := NewAppleOAuthService(config, mockRedis)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	appleService := service.(*appleOAuthService)
	ctx := context.Background()

	// Create valid cached public key entries
	testCached := cachedPublicKey{
		KeyData:   []byte(`{"n":"test-modulus","e":"AQAB"}`),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	testData1, _ := json.Marshal(testCached)
	testData2, _ := json.Marshal(testCached)

	cacheKey1 := appleService.buildCacheKey("test-key-1")
	cacheKey2 := appleService.buildCacheKey("test-key-2")

	mockRedis.Set(ctx, cacheKey1, string(testData1), time.Hour)
	mockRedis.Set(ctx, cacheKey2, string(testData2), time.Hour)

	// Verify they exist
	stats, err := appleService.getCacheStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get cache stats: %v", err)
	}

	if stats["total_keys"].(int) != 2 {
		t.Errorf("Expected 2 cache entries, got %d", stats["total_keys"].(int))
	}

	// Test invalidation
	err = appleService.invalidateKeyCache(ctx)
	if err != nil {
		t.Fatalf("Failed to invalidate cache: %v", err)
	}

	// Verify cache is empty
	stats, err = appleService.getCacheStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get cache stats after invalidation: %v", err)
	}

	if stats["total_keys"].(int) != 0 {
		t.Errorf("Cache should be empty after invalidation, got %d keys", stats["total_keys"].(int))
	}

	t.Logf("✅ Cache invalidation test completed successfully")
}

// TestAppleOAuthService_CacheKeyPatterns tests cache key generation
func TestAppleOAuthService_CacheKeyPatterns(t *testing.T) {
	config := &OAuthProviderConfig{
		ClientID: "test-client-id",
	}

	mockRedis := NewMockRedisClient()
	service, err := NewAppleOAuthService(config, mockRedis)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	appleService := service.(*appleOAuthService)

	// Test cache key generation
	testKeyID := "ABCD1234"
	expectedKey := "apple:pubkey:ABCD1234"
	actualKey := appleService.buildCacheKey(testKeyID)

	if actualKey != expectedKey {
		t.Errorf("Expected cache key %s, got %s", expectedKey, actualKey)
	}

	// Test pattern matching
	wildcard := appleService.buildCacheKey("*")
	expectedPattern := "apple:pubkey:*"

	if wildcard != expectedPattern {
		t.Errorf("Expected pattern %s, got %s", expectedPattern, wildcard)
	}

	t.Logf("✅ Cache key patterns test completed successfully")
}
