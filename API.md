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
    "tokenExpiry": 1234567890,
    "needRegister": false
  }
  ```
- **First Login (401 Unauthorized):** Body contains `needRegister: true`
- **Invalid Credentials (401 Unauthorized):** Body contains `needRegister: false`

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

### Register Info
**Endpoint:** `GET /api/register-info`

**Description:** Gets registration information for a user with a setup token.

**Query Parameters:**
- `token`: The setup token for the user.

**Response:**
- **Success (200 OK):**
  ```json
  {
    "username": "string",
    "displayName": "string",
    "totpSecret": "BASE32_SECRET_STRING"
  }
  ```
- **Error (404 Not Found):** If token is invalid or expired.

### Logoff
**Endpoint:** `POST /api/logoff`

**Description:** Invalidates the JWT on the server and unsets the cookie.

**Response:**
- **Success (200 OK)**

### Reset Password
**Endpoint:** `POST /api/reset-password`

**Description:** Resets the password for the currently authenticated user. Invalidates all tokens, generates a new TOTP secret, and returns a new setup link. The user status is changed back to "created".

**Response:**
- **Success (200 OK):**
  ```json
  {
    "success": true,
    "setupLink": "string" // URL to share with user to complete registration again
  }
  ```

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

## Files

### Upload Image
**Endpoint:** `POST /api/upload/image`

**Description:** Uploads an image file. Supports JPEG, PNG, GIF, WebP. Limit 10MB.

**Headers:**
- `Content-Type`: `image/*` or `application/octet-stream` (body is raw binary)

**Response:**
```json
{
  "id": "uuid_string"
}
```

### Get Image
**Endpoint:** `GET /api/images/{id}`

**Description:** Downloads an image by its UUID.

**Response:**
- **Success (200 OK):** Binary image content with appropriate `Content-Type` and `Content-Length`.
- **Not Found (404):** If ID doesn't exist.

## Admin API

The Admin API runs on a separate port (default 8081) and is used for management tasks.

### List Users
**Endpoint:** `GET /api/users`

**Description:** Returns all users in the system with their full details.

**Response:**
```json
[
  {
    "id": "string",
    "userName": "string",
    "displayName": "string",
    "avatarUrl": "string",
    "presence": {
      "online": boolean,
      "lastSeen": "unix_timestamp"
    },
    "status": "string"
  }
]
```

### Add User
**Endpoint:** `POST /admin/users`

**Request Body:**
```json
{
  "username": "string",
  "displayName": "string" // Optional
}
```

**Response:**
```json
{
  "success": true,
  "username": "string",
  "setupLink": "string" // URL to share with user to complete registration
}
```

### Delete User
**Endpoint:** `DELETE /admin/users`

**Query Parameters:**
- `id`: The user ID of the user to delete.

**Response:**
```json
{
  "success": true,
  "message": "User <id> deleted"
}
```

### Reset User Password
**Endpoint:** `POST /api/users/reset-password`

**Description:** Admin endpoint to reset a user's password. Removes all their tokens, generates a new TOTP secret, sets their status to "created", and returns a registration link.

**Query Parameters:**
- `id`: The user ID of the user to reset.

**Response:**
```json
{
  "success": true,
  "setupLink": "string",
  "message": "Password for user <id> reset successfully"
}
```

