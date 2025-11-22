# API Protocol

This document defines the client-server JSON-based protocol for the chat application.

## Authentication

### Login
**Endpoint:** `POST /login`

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response:**
- **Success (200 OK):** Returns the JWT token in the body and sets it as a cookie.
  ```json
  {
    "token": "jwt_token_string"
  }
  ```
  **Headers:**
  `Set-Cookie: token=jwt_token_string; HttpOnly; Path=/;`

### Logoff
**Endpoint:** `POST /logoff`

**Description:** Invalidates the JWT on the server and unsets the cookie.

**Response:**
- **Success (200 OK)**

## Users & Chats

All endpoints below require a valid JWT token.

### Get Users
**Endpoint:** `GET /users`

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
**Endpoint:** `GET /chats`

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

**Endpoint:** `GET /chat`

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
