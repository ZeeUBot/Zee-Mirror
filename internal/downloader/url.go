package downloader

import "strings"

// IsNonRangeURL reports whether url is served by a backend known to ignore
// HTTP Range headers (e.g. Google's video-downloads / Drive endpoints, which
// reply with the whole body regardless of Range).
//
// Range-based segmented downloads and resume probing against such backends
// make aria2 abort with errorCode=8 (Invalid range header), so callers should
// fall back to a plain single-connection GET without resume.
func IsNonRangeURL(url string) bool {
	nonRangePatterns := []string{
		"video-downloads.googleusercontent.com",
		"drive.google.com/uc?",
		"drive.google.com/uc&id=",
		"drive.usercontent.google.com",
	}

	for _, pattern := range nonRangePatterns {
		if strings.Contains(url, pattern) {
			return true
		}
	}
	return false
}
