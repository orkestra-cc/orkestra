package types

import (
	"context"
	"time"
)

// DeviceInfo contains information about the client device
type DeviceInfo struct {
	DeviceID    string    `json:"deviceId"`
	DeviceType  string    `json:"deviceType"`
	Platform    string    `json:"platform"`
	UserAgent   string    `json:"userAgent"`
	IP          string    `json:"ip"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"createdAt"`
}

// GetDeviceInfoFromContext retrieves device information from request context
func GetDeviceInfoFromContext(ctx context.Context) *DeviceInfo {
	if deviceInfo := ctx.Value("deviceInfo"); deviceInfo != nil {
		if di, ok := deviceInfo.(*DeviceInfo); ok {
			return di
		}
	}
	return nil
}
