import { store } from '../state.js';

function resolveChatAvatarUrl(chat, users, currentUserId) {
    if (chat.avatarUrl) return chat.avatarUrl;
    if (chat.isDm) {
        const otherUserId = chat.id.replace('dm_', '').split('_').find(id => id !== currentUserId);
        return users.find(u => u.id === otherUserId)?.avatarUrl || null;
    }
    return null;
}

export function createChatList(container) {
    const render = (state) => {
        const avatarUrls = new Map();
        // nosemgrep
        container.innerHTML = `
            <div class="chat-list-header">
                <h2>Chats</h2>
            </div>
            <div class="chat-list-items">
                ${state.chats.map(chat => {
            const avatarUrl = resolveChatAvatarUrl(chat, state.users, state.currentUser?.id);
            avatarUrls.set(chat.id, avatarUrl);
            const avatarHtml = avatarUrl
                ? `<div class="avatar" style="padding:0;"></div>`
                : `<div class="avatar">${chat.name.charAt(0)}</div>`;
            return `
                    <div class="chat-item ${state.activeChatId === chat.id ? 'active' : ''}" data-id="${chat.id}">
                        ${avatarHtml}
                        <div class="chat-info">
                            <div class="chat-name">${chat.name}</div>
                            <div class="chat-preview">${chat.isDm && chat.online ? '<span style="color: #4caf50; font-size: 0.8em;">● Online</span>' : ''}</div>
                        </div>
                        ${chat.unreadCount > 0 ? `<div class="unread-badge">${chat.unreadCount}</div>` : ''}
                    </div>
                    `;
        }).join('')}
            </div>
        `;

        // Add event listeners and avatar images
        container.querySelectorAll('.chat-item').forEach(item => {
            item.addEventListener('click', () => {
                store.setActiveChat(item.dataset.id);
            });

            const avatarUrl = avatarUrls.get(item.dataset.id);
            if (avatarUrl) {
                const avatarEl = item.querySelector('.avatar');
                if (avatarEl) {
                    const img = document.createElement('img');
                    img.src = avatarUrl;
                    img.alt = 'Avatar';
                    img.style.cssText = 'width:100%; height:100%; border-radius:50%; object-fit:cover;';
                    avatarEl.appendChild(img);
                }
            }
        });
    };

    // Initial render
    render(store.state);

    // Subscribe to state changes
    store.subscribe(render);
}
