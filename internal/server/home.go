package server

import (
	"net/http"

	"github.com/dusan/page-report/web"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, web.StaticFS, "static/index.html")
}
