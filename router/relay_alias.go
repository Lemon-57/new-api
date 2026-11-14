package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// WithRelayAliases accepts the registered /v1 APIs without their version prefix.
// Normalize before Gin matches a route so aliases use the original middleware,
// authentication, channel selection and upstream request paths.
func WithRelayAliases(engine *gin.Engine) http.Handler {
	aliases := make(map[string]struct{})
	for _, route := range engine.Routes() {
		path, ok := strings.CutPrefix(route.Path, "/v1/")
		if !ok {
			continue
		}
		segments := strings.Split(path, "/")
		for i, segment := range segments {
			if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
				path = strings.Join(segments[:i], "/")
				break
			}
		}
		if path != "" {
			aliases["/"+path] = struct{}{}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for path := range aliases {
			if r.URL.Path != path && !strings.HasPrefix(r.URL.Path, path+"/") {
				continue
			}
			request := r.Clone(r.Context())
			request.URL.Path = "/v1" + r.URL.Path
			if r.URL.RawPath != "" {
				request.URL.RawPath = "/v1" + r.URL.RawPath
			}
			request.RequestURI = request.URL.RequestURI()
			engine.ServeHTTP(w, request)
			return
		}
		engine.ServeHTTP(w, r)
	})
}
