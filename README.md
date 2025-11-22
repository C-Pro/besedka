# Besedka

Besedka is a modern, self-hosted chat application built with Go and vanilla web technologies. It aims to provide a seamless and responsive communication experience across devices.

## 🚧 Work in Progress

**Note:** This project is currently under active development. Features, APIs, and data structures are subject to change. The current implementation relies on in-memory stub data for demonstration and testing purposes.

## Features

- **Real-time Messaging**: Instant message delivery using WebSockets.
- **Responsive UI**: A fluid interface that adapts to both desktop and mobile workflows.
- **User Presence**: Real-time status updates (Online/Offline) and "Last Seen" timestamps.
- **Chat Management**: Support for Direct Messages (DMs) and Townhall-style group chats.

## Tech Stack

- **Backend**: Go (Golang)
- **Frontend**: HTML5, CSS3, Vanilla JavaScript
- **Protocol**: WebSockets for real-time events

## Getting Started

To run the application locally:

1. Ensure you have Go installed (1.25+).
2. Start the server:
   ```bash
   go run main.go
   ```
3. Open your browser and navigate to `http://localhost:8080`.

## Future Roadmap

- [ ] Persistent storage (Database integration)
- [ ] User authentication and session management
- [ ] File attachments and media support
- [ ] Rich text editing
- [ ] Emojis and reactions
