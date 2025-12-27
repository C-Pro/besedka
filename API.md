# API Protocol

This document defines the client-server JSON-based protocol for the chat application.

## Authentication

### Login
**Endpoint:** `POST /api/login`

**Request Body:**
```json
{
  "username": "string",
  "password": "string",
  "totp": 123456 // Optional: required if user is fully registered
}
```

**Response:**
- **Success (200 OK):** Returns the JWT token in the body and sets it as a cookie.
  ```json
  {
    "token": "jwt_token_string",
    "tokenExpiry": 1234567890
  }
  ```
- **First Login (401 Unauthorized):** Indicates user needs to complete setup/registration.
  - Body text contains "First login matches".

### Register (Setup)
**Endpoint:** `POST /api/register`

**Request Body:**
```json
{
  "username": "string",
  "password": "old_password",
  "newPassword": "new_password"
}
```

**Response:**
- **Success (200 OK):** Returns the TOTP secret.
  ```json
  {
    "success": true,
    "totpSecret": "BASE32_SECRET_STRING"
  }
  ```

### Logoff
**Endpoint:** `POST /api/logoff`

**Description:** Invalidates the JWT on the server and unsets the cookie.

**Response:**
- **Success (200 OK)**

## Users & Chats

All endpoints below require a valid JWT token.

### Get Current User
**Endpoint:** `GET /api/me`

**Description:** Returns the currently authenticated user.

**Response:**
```json
{
  "id": "string",
  "name": "string"
}
```

### Get Users
**Endpoint:** `GET /api/users`

**Description:** Returns all users of the system.

**Response:**
```json
[
  {
    "id": "string",
    "displayName": "string",
    "avatarUrl": "string",
    "presence": {
      "online": boolean,
      "lastSeen": "unix_timestamp"
    }
  }
]
```

### Get Chats
**Endpoint:** `GET /api/chats`

**Description:** Returns a list of chats.
- **Townhall:** Fixed ID.
- **DM:** Derived from user IDs (e.g., hash of sorted user IDs).
- **Online Marker:** DM chats may include an online marker if the corresponding user is online.

**Response:**
```json
[
  {
    "id": "string",
    "name": "string",
    "unreadCount": number,
    "isDm": boolean,
    "online": boolean // Optional, for DMs
  }
]
```

## WebSocket Protocol

**Endpoint:** `GET /api/chat`

**Description:** Opens a WebSocket connection. Requires authentication.

### Client Messages

#### Join Chat
Subscribes user to a chat.
```json
{
  "type": "join",
  "chatId": "string"
}
```

#### Leave Chat
Unsubscribes user from a chat.
```json
{
  "type": "leave",
  "chatId": "string"
}
```

#### Send Message
Sends a message to a chat.
```json
{
  "type": "send",
  "chatId": "string",
  "content": "string"
}
```

### Server Messages

#### Presence Update
Sent when a user's presence changes.
```json
{
  "type": "presence",
  "userId": "string",
  "online": boolean
}
```

#### New Messages
Sent when new messages arrive in a subscribed chat.
```json
{
  "type": "messages",
  "chatId": "string",
  "messages": [
    {
      "timestamp": "unix_timestamp",
      "userId": "string",
      "content": "string"
    }
  ]
}
```
