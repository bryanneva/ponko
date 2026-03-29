package slack

import "strings"

var PermanentErrors = []string{
	"channel_not_found",
	"not_in_channel",
	"account_inactive",
	"invalid_auth",
	"token_revoked",
}

func IsPermanentError(err error) bool {
	msg := err.Error()
	for _, code := range PermanentErrors {
		if strings.Contains(msg, "slack API error: "+code) {
			return true
		}
	}
	return false
}

func IsCredentialError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "invalid_auth") || strings.Contains(msg, "token_revoked")
}
