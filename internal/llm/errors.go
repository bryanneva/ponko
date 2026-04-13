package llm

import "strings"

func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 401 and 403 are always permanent (auth/permission issues)
	if strings.Contains(msg, "claude API error (401)") || strings.Contains(msg, "claude API error (403)") {
		return true
	}
	// Specific 400 errors that are permanent (billing)
	if strings.Contains(msg, "credit balance is too low") {
		return true
	}
	return false
}
