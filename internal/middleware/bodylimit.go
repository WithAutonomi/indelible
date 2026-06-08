package middleware

import (
	"net/http"
	"strings"
)

// MaxBodySize limits request body size for non-multipart requests.
// Multipart requests (file uploads) are skipped since they have their
// own size limit via max_upload_size_bytes in the upload handler.
// NDJSON requests (the bulk uploads import) are likewise skipped: a catalog
// restore is legitimately larger than the JSON cap and is streamed line-by-line
// through its own bounded scanner rather than buffered.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "multipart/form-data") && !strings.HasPrefix(ct, "application/x-ndjson") {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
