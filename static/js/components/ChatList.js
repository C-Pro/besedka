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
    // 1. Initial Shell
    container.innerHTML = `
        <div class="chat-list-header">
            <h2>Chats</h2>
        </div>
        <div class="chat-list-items" id="chat-list-items"></div>
    `;
    const itemsContainer = container.querySelector('#chat-list-items');
    const chatElements = new Map(); // chatId -> HTMLElement

    const createChatItem = (chat, state) => {
        const avatarUrl = resolveChatAvatarUrl(chat, state.users, state.currentUser?.id);
        const div = document.createElement('div');
        div.className = `chat-item ${state.activeChatId === chat.id ? 'active' : ''}`;
        div.setAttribute('data-id', chat.id);

        const avatarHtml = avatarUrl
            ? `<div class="avatar" style="padding:0;"><img src="${avatarUrl}" alt="Avatar" style="width:100%; height:100%; border-radius:50%; object-fit:cover;"></div>`
            : `<div class="avatar">${chat.name.charAt(0)}</div>`;

        const unreadCount = state.unreadCounts ? (state.unreadCounts[chat.id] || 0) : 0;
        const onlineHtml = chat.isDm && chat.online ? '<span style="color: #4caf50; font-size: 0.8em;">● Online</span>' : '';

        div.innerHTML = `
            ${avatarHtml}
            <div class="chat-info">
                <div class="chat-name">${chat.name}</div>
                <div class="chat-preview">${onlineHtml}</div>
            </div>
            ${unreadCount > 0 ? `<div class="unread-badge">${unreadCount}</div>` : ''}
        `;

        div.addEventListener('click', () => {
            store.setActiveChat(chat.id);
        });

        return div;
    };

    const updateUI = (state) => {
        // Handle new or removed chats
        const currentChatIds = new Set(state.chats.map(c => c.id));
        
        // Remove old chats
        for (const [chatId, el] of chatElements.entries()) {
            if (!currentChatIds.has(chatId)) {
                el.remove();
                chatElements.delete(chatId);
            }
        }

        // Add or Update chats
        state.chats.forEach(chat => {
            let el = chatElements.get(chat.id);
            if (!el) {
                el = createChatItem(chat, state);
                itemsContainer.appendChild(el);
                chatElements.set(chat.id, el);
            } else {
                // Update existing item
                el.classList.toggle('active', state.activeChatId === chat.id);
                
                // Update unread badge
                const unreadCount = state.unreadCounts ? (state.unreadCounts[chat.id] || 0) : 0;
                let badge = el.querySelector('.unread-badge');
                if (unreadCount > 0) {
                    if (!badge) {
                        badge = document.createElement('div');
                        badge.className = 'unread-badge';
                        el.appendChild(badge);
                    }
                    badge.textContent = unreadCount;
                } else if (badge) {
                    badge.remove();
                }

                // Update online status
                const preview = el.querySelector('.chat-preview');
                const onlineHtml = chat.isDm && chat.online ? '<span style="color: #4caf50; font-size: 0.8em;">● Online</span>' : '';
                if (preview.innerHTML !== onlineHtml) {
                    preview.innerHTML = onlineHtml;
                }
            }
        });
    };

    // Initial render
    updateUI(store.state);

    // Subscription with Diffing
    let lastProcessedState = {
        activeChatId: store.state.activeChatId,
        chatListHash: '',
        unreadHash: ''
    };

    const getChatListHash = (state) => {
        return state.chats.map(c => `${c.id}:${c.online}`).join('|');
    };

    const getUnreadHash = (state) => {
        return JSON.stringify(state.unreadCounts || {});
    };

    store.subscribe((state) => {
        const currentChatListHash = getChatListHash(state);
        const currentUnreadHash = getUnreadHash(state);

        const hasRelevantChange = 
            state.activeChatId !== lastProcessedState.activeChatId ||
            currentChatListHash !== lastProcessedState.chatListHash ||
            currentUnreadHash !== lastProcessedState.unreadHash;

        if (hasRelevantChange) {
            lastProcessedState = {
                activeChatId: state.activeChatId,
                chatListHash: currentChatListHash,
                unreadHash: currentUnreadHash
            };
            updateUI(state);
        }
    });
}

