import { store } from '../state.js';

export function createChatWindow(container) {
    const SVG_NS = 'http://www.w3.org/2000/svg';
    let lastChatId = null;
    let filesToAttach = [];
    let isUploading = false;
    let uploadAbortController = null;
    let firstRenderedSeq = 0;
    let lastRenderedSeq = 0;
    let forceUsersRefresh = false;

    const createSvgElement = (name, attrs = {}) => {
        const el = document.createElementNS(SVG_NS, name);
        Object.entries(attrs).forEach(([key, value]) => el.setAttribute(key, value));
        return el;
    };

    const createAttachmentSpinner = () => {
        const svg = createSvgElement('svg', { class: 'spinner', viewBox: '25 25 50 50' });
        svg.appendChild(createSvgElement('circle', { cx: '50', cy: '50', r: '20' }));
        return svg;
    };

    const createFileIcon = () => {
        const svg = createSvgElement('svg', {
            width: '24',
            height: '24',
            viewBox: '0 0 24 24',
            fill: 'none',
            stroke: 'currentColor',
            'stroke-width': '2',
            'stroke-linecap': 'round',
            'stroke-linejoin': 'round'
        });
        svg.appendChild(createSvgElement('path', { d: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z' }));
        svg.appendChild(createSvgElement('polyline', { points: '14 2 14 8 20 8' }));
        svg.appendChild(createSvgElement('line', { x1: '16', y1: '13', x2: '8', y2: '13' }));
        svg.appendChild(createSvgElement('line', { x1: '16', y1: '17', x2: '8', y2: '17' }));
        svg.appendChild(createSvgElement('polyline', { points: '10 9 9 9 8 9' }));
        return svg;
    };

    // Overlay Elements (created once)
    let overlay = document.getElementById('image-overlay');
    if (!overlay) {
        overlay = document.createElement('div');
        overlay.id = 'image-overlay';
        overlay.className = 'image-overlay';
        overlay.innerHTML = `
            <div class="overlay-controls" id="overlay-controls">
                <div class="overlay-info">
                    <span class="overlay-sender" id="overlay-sender"></span>
                    <span class="overlay-time" id="overlay-time"></span>
                </div>
                <div class="overlay-actions">
                    <button class="overlay-btn" id="overlay-download" title="Download">
                        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                            <polyline points="7 10 12 15 17 10"></polyline>
                            <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                    </button>
                    <button class="overlay-btn" id="overlay-close" title="Close">
                        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <line x1="18" y1="6" x2="6" y2="18"></line>
                            <line x1="6" y1="6" x2="18" y2="18"></line>
                        </svg>
                    </button>
                </div>
            </div>
            <img id="overlay-image" src="" alt="Full view">
        `;
        document.body.appendChild(overlay);

        const closeOverlay = () => {
            overlay.classList.remove('active');
            overlay.querySelector('#overlay-image').src = '';
        };

        overlay.querySelector('#overlay-close').addEventListener('click', closeOverlay);
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) closeOverlay();
        });

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && overlay.classList.contains('active')) {
                closeOverlay();
            }
        });

        let fadeTimer;
        const resetFade = () => {
            const controls = overlay.querySelector('#overlay-controls');
            controls.classList.remove('faded');
            clearTimeout(fadeTimer);
            fadeTimer = setTimeout(() => {
                controls.classList.add('faded');
            }, 3000);
        };
        overlay.addEventListener('mousemove', resetFade);
        overlay.addEventListener('click', resetFade);
    }

    const handleEscape = (e) => {
        if (e.key === 'Escape' && isUploading) {
            if (uploadAbortController) {
                uploadAbortController.abort();
                uploadAbortController = null;
            }
            isUploading = false;
            updateUI(store.state);
        }
    };
    document.addEventListener('keydown', handleEscape);

    // Initial Shell Structure
    container.innerHTML = `
        <div class="chat-header">
            <div class="chat-header-left" style="display: flex; align-items: center;">
                <div class="avatar" style="width:32px; height:32px; font-size: 14px;"></div>
                <h3 style="margin-left: 10px;"></h3>
            </div>
            <div class="actions"></div>
        </div>
        <div class="messages-container" id="messages-container">
            <div class="history-loading" style="display:none">
                Loading<span class="loading-dots"></span>
            </div>
        </div>
        <div class="input-area">
            <input type="file" id="file-input" style="display:none">
            <button class="attach-btn" id="attach-btn">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"></path>
                </svg>
            </button>
            <div id="attachments-indicator-container"></div>
            <textarea class="message-input" placeholder="Type a message..." id="message-input" rows="1"></textarea>
            <button class="send-btn" id="send-btn">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="22" y1="2" x2="11" y2="13"></line>
                    <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
                </svg>
            </button>
        </div>
        <div id="empty-state" style="display: none; align-items: center; justify-content: center; height: 100%; color: var(--text-secondary); position: absolute; top: 0; left: 0; width: 100%; background: var(--bg-app); z-index: 5;">
            Select a chat to start messaging
        </div>
    `;

    const elements = {
        headerAvatar: container.querySelector('.chat-header .avatar'),
        headerTitle: container.querySelector('.chat-header h3'),
        messagesContainer: container.querySelector('#messages-container'),
        historyLoading: container.querySelector('.history-loading'),
        input: container.querySelector('#message-input'),
        sendBtn: container.querySelector('#send-btn'),
        attachBtn: container.querySelector('#attach-btn'),
        fileInput: container.querySelector('#file-input'),
        attachmentsContainer: container.querySelector('#attachments-indicator-container'),
        emptyState: container.querySelector('#empty-state')
    };

    let scrollState = {
        wasAtBottom: true,
        prevScrollHeight: 0,
        prevScrollTop: 0
    };

    const createMessageElement = (msg, state) => {
        const isMe = msg.sender === 'me';
        let senderDisplayName = 'me';
        if (!isMe) {
            const user = state.users.find(u => u.id === msg.userId);
            senderDisplayName = user ? user.displayName : msg.userId;
        }

        const div = document.createElement('div');
        div.className = 'message-line';
        div.setAttribute('data-seq', msg.seq);

        const attachmentsFragment = document.createDocumentFragment();
        if (msg.attachments && msg.attachments.length > 0) {
            msg.attachments.forEach(att => {
                if (att.type === 'image') {
                    if (!att.fileId) return;
                    const imageUrl = `/api/images/${encodeURIComponent(att.fileId)}`;
                    const imageWrap = document.createElement('div');
                    imageWrap.className = 'message-attachment';
                    imageWrap.dataset.src = imageUrl;
                    imageWrap.dataset.sender = isMe ? 'You' : senderDisplayName;
                    imageWrap.dataset.time = msg.timestamp;

                    const placeholder = document.createElement('div');
                    placeholder.className = 'attachment-placeholder';
                    placeholder.appendChild(createAttachmentSpinner());
                    imageWrap.appendChild(placeholder);

                    const img = document.createElement('img');
                    img.src = imageUrl;
                    img.alt = att.name || '';
                    img.loading = 'lazy';
                    img.addEventListener('load', () => {
                        imageWrap.classList.add('loaded');
                    });
                    imageWrap.appendChild(img);
                    attachmentsFragment.appendChild(imageWrap);
                } else if (att.type === 'file') {
                    if (!att.fileId) return;
                    const fileWrap = document.createElement('div');
                    fileWrap.className = 'message-attachment-file';
                    fileWrap.dataset.fileId = att.fileId;
                    fileWrap.dataset.name = att.name || '';
                    fileWrap.dataset.mime = att.mimeType || '';

                    const icon = document.createElement('div');
                    icon.className = 'file-icon';
                    icon.appendChild(createFileIcon());
                    fileWrap.appendChild(icon);

                    const fileInfo = document.createElement('div');
                    fileInfo.className = 'file-info';
                    const fileName = document.createElement('span');
                    fileName.className = 'file-name';
                    fileName.title = att.name || '';
                    fileName.textContent = att.name || '';
                    fileInfo.appendChild(fileName);
                    fileWrap.appendChild(fileInfo);

                    attachmentsFragment.appendChild(fileWrap);
                }
            });
        }

        const timeSpan = document.createElement('span');
        timeSpan.className = 'message-time';
        timeSpan.textContent = `[${msg.timestamp}]`;
        div.appendChild(timeSpan);

        const senderSpan = document.createElement('span');
        senderSpan.className = `message-sender ${isMe ? 'is-me' : ''}`;
        senderSpan.textContent = `<${senderDisplayName}>`;
        div.appendChild(senderSpan);

        const contentSpan = document.createElement('span');
        contentSpan.className = 'message-content';
        contentSpan.innerHTML = msg.text;
        div.appendChild(contentSpan);

        div.appendChild(attachmentsFragment);

        // Handle Image Clicks
        div.querySelectorAll('.message-attachment').forEach(el => {
            el.addEventListener('click', () => {
                const src = el.dataset.src;
                const sender = el.dataset.sender;
                const time = el.dataset.time;

                const img = overlay.querySelector('#overlay-image');
                img.src = src;
                overlay.querySelector('#overlay-sender').textContent = sender;
                overlay.querySelector('#overlay-time').textContent = time;

                const dBtn = overlay.querySelector('#overlay-download');
                dBtn.onclick = (e) => {
                    e.stopPropagation();
                    const a = document.createElement('a');
                    a.href = src;
                    a.download = `image-${Date.now()}.png`;
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                };

                overlay.classList.add('active');
            });
        });

        // Handle File Clicks
        div.querySelectorAll('.message-attachment-file').forEach(el => {
            el.addEventListener('click', () => {
                const fileId = el.dataset.fileId;
                if (!fileId) return;
                const name = el.dataset.name;
                let menu = document.getElementById('file-download-menu');
                if (!menu) {
                    menu = document.createElement('div');
                    menu.id = 'file-download-menu';
                    menu.className = 'file-download-menu';
                    document.body.appendChild(menu);
                    document.addEventListener('click', (evt) => {
                        if (!menu.contains(evt.target) && !evt.target.closest('.message-attachment-file')) {
                            menu.style.display = 'none';
                        }
                    });
                }
                menu.replaceChildren();
                const downloadLink = document.createElement('a');
                downloadLink.href = `/api/files/${fileId}?name=${encodeURIComponent(name || fileId)}`;
                downloadLink.download = name || '';
                downloadLink.textContent = `Download ${name || ''}`;
                menu.appendChild(downloadLink);
                const rect = el.getBoundingClientRect();
                menu.style.display = 'block';
                menu.style.left = `${rect.left + window.scrollX}px`;
                menu.style.top = `${rect.bottom + window.scrollY + 5}px`;
            });
        });

        // Add copy buttons to code blocks
        div.querySelectorAll('.message-content pre').forEach(pre => {
            const btn = document.createElement('button');
            btn.className = 'copy-code-btn';
            btn.title = 'Copy code';
            btn.setAttribute('aria-label', 'Copy code');
            btn.innerHTML = `
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                </svg>
            `;
            btn.onclick = async (e) => {
                e.stopPropagation();
                const code = pre.querySelector('code');
                const text = code ? code.innerText : pre.innerText;
                try {
                    await navigator.clipboard.writeText(text);
                    btn.classList.add('copied');
                    btn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';
                    setTimeout(() => {
                        btn.classList.remove('copied');
                        btn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
                    }, 2000);
                } catch (err) { console.error('Failed to copy: ', err); }
            };
            pre.appendChild(btn);
        });

        // Handle image loads for scrolling
        div.querySelectorAll('img').forEach(img => {
            img.addEventListener('load', () => {
                if (scrollState.wasAtBottom) {
                    elements.messagesContainer.scrollTop = elements.messagesContainer.scrollHeight;
                }
            });
        });

        return div;
    };

    const updateUI = (state) => {
        const usersChanged = forceUsersRefresh;
        forceUsersRefresh = false;
        const activeChat = state.chats.find(c => c.id === state.activeChatId);
        if (!activeChat) {
            elements.emptyState.style.display = 'flex';
            return;
        }
        elements.emptyState.style.display = 'none';

        const chatChanged = state.activeChatId !== lastChatId;
        const messages = state.messages[state.activeChatId] || [];
        const isLoading = state.isLoadingHistory?.[state.activeChatId] || false;

        // 1. Handle Chat Switching
        if (chatChanged) {
            lastChatId = state.activeChatId;
            firstRenderedSeq = 0;
            lastRenderedSeq = 0;
            elements.messagesContainer.querySelectorAll('.message-line').forEach(el => el.remove());
            elements.headerTitle.textContent = activeChat.name;
            
            let headerAvatarUrl = activeChat.avatarUrl || null;
            if (!headerAvatarUrl && activeChat.isDm) {
                const otherUserId = activeChat.id.replace('dm_', '').split('_').find(id => id !== state.currentUser?.id);
                const fullUser = state.users.find(u => u.id === otherUserId);
                headerAvatarUrl = fullUser?.avatarUrl || null;
            }
            
            elements.headerAvatar.innerHTML = '';
            if (headerAvatarUrl) {
                elements.headerAvatar.style.padding = '0';
                const img = document.createElement('img');
                img.src = headerAvatarUrl;
                img.style.cssText = 'width:100%; height:100%; border-radius:50%; object-fit:cover;';
                elements.headerAvatar.appendChild(img);
            } else {
                elements.headerAvatar.style.padding = '';
                elements.headerAvatar.textContent = activeChat.name.charAt(0);
            }
            
            elements.input.value = '';
            elements.input.style.height = '40px';
            scrollState.wasAtBottom = true;
        } else if (usersChanged) {
            firstRenderedSeq = 0;
            lastRenderedSeq = 0;
            elements.messagesContainer.querySelectorAll('.message-line').forEach(el => el.remove());
            elements.headerTitle.textContent = activeChat.name;

            let headerAvatarUrl = activeChat.avatarUrl || null;
            if (!headerAvatarUrl && activeChat.isDm) {
                const otherUserId = activeChat.id.replace('dm_', '').split('_').find(id => id !== state.currentUser?.id);
                const fullUser = state.users.find(u => u.id === otherUserId);
                headerAvatarUrl = fullUser?.avatarUrl || null;
            }

            elements.headerAvatar.innerHTML = '';
            if (headerAvatarUrl) {
                elements.headerAvatar.style.padding = '0';
                const img = document.createElement('img');
                img.src = headerAvatarUrl;
                img.style.cssText = 'width:100%; height:100%; border-radius:50%; object-fit:cover;';
                elements.headerAvatar.appendChild(img);
            } else {
                elements.headerAvatar.style.padding = '';
                elements.headerAvatar.textContent = activeChat.name.charAt(0);
            }
        }

        // 2. Update Loading State
        elements.historyLoading.style.display = isLoading ? 'flex' : 'none';

        // 3. Surgical Message Updates
        if (messages.length > 0) {
            // Initial render
            if (firstRenderedSeq === 0) {
                const fragment = document.createDocumentFragment();
                messages.forEach(msg => fragment.appendChild(createMessageElement(msg, state)));
                elements.messagesContainer.appendChild(fragment);
                firstRenderedSeq = messages[0].seq;
                lastRenderedSeq = messages[messages.length - 1].seq;
                if (scrollState.wasAtBottom) {
                    elements.messagesContainer.scrollTop = elements.messagesContainer.scrollHeight;
                }
            } else {
                // History prepend
                const historyMessages = messages.filter(m => m.seq < firstRenderedSeq);
                if (historyMessages.length > 0) {
                    const fragment = document.createDocumentFragment();
                    historyMessages.forEach(msg => fragment.appendChild(createMessageElement(msg, state)));
                    
                    const firstMsg = elements.messagesContainer.querySelector('.message-line');
                    const oldHeight = elements.messagesContainer.scrollHeight;
                    const oldScrollTop = elements.messagesContainer.scrollTop;
                    
                    if (firstMsg) {
                        elements.messagesContainer.insertBefore(fragment, firstMsg);
                    } else {
                        elements.messagesContainer.appendChild(fragment);
                    }
                    
                    const newHeight = elements.messagesContainer.scrollHeight;
                    elements.messagesContainer.scrollTop = oldScrollTop + (newHeight - oldHeight);
                    firstRenderedSeq = messages[0].seq;
                }

                // New message append
                const newMessages = messages.filter(m => m.seq > lastRenderedSeq);
                if (newMessages.length > 0) {
                    const fragment = document.createDocumentFragment();
                    newMessages.forEach(msg => fragment.appendChild(createMessageElement(msg, state)));
                    elements.messagesContainer.appendChild(fragment);
                    lastRenderedSeq = messages[messages.length - 1].seq;
                    if (scrollState.wasAtBottom) {
                        elements.messagesContainer.scrollTop = elements.messagesContainer.scrollHeight;
                    }
                }
            }
        }

        // 4. Update Attachments & Upload State
        elements.attachBtn.disabled = isUploading;
        elements.sendBtn.disabled = isUploading;
        
        if (isUploading) {
            elements.attachBtn.innerHTML = `<svg class="spinner" width="20" height="20" viewBox="0 0 50 50"><circle cx="25" cy="25" r="20"></circle></svg>`;
        } else {
            elements.attachBtn.innerHTML = `<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"></path></svg>`;
        }

        elements.attachmentsContainer.innerHTML = filesToAttach.length > 0 ? `
            <div class="attach-indicator">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>
                ${filesToAttach.length} attached
            </div>` : '';
    };

    // Event Listeners
    let scrollThrottleTimer = null;
    elements.messagesContainer.addEventListener('scroll', () => {
        const c = elements.messagesContainer;
        scrollState.wasAtBottom = (c.scrollHeight - c.scrollTop - c.clientHeight) < 50;
        
        if (scrollThrottleTimer) return;
        scrollThrottleTimer = setTimeout(() => {
            scrollThrottleTimer = null;
            if (c.scrollTop <= 100 && !store.state.isLoadingHistory?.[lastChatId]) {
                const msgs = store.state.messages[lastChatId] || [];
                const minSeq = msgs.length > 0 ? msgs[0].seq : 0;
                if (minSeq > 1) {
                    store.fetchMessages(lastChatId, minSeq - 100, minSeq - 1);
                }
            }
        }, 200);
    });

    const handleSend = () => {
        const text = elements.input.value.trim();
        if (text || filesToAttach.length > 0) {
            scrollState.wasAtBottom = true;
            store.sendMessage(lastChatId, text, filesToAttach);
            elements.input.value = '';
            elements.input.style.height = '40px';
            filesToAttach = [];
            updateUI(store.state);
        }
    };

    elements.sendBtn.addEventListener('click', handleSend);
    elements.input.addEventListener('input', () => {
        elements.input.style.height = 'auto';
        elements.input.style.height = `${Math.max(40, elements.input.scrollHeight)}px`;
    });
    elements.input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSend();
        }
    });

    elements.attachBtn.addEventListener('click', () => elements.fileInput.click());
    elements.fileInput.addEventListener('change', async (e) => {
        if (e.target.files.length > 0) {
            const file = e.target.files[0];
            isUploading = true;
            uploadAbortController = new AbortController();
            updateUI(store.state);
            try {
                const isImage = file.type.startsWith('image/');
                const result = isImage 
                    ? await store.uploadImage(file, uploadAbortController.signal)
                    : await store.uploadFile(file, uploadAbortController.signal);
                if (!uploadAbortController.signal.aborted) {
                    filesToAttach.push({ type: isImage ? 'image' : 'file', name: file.name, mimeType: file.type || 'application/octet-stream', fileId: result.id });
                }
            } catch (err) {
                if (!uploadAbortController?.signal.aborted) alert(`Failed to upload ${file.name}: ${err.message}`);
            } finally {
                uploadAbortController = null;
                isUploading = false;
                elements.fileInput.value = '';
                updateUI(store.state);
            }
        }
    });

    // Initial Render
    updateUI(store.state);

    // Subscription with Diffing
    const getUsersHash = (state) => {
        return state.users.map(u => `${u.id}:${u.displayName || ''}:${u.avatarUrl || ''}`).join('|');
    };

    let lastProcessedState = {
        chatId: store.state.activeChatId,
        msgCount: (store.state.messages[store.state.activeChatId] || []).length,
        loading: store.state.isLoadingHistory?.[store.state.activeChatId],
        scrollSignal: store.state.forceScrollSignal,
        usersHash: getUsersHash(store.state)
    };

    store.subscribe((state) => {
        const currentMsgs = state.messages[state.activeChatId] || [];
        const currentLoading = state.isLoadingHistory?.[state.activeChatId];
        const currentUsersHash = getUsersHash(state);
        const usersChanged = currentUsersHash !== lastProcessedState.usersHash;
        
        const hasRelevantChange = 
            state.activeChatId !== lastProcessedState.chatId ||
            currentMsgs.length !== lastProcessedState.msgCount ||
            currentLoading !== lastProcessedState.loading ||
            state.forceScrollSignal !== lastProcessedState.scrollSignal ||
            usersChanged;

        if (hasRelevantChange) {
            if (state.forceScrollSignal !== lastProcessedState.scrollSignal) {
                scrollState.wasAtBottom = true;
            }
            if (usersChanged) {
                forceUsersRefresh = true;
            }

            lastProcessedState = {
                chatId: state.activeChatId,
                msgCount: currentMsgs.length,
                loading: currentLoading,
                scrollSignal: state.forceScrollSignal,
                usersHash: currentUsersHash
            };
            
            updateUI(state);
        }
    });
}
