package utils

import (
	"time"

	"github.com/user00265/dxclustergoapi/backend/lotw"
)

// GetLoTWMemberValue calculates the number of days since last LoTW upload.
// This matches the WaveLog API behavior which returns:
// - The number of days since last upload (e.g., "2", "31", etc.) if the callsign was found
// - false if the callsign was not found in the LoTW database
func GetLoTWMemberValue(lotwActivity *lotw.UserActivity) interface{} {
	if lotwActivity == nil {
		return false
	}

	// Calculate days since last upload
	now := time.Now().UTC()
	daysSinceUpload := int(now.Sub(lotwActivity.LastUploadUTC).Hours() / 24)

	// Return the number of days as the value (matching WaveLog API behavior)
	return daysSinceUpload
}
