import { store } from '../state.js';

export function createChatWindow(container) {
    const render = (state) => {
        const activeChat = state.chats.find(c => c.id === state.activeChatId);
        const messages = state.messages[state.activeChatId] || [];

        if (!activeChat) {
            container.innerHTML = `
                <div style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--text-secondary);">
                    Select a chat to start messaging
                </div>
            `;
            return;
        }

        container.innerHTML = `
            <div class="chat-header">
                <h3>${activeChat.name}</h3>
                <div class="actions">
                    <!-- Placeholder for actions -->
                </div>
            </div>
            <div class="messages-container" id="messages-container">
                ${messages.map(msg => {
            const isMe = msg.sender === 'me';
            let senderName = 'me';
            if (!isMe) {
                const user = state.users.find(u => u.id === msg.userId);
                senderName = user ? user.displayName : msg.userId;
            }

            return `
                    <div class="message-line">
                        <span class="message-time">[${msg.timestamp}]</span>
                        <span class="message-sender ${isMe ? 'is-me' : ''}">&lt;${senderName}&gt;</span>
                        <span class="message-content">${msg.text}</span>
                    </div>
                    `;
        }).join('')}
            </div>
            <div class="input-area">
                <input type="text" class="message-input" placeholder="Type a message..." id="message-input">
                <button class="send-btn" id="send-btn">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="22" y1="2" x2="11" y2="13"></line>
                        <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
                    </svg>
                </button>
            </div>
        `;

        // Scroll to bottom
        const messagesContainer = container.querySelector('#messages-container');
        if (messagesContainer) {
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
        }

        // Event listeners
        const input = container.querySelector('#message-input');
        const sendBtn = container.querySelector('#send-btn');

        const handleSend = () => {
            const text = input.value.trim();
            if (text) {
                store.sendMessage(state.activeChatId, text);
                input.value = '';
                input.focus();
            }
        };

        if (sendBtn) {
            sendBtn.addEventListener('click', handleSend);
        }

        if (input) {
            input.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') {
                    handleSend();
                }
            });
        }
    };

    // Initial render
    render(store.state);

    // Subscribe to state changes
    store.subscribe((state) => {
        // Optimization: Only re-render if active chat or messages changed
        // For now, just re-render everything for simplicity
        render(state);
    });
}
