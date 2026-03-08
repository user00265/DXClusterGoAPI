package httputil

import "net/http"

// HTTPDoer is a minimal interface for HTTP clients, enabling test injection.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
