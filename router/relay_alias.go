package router

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func WithRelayAliases(engine *gin.Engine) http.Handler {
	aliases := make(map[string]struct{})
	for _, route := range engine.Routes() {
		path, ok := strings.CutPrefix(route.Path, "/v1/")
		if !ok {
			continue
		}
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			if strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
				path = strings.Join(segments[:index], "/")
				break
			}
		}
		if path != "" {
			aliases["/"+path] = struct{}{}
		}
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		for path := range aliases {
			if incoming.URL.Path != path && !strings.HasPrefix(incoming.URL.Path, path+"/") {
				continue
			}
			request := incoming.Clone(incoming.Context())
			request.URL.Path = "/v1" + incoming.URL.Path
			if incoming.URL.RawPath != "" {
				request.URL.RawPath = "/v1" + incoming.URL.RawPath
			}
			request.RequestURI = request.URL.RequestURI()
			engine.ServeHTTP(writer, request)
			return
		}
		engine.ServeHTTP(writer, incoming)
	})
}
