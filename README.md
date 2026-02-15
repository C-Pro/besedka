# Besedka

[![Workflow Pipeline](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml/badge.svg)](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml)

Besedka is a self-hosted chat application for a limited number of users (e.g. for a family or a small group of friends) built with Go and vanilla web technologies.

## 🚧 Work in Progress

**Note:** This project is currently under active development with heavy use of AI (Google Antigravity).

## Tech Stack

- **Backend**: Go (Golang)
- **Frontend**: HTML5, CSS3, Vanilla JavaScript
- **Protocol**: JSON over WebSocket

## Getting Started

### Docker

1. Create a directory for data:
   ```bash
   mkdir -p $(pwd)/data
   ```

2. Run the server:

```bash
docker run -name besedka -v $(pwd)/data:/data -e AUTH_SECRET=your-secret-key \
   -e DB_PATH=/data/db -p 8080:8080 ghcr.io/c-pro/besedka:latest
```

3. Create a user:

```bash
docker exec besedka -add-user username
```

4. It will output the registration link, follow it in the browser to register the user.


### Local development

To run locally from source:

1. Ensure you have Go installed (1.25+).
2. Start the server:
   ```bash
   AUTH_SECRET=your-secret-key go run main.go
   ```
3. Create a user (in the different terminal):
   ```bash
   go run main.go -add-user user
   ```
4. It will output the registration link, follow it in the browser to register the user.

## Future Roadmap

- [x] Realtime updates for user presence/creation/deletion
- [ ] User profile/settings
- [ ] Password reset
- [ ] User deletion
- [ ] Admin UI
- [ ] File attachments and media support
- [ ] Infinite scrolling
- [ ] Rich text editing
- [ ] Emojis and reactions
