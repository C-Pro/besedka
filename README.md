# Besedka

[![Workflow Pipeline](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml/badge.svg)](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml)

Besedka is a self-hosted chat application for a limited number of users (e.g. for a family or a small group of friends) built with Go and vanilla web technologies.

It is designed to be self-sufficient without any hard dependencies on external services. All CSS and JS is self hosted, no 3rd party APIs or CDNs are used. It does not require nor support authentication with external OAuth providers (login with Google, Apple, etc.).
It does not track you or share your data with anyone.
## 🚧 Work in Progress

**Note:** This project is currently under active development with heavy use of AI (Google Antigravity).

## Tech Stack

- **Backend**: Go
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
docker run --name besedka -v $(pwd)/data:/data -e AUTH_SECRET=your-secret-key \
   -e BESEDKA_DB=/data/db -e ADMIN_ADDR=:8081 -p 8080:8080 -p 8081:8081 \
   ghcr.io/c-pro/besedka:latest
```

3. Access the Admin UI at [http://localhost:8081](http://localhost:8081) to manage users.
   - Default credentials: `admin` / `1337chat`

4. Create a user and follow the provided registration link to register.
5. Chat is available at [http://localhost:8080](http://localhost:8080)


### Local development

To run locally from source:

1. Ensure you have Go installed (1.25+).
2. Start the server:
   ```bash
   AUTH_SECRET=your-secret-key go run main.go
   ```
3. Access the Admin UI at [http://localhost:8081](http://localhost:8081) to manage users.
   - Default credentials: `admin` / `1337chat`
4. Create a user and follow the provided registration link to register.
5. Chat is available at [http://localhost:8080](http://localhost:8080)

## Configuration

Besedka is configured entirely via environment variables.

| Variable | Description | Default |
| :--- | :--- | :--- |
| `AUTH_SECRET` | **(Required)** Secret key used for encrypting data and signing tokens. | |
| `BESEDKA_DB` | Path to the bbolt database file. | `besedka.db` |
| `API_ADDR` | Address for the main chat server to listen on. | `:443` (if TLS enabled), else `:8080` |
| `ADMIN_ADDR` | Address for the Admin UI to listen on. | `localhost:8081` |
| `BASE_URL` | The public base URL of the application. | `http://localhost:8080` |
| `UPLOADS_PATH` | Directory where uploaded files and avatars are stored. | `uploads` |
| `ADMIN_USER` | Username for the Admin UI. | `admin` |
| `ADMIN_PASSWORD` | Password for the Admin UI. | `1337chat` |
| `TOKEN_EXPIRY` | Duration for which authentication tokens remain valid. | `24h` |
| `MAX_IMAGE_SIZE` | Maximum size for image uploads in bytes. | 10MB (`10485760`) |
| `MAX_AVATAR_SIZE` | Maximum size for avatar uploads in bytes. | 5MB (`5242880`) |
| `MAX_FILE_SIZE` | Maximum size for general file uploads in bytes. | 25MB (`26214400`) |
| `TLS_CERT` | Path to a custom TLS certificate file. | |
| `TLS_KEY` | Path to a custom TLS private key file. | |
| `TLS_AUTO_CERT_PATH` | Directory to cache Let's Encrypt certificates. Enables automatic Let's Encrypt integration. | |
| `ENABLE_HTTP_CHALLENGE` | Set to `true` to enable an HTTP-01 challenge server for Let's Encrypt. | `false` |
| `HTTP_CHALLENGE_PORT` | Port for the HTTP-01 challenge server to listen on. | `80` |

## Encryption

Besedka supports at-rest encryption for the database and uploaded files. When `AUTH_SECRET` is provided, all sensitive data (users, messages, tokens, files) will be encrypted.

## Future Roadmap

- [x] Realtime updates for user presence/creation/deletion
- [x] User profile/settings
- [x] Password reset
- [x] User deletion
- [x] Admin UI
- [x] File attachments and media support
- [x] Infinite scrolling
- [x] Markdown formatting
- [ ] Emojis and reactions
- [x] Notifications
- [ ] Messages backup
- [x] TLS support
- [x] LetsEncrypt integration