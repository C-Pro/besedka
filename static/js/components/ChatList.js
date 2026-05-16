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

    const renderAvatar = (avatarContainer, chat, state) => {
        const avatarUrl = resolveChatAvatarUrl(chat, state.users, state.currentUser?.id);
        avatarContainer.replaceChildren();
        if (avatarUrl) {
            avatarContainer.style.padding = '0';
            const img = document.createElement('img');
            img.src = avatarUrl;
            img.alt = 'Avatar';
            img.style.width = '100%';
            img.style.height = '100%';
            img.style.borderRadius = '50%';
            img.style.objectFit = 'cover';
            avatarContainer.appendChild(img);
        } else {
            avatarContainer.style.padding = '';
            avatarContainer.textContent = (chat.name || '?').charAt(0);
        }
    };

    const updateChatItem = (el, chat, state) => {
        el.classList.toggle('active', state.activeChatId === chat.id);

        const avatar = el.querySelector('.avatar');
        renderAvatar(avatar, chat, state);

        const nameEl = el.querySelector('.chat-name');
        nameEl.textContent = chat.name;

        const preview = el.querySelector('.chat-preview');
        preview.replaceChildren();
        if (chat.isDm && chat.online) {
            const online = document.createElement('span');
            online.style.color = '#4caf50';
            online.style.fontSize = '0.8em';
            online.textContent = '● Online';
            preview.appendChild(online);
        }

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
    };

    const createChatItem = (chat, state) => {
        const div = document.createElement('div');
        div.className = `chat-item ${state.activeChatId === chat.id ? 'active' : ''}`;
        div.setAttribute('data-id', chat.id);
        const avatar = document.createElement('div');
        avatar.className = 'avatar';
        const info = document.createElement('div');
        info.className = 'chat-info';
        const name = document.createElement('div');
        name.className = 'chat-name';
        const preview = document.createElement('div');
        preview.className = 'chat-preview';
        info.appendChild(name);
        info.appendChild(preview);
        div.appendChild(avatar);
        div.appendChild(info);

        div.addEventListener('click', () => {
            store.setActiveChat(chat.id);
        });

        updateChatItem(div, chat, state);
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
                updateChatItem(el, chat, state);
            }
        });
    };

    // Initial render
    updateUI(store.state);

    // Subscription with Diffing
    let lastProcessedState = {
        activeChatId: store.state.activeChatId,
        chatListHash: '',
        unreadHash: '',
        usersHash: store.state.users.map(u => `${u.id}:${u.avatarUrl || ''}`).join('|')
    };

    const getChatListHash = (state) => {
        return state.chats.map(c => `${c.id}:${c.online}`).join('|');
    };

    const getUnreadHash = (state) => {
        return JSON.stringify(state.unreadCounts || {});
    };

    const getUsersHash = (state) => {
        return state.users.map(u => `${u.id}:${u.avatarUrl || ''}`).join('|');
    };

    store.subscribe((state) => {
        const currentChatListHash = getChatListHash(state);
        const currentUnreadHash = getUnreadHash(state);
        const currentUsersHash = getUsersHash(state);

        const hasRelevantChange = 
            state.activeChatId !== lastProcessedState.activeChatId ||
            currentChatListHash !== lastProcessedState.chatListHash ||
            currentUnreadHash !== lastProcessedState.unreadHash ||
            currentUsersHash !== lastProcessedState.usersHash;

        if (hasRelevantChange) {
            lastProcessedState = {
                activeChatId: state.activeChatId,
                chatListHash: currentChatListHash,
                unreadHash: currentUnreadHash,
                usersHash: currentUsersHash
            };
            updateUI(state);
        }
    });
}
