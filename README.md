# Besedka

[![Workflow Pipeline](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml/badge.svg)](https://github.com/C-Pro/besedka/actions/workflows/pipeline.yml)

Besedka is a self-hosted chat application for a limited number of users (e.g. for a family or a small group of friends) built with Go and vanilla web technologies.

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

## Encryption & Migration

Besedka supports at-rest encryption for the database and uploaded files. When `AUTH_SECRET` is provided, all sensitive data (users, messages, tokens, files) will be encrypted.

### Upgrading to Encryption

If you are upgrading from an older version of Besedka that did not have encryption enabled, you **must** migrate your data before the server can start with an `AUTH_SECRET`.

1. Ensure your configuration (`AUTH_SECRET`, `BESEDKA_DB`, `UPLOADS_PATH`) is set.
2. Run the migration tool:
   ```bash
   go run cmd/migrate/main.go
   ```

The migration tool will:
- Create a backup of your database (`besedka.db.[TIMESTAMP].tar.gz`).
- Create a backup of your uploads directory (`uploads.[TIMESTAMP].tar.gz`).
- Encrypt all existing data and files.
- Generate a unique encryption salt and store it in the database.

Once migrated, you can start the server normally.

**Note:** If you start with a fresh (empty) database and an `AUTH_SECRET`, encryption will be enabled and initialized automatically.

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
- [ ] TLS support
- [ ] LetsEncrypt integration