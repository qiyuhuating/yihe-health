package deviceprotocol

// ValidRequestID enforces the common SOS and durable telemetry event ID format.
func ValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '.' || c == ':' || c == '-') {
			return false
		}
	}
	return true
}
