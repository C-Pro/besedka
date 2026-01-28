package http

import (
	"besedka/internal/auth"
	"io/fs"
	"net/http"
	"strings"
)

func NewFileServerHandler(authService *auth.AuthService, assets fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(assets))

	return func(w http.ResponseWriter, r *http.Request) {
		// For / and /index.html check for a valid token and redirect to login if not found.
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			// Check cookie
			cookie, err := r.Cookie("token")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}

			// Validate token
			if _, err := authService.GetUserID(cookie.Value); err != nil {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}
		}

		// Prevent serving the static.go file
		if strings.HasSuffix(r.URL.Path, "/static.go") {
			http.NotFound(w, r)
			return
		}

		// Default to file server
		fileServer.ServeHTTP(w, r)
	}
}
