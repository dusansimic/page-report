package server

import "mime"

// Report bodies are handed back to browsers as top-level documents, so the
// content type decides how the browser parses them. Only these two forms are
// ever stored or emitted; anything else — `image/svg+xml` above all, which
// renders as a scriptable document — is refused at upload and downgraded on
// serve.
const (
	contentTypeHTML = "text/html; charset=utf-8"
	contentTypeText = "text/plain; charset=utf-8"
)

// defaultContentType is what an upload that names no content type is stored as.
const defaultContentType = contentTypeHTML

// canonicalContentType maps a client- or database-supplied content type onto
// one of the two allowed canonical forms, discarding any parameters the caller
// sent. ok=false means the media type is not on the allowlist. Allowlisting
// rather than denylisting is deliberate: the set of MIME types a browser will
// render as a document is not enumerable in advance.
func canonicalContentType(s string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(s)
	if err != nil {
		return "", false
	}
	switch mediaType {
	case "text/html":
		return contentTypeHTML, true
	case "text/plain":
		return contentTypeText, true
	}
	return "", false
}
