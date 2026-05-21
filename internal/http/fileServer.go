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
			_, expiry, err := authService.GetUserID(cookie.Value)
			if err != nil {
				http.Redirect(w, r, "/login.html", http.StatusFound)
				return
			}

			if !expiry.IsZero() {
				http.SetCookie(w, &http.Cookie{
					Name:     "token",
					Value:    cookie.Value,
					HttpOnly: true,
					Secure:   true,
					Path:     "/",
					Expires:  expiry,
				})
			}
		}

		// Prevent serving the static.go file
		if strings.HasSuffix(r.URL.Path, "/static.go") {
			http.NotFound(w, r)
			return
		}

		// Set Cache-Control headers based on path
		path := r.URL.Path
		if path == "/" || strings.HasSuffix(path, ".html") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		} else if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		}

		// Default to file server
		fileServer.ServeHTTP(w, r)
	}
}
