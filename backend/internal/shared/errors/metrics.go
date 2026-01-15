package errors

import (
	"sync"
	"time"
)

// ErrorMetrics tracks error metrics for monitoring and alerting
type ErrorMetrics struct {
	mu sync.RWMutex

	// Error counters by category and code
	errorCounts   map[ErrorCategory]map[ErrorCode]int64
	errorRates    map[ErrorCategory]map[ErrorCode]*RateTracker
	panicCount    int64
	requestCounts map[int]int64 // HTTP status code -> count

	// Performance metrics
	responseTimeSum map[int]time.Duration // HTTP status code -> total response time
	requestCount    int64

	// Rate limiting metrics
	rateLimitHits  int64
	securityEvents int64

	// Time-based metrics
	lastReset    time.Time
	metricWindow time.Duration
}

// RateTracker tracks error rates over time
type RateTracker struct {
	count      int64
	lastUpdate time.Time
	window     time.Duration
	samples    []TimestampedCount
}

// TimestampedCount represents a count at a specific time
type TimestampedCount struct {
	timestamp time.Time
	count     int64
}

// NewErrorMetrics creates a new error metrics tracker
func NewErrorMetrics() *ErrorMetrics {
	return &ErrorMetrics{
		errorCounts:     make(map[ErrorCategory]map[ErrorCode]int64),
		errorRates:      make(map[ErrorCategory]map[ErrorCode]*RateTracker),
		requestCounts:   make(map[int]int64),
		responseTimeSum: make(map[int]time.Duration),
		lastReset:       time.Now(),
		metricWindow:    time.Hour, // 1-hour window for metrics
	}
}

// RecordError records an error occurrence
func (m *ErrorMetrics) RecordError(err *AppError) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Initialize maps if needed
	if m.errorCounts[err.Category] == nil {
		m.errorCounts[err.Category] = make(map[ErrorCode]int64)
	}
	if m.errorRates[err.Category] == nil {
		m.errorRates[err.Category] = make(map[ErrorCode]*RateTracker)
	}

	// Increment error count
	m.errorCounts[err.Category][err.Code]++

	// Track error rate
	if m.errorRates[err.Category][err.Code] == nil {
		m.errorRates[err.Category][err.Code] = &RateTracker{
			window:  m.metricWindow,
			samples: make([]TimestampedCount, 0),
		}
	}
	m.errorRates[err.Category][err.Code].Record()

	// Record HTTP status
	m.requestCounts[err.HTTPStatus]++

	// Special tracking for security and rate limit events
	if err.Category == CategorySecurity {
		m.securityEvents++
	}
	if err.Category == CategoryRateLimit {
		m.rateLimitHits++
	}
}

// RecordPanic records a panic occurrence
func (m *ErrorMetrics) RecordPanic() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.panicCount++
	m.requestCounts[500]++ // Panics result in 500 status
}

// RecordRequest records a request completion
func (m *ErrorMetrics) RecordRequest(statusCode int, method, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestCounts[statusCode]++
	m.requestCount++
}

// GetErrorCounts returns error counts by category and code
func (m *ErrorMetrics) GetErrorCounts() map[ErrorCategory]map[ErrorCode]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a deep copy
	result := make(map[ErrorCategory]map[ErrorCode]int64)
	for category, codes := range m.errorCounts {
		result[category] = make(map[ErrorCode]int64)
		for code, count := range codes {
			result[category][code] = count
		}
	}

	return result
}

// GetErrorRate returns the error rate for a specific category and code
func (m *ErrorMetrics) GetErrorRate(category ErrorCategory, code ErrorCode) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.errorRates[category] == nil || m.errorRates[category][code] == nil {
		return 0.0
	}

	return m.errorRates[category][code].GetRate()
}

// GetOverallErrorRate returns the overall error rate (errors/requests)
func (m *ErrorMetrics) GetOverallErrorRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.requestCount == 0 {
		return 0.0
	}

	totalErrors := int64(0)
	for _, codes := range m.errorCounts {
		for _, count := range codes {
			totalErrors += count
		}
	}

	return float64(totalErrors) / float64(m.requestCount)
}

// GetStatusCodeDistribution returns the distribution of HTTP status codes
func (m *ErrorMetrics) GetStatusCodeDistribution() map[int]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[int]int64)
	for status, count := range m.requestCounts {
		result[status] = count
	}

	return result
}

// GetSecurityMetrics returns security-related metrics
func (m *ErrorMetrics) GetSecurityMetrics() SecurityMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return SecurityMetrics{
		SecurityEvents: m.securityEvents,
		RateLimitHits:  m.rateLimitHits,
		PanicCount:     m.panicCount,
		TotalRequests:  m.requestCount,
		WindowStart:    m.lastReset,
		WindowDuration: m.metricWindow,
	}
}

