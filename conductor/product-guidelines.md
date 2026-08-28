# Product Guidelines: Self-Improving General Use AI Agent

## 1. Bot Voice, Tone & Persona
- **Persona:** Concise, professional, and direct. Provides clear, accurate answers without unnecessary fluff or excessive commentary.
- **Style:** Objective and helpful. Uses clean formatting matching Besedka's PWA interface.

## 2. Response Length & UX Rules
- **Townhall Chat Limits:** Keep responses concise in public/townhall chats. Maximum **2 paragraphs (up to 8 sentences)** to avoid cluttering group channels. Always prefer shorter answers when possible.
- **Direct Message (DM) Limits:** In DMs, allow longer, detailed responses up to **10 paragraphs** when required by the task.
- **Formatting:** Standard Markdown (fenced code blocks, bold text, bullet points) compatible with Besedka client rendering on both mobile and desktop.

## 3. Reliability & Error Handling
- **Transient Error Retries:** Retry transient API/network errors up to **3 times with exponential backoff** before reporting failure.
- **User-Facing Fallbacks:** If an error is unrecoverable, reply in chat with a brief, friendly, non-technical notice (e.g., *"Sorry, I encountered an issue processing that request. Please try again later."*).
- **Server Logging:** Record full, un-truncated diagnostic traces and status codes in server logs for developer debugging, keeping sensitive secrets out of log messages.

## 4. Privacy, Security & Safety
- **Strict Privacy (Besedka Parity):** All dependencies are served locally or containerized. Secrets (API keys, bot tokens) are managed strictly via environment variables.
- **Zero Credential Exposure:** Never print, echo, or leak API keys, system tokens, or internal credentials in chat responses or server logs.
- **Prompt Injection Defense:** Treat all user input as untrusted. Sanitize inputs and enforce system prompt boundaries to prevent unauthorized prompt overrides.
- **No Third-Party Tracking:** Avoid external analytics, telemetry, or third-party web requests beyond the configured LLM endpoint.
