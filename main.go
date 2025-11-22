package main

import (
	"besedka/internal/api"
	"besedka/internal/ws"
	"log"
	"net/http"
)

func main() {
	// Serve static files
	fs := http.FileServer(http.Dir("."))
	http.Handle("/", fs)

	// API endpoints
	http.HandleFunc("/api/login", api.LoginHandler)
	http.HandleFunc("/api/logoff", api.LogoffHandler)
	http.HandleFunc("/api/users", api.UsersHandler)
	http.HandleFunc("/api/chats", api.ChatsHandler)

	// WebSocket endpoint
	http.HandleFunc("/api/chat", ws.HandleConnections)

	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}
