package http

import (
	"besedka/internal/auth"
	"net/http"
	"strings"
)

func NewFileServerHandler(authService *auth.AuthService, root string) http.HandlerFunc {
	fs := http.FileServer(http.Dir(root))

	return func(w http.ResponseWriter, r *http.Request) {
		// public paths that don't need auth
		publicPaths := []string{"/login.html", "/register.html", "/css/", "/js/"}

		// If it matches a public path prefix, serve it directly
		for _, prefix := range publicPaths {
			if strings.HasPrefix(r.URL.Path, prefix) {
				fs.ServeHTTP(w, r)
				return
			}
		}

		// Specific check for exactly root "/" or "/index.html"
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			// Check cookie
			cookie, err := r.Cookie("token")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login.html", http.StatusUnauthorized)
				return
			}

			// Validate token
			if _, err := authService.GetUserID(cookie.Value); err != nil {
				http.Redirect(w, r, "/login.html", http.StatusUnauthorized)
				return
			}
		}

		// Default to file server
		fs.ServeHTTP(w, r)
	}
}
