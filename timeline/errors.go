package timeline

import "strings"

// Error kinds for api_error intervals and events.
const (
	ErrorOverloaded = "overloaded"
	ErrorRateLimit  = "rate_limit"
	ErrorTimeout    = "timeout"
	ErrorAuth       = "auth"
	ErrorServer     = "server"
	ErrorNetwork    = "network"
	ErrorSleep      = "sleep"
	ErrorOther      = "other"
	// ErrorRetry is a request that got no response and was re-sent.
	ErrorRetry = "retry"
)

// ErrorKind types an API error message by its text.
func ErrorKind(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "went to sleep"):
		return ErrorSleep
	case strings.Contains(t, "overloaded") || strings.Contains(t, "529"):
		return ErrorOverloaded
	case strings.Contains(t, "rate limit") || strings.Contains(t, "429"):
		return ErrorRateLimit
	case strings.Contains(t, "timed out") || strings.Contains(t, "timeout"):
		return ErrorTimeout
	case strings.Contains(t, "login") || strings.Contains(t, "401") || strings.Contains(t, "auth"):
		return ErrorAuth
	case strings.Contains(t, "500") || strings.Contains(t, "502") || strings.Contains(t, "503") || strings.Contains(t, "internal server"):
		return ErrorServer
	case strings.Contains(t, "connection") || strings.Contains(t, "network") || strings.Contains(t, "econn"):
		return ErrorNetwork
	}
	return ErrorOther
}
