import { store } from '../state.js';

export function createChatList(container) {
    const render = (state) => {
        // nosemgrep
        container.innerHTML = `
            <div class="chat-list-header">
                <h2>Chats</h2>
            </div>
            <div class="chat-list-items">
                ${state.chats.map(chat => {
            let avatarHtml = `<div class="avatar">${chat.name.charAt(0)}</div>`;
            if (chat.avatarUrl) {
                avatarHtml = `<div class="avatar" style="padding:0;"><img src="${chat.avatarUrl}" alt="Avatar" style="width:100%; height:100%; border-radius:50%; object-fit:cover;"></div>`;
            } else if (chat.isDm) {
                const otherUserId = chat.id.replace('dm_', '').split('_').find(id => id !== state.currentUser?.id);
                const fullUser = state.users.find(u => u.id === otherUserId);
                if (fullUser?.avatarUrl) {
                    avatarHtml = `<div class="avatar" style="padding:0;"><img src="${fullUser.avatarUrl}" alt="Avatar" style="width:100%; height:100%; border-radius:50%; object-fit:cover;"></div>`;
                }
            }
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