// GetTopErrors returns the most frequent errors
func (m *ErrorMetrics) GetTopErrors(limit int) []ErrorSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errors []ErrorSummary
	for category, codes := range m.errorCounts {
		for code, count := range codes {
			errors = append(errors, ErrorSummary{
				Category: category,
				Code:     code,
				Count:    count,
				Rate:     m.errorRates[category][code].GetRate(),
			})
		}
	}

	// Sort by count (simple bubble sort for small datasets)
	for i := 0; i < len(errors)-1; i++ {
		for j := 0; j < len(errors)-i-1; j++ {
			if errors[j].Count < errors[j+1].Count {
				errors[j], errors[j+1] = errors[j+1], errors[j]
			}
		}
	}

	if len(errors) > limit {
		errors = errors[:limit]
	}

	return errors
}

// Reset resets all metrics (useful for periodic cleanup)
func (m *ErrorMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorCounts = make(map[ErrorCategory]map[ErrorCode]int64)
	m.errorRates = make(map[ErrorCategory]map[ErrorCode]*RateTracker)
	m.requestCounts = make(map[int]int64)
	m.responseTimeSum = make(map[int]time.Duration)
	m.panicCount = 0
	m.requestCount = 0
	m.rateLimitHits = 0
	m.securityEvents = 0
	m.lastReset = time.Now()
}

// IsHealthy checks if error rates are within acceptable thresholds
func (m *ErrorMetrics) IsHealthy() HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := HealthStatus{
		Healthy: true,
		Issues:  make([]string, 0),
	}

	// Check overall error rate
	overallRate := m.GetOverallErrorRate()
	if overallRate > 0.05 { // 5% error rate threshold
		status.Healthy = false
		status.Issues = append(status.Issues, "High overall error rate")
	}

	// Check for excessive panics
	if m.requestCount > 0 && float64(m.panicCount)/float64(m.requestCount) > 0.001 { // 0.1% panic rate
		status.Healthy = false
		status.Issues = append(status.Issues, "High panic rate")
	}

	// Check for security events
	if m.securityEvents > 10 {
		status.Healthy = false
		status.Issues = append(status.Issues, "High number of security events")
	}

	// Check 5xx error rate
	serverErrors := m.requestCounts[500] + m.requestCounts[502] + m.requestCounts[503] + m.requestCounts[504]
	if m.requestCount > 0 && float64(serverErrors)/float64(m.requestCount) > 0.01 { // 1% server error rate
		status.Healthy = false
		status.Issues = append(status.Issues, "High server error rate")
	}

	return status
}

// RateTracker methods

// Record records a new occurrence
func (rt *RateTracker) Record() {
	now := time.Now()
	rt.count++
	rt.lastUpdate = now

	// Add to samples
	rt.samples = append(rt.samples, TimestampedCount{
		timestamp: now,
		count:     1,
	})

	// Clean old samples
	rt.cleanOldSamples()
}

// GetRate returns the current rate (occurrences per minute)
func (rt *RateTracker) GetRate() float64 {
	rt.cleanOldSamples()

	if len(rt.samples) == 0 {
		return 0.0
	}

	totalCount := int64(0)
	for _, sample := range rt.samples {
		totalCount += sample.count
	}

	windowMinutes := rt.window.Minutes()
	return float64(totalCount) / windowMinutes
}

// cleanOldSamples removes samples outside the time window
func (rt *RateTracker) cleanOldSamples() {
	cutoff := time.Now().Add(-rt.window)
	validSamples := make([]TimestampedCount, 0)

	for _, sample := range rt.samples {
		if sample.timestamp.After(cutoff) {
			validSamples = append(validSamples, sample)
		}
	}

	rt.samples = validSamples
}

// Supporting types

// SecurityMetrics represents security-related metrics
type SecurityMetrics struct {
	SecurityEvents int64         `json:"security_events"`
	RateLimitHits  int64         `json:"rate_limit_hits"`
	PanicCount     int64         `json:"panic_count"`
	TotalRequests  int64         `json:"total_requests"`
	WindowStart    time.Time     `json:"window_start"`
	WindowDuration time.Duration `json:"window_duration"`
}

// ErrorSummary represents a summary of error occurrences
type ErrorSummary struct {
	Category ErrorCategory `json:"category"`
	Code     ErrorCode     `json:"code"`
	Count    int64         `json:"count"`
	Rate     float64       `json:"rate"`
}

// HealthStatus represents the health status based on error metrics
type HealthStatus struct {
	Healthy bool     `json:"healthy"`
	Issues  []string `json:"issues"`
}
