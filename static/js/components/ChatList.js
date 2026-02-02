import { store } from '../state.js';
import { escapeHtml } from '../utils.js';

export function createChatList(container) {
    const render = (state) => {
        // nosemgrep
        container.innerHTML = `
            <div class="chat-list-header">
                <h2>Chats</h2>
            </div>
            <div class="chat-list-items">
                ${state.chats.map(chat => `
                    <div class="chat-item ${state.activeChatId === chat.id ? 'active' : ''}" data-id="${escapeHtml(chat.id)}">
                        <div class="avatar">${escapeHtml(chat.name).charAt(0)}</div>
                        <div class="chat-info">
                            <div class="chat-name">${escapeHtml(chat.name)}</div>
                            <div class="chat-preview">${chat.isDm && chat.online ? '<span style="color: #4caf50; font-size: 0.8em;">● Online</span>' : ''}</div>
                        </div>
                        ${chat.unreadCount > 0 ? `<div class="unread-badge">${chat.unreadCount}</div>` : ''}
                    </div>
                `).join('')}
            </div>
        `;

        // Add event listeners
        container.querySelectorAll('.chat-item').forEach(item => {
            item.addEventListener('click', () => {
                const id = item.dataset.id;
                store.setActiveChat(id);
            });
        });
    };

    // Initial render
    render(store.state);

    // Subscribe to state changes
    store.subscribe(render);
}
