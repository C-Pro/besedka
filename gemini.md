# Besedka

Besedka is a simple self-hosted multiuser chat application for small groups. It is aimed to people who want to have their own chat not controlled by any third parties.

## Design limitations

Due to the "small groups" nature we do not aim to support large number of users,
or large number of messages. We do not plan to support self-registration of the new users.
All users are created manually by the admin.
At this time there is no plans to support arbitary group chats: each besedka installation
has exactly one "Town hall" group chat containing all the users.
There is no user search, you see all registered users right away in the sidebar and can chat them individually right away.

## Core Non-Functional Requirements

### Easy installation and maintenance
Chat server is a single self contained binary that can be run as is or as a docker image.

### Privacy and security
Project is very strict to introducing new dependencies, we want to minimise supply chain attack surface. No npm even at build time (vetting npm deps is too taxing for a small project like this). No third party trackers or runtime dependencies. App should have all its dependencies served locally, no network requests to other domains then the one the chat is hosted on.
We don't offer e2e encryption at this time but we are doing our best to provide industry standard security.

### Essential features
While we do not try to be most featurefull chat application, we still want to provide all the necessities.

### Simple API design
The chat API should be simple for developers to use to facilitate 3rd party clients/UIs development.


## Coding style

Use comments only when necessary. Prefer self-documenting code.
Avoid leaving chain of thoughts style comments in the code.
When defining HTTP handlers always use Go 1.22+ style: `mux.HandleFunc("POST /admin/users", h.AddUserHandler)` instead of doing switch on `r.Method` inside the handler.


# Rules

When implementing new feature, always make sure to cover it with tests.
Always test the new features manually in the browser.
Make sure the server is running and the app is accessible at `http://localhost:8080`.
To run the server use `AUTH_SECRET=very-secure-secret-key-for-development-mode go run .`
To generate a TOTP key use `go run ./cmd/totp/main.go <base32 secret>`.