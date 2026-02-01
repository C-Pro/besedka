import { store } from '../state.js';

export function createChatWindow(container) {
    let lastChatId = null;
    let filesToAttach = [];
    let isUploading = false;

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

        // Overlay Logic
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

        // Fade controls on inactivity
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

    const render = (state) => {
        // preserve input state
        const oldInput = container.querySelector('#message-input');
        let currentText = '';
        if (oldInput && lastChatId === state.activeChatId) {
            currentText = oldInput.value;
        }
        // Force reset input if we just sent a message (detected by logic elsewhere? No, simple re-render might not be enough)
        // With current primitive re-render, we lose focus and cursor.
        // Ideally we should virtual DOM this, but for now we re-render.

        lastChatId = state.activeChatId;

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

        const messagesHtml = messages.map(msg => {
            const isMe = msg.sender === 'me';
            let senderDisplayName = 'me';
            if (!isMe) {
                const user = state.users.find(u => u.id === msg.userId);
                senderDisplayName = user ? user.displayName : msg.userId;
            }

            let attachmentsHtml = '';
            if (msg.attachments && msg.attachments.length > 0) {
                attachmentsHtml = msg.attachments.map(att => {
                    if (att.type === 'image') {
                        return `
                        <div class="message-attachment" 
                             data-src="/api/images/${att.fileId}" 
                             data-sender="${isMe ? 'You' : senderDisplayName}" 
                             data-time="${msg.timestamp}">
                            <img src="/api/images/${att.fileId}" alt="${att.name}" loading="lazy">
                        </div>
                        `;
                    }
                    return '';
                }).join('');
            }

            return `
                <div class="message-line">
                    <span class="message-time">[${msg.timestamp}]</span>
                    <span class="message-sender ${isMe ? 'is-me' : ''}">&lt;${senderDisplayName}&gt;</span>
                    <span class="message-content">${msg.text}</span>
                    ${attachmentsHtml}
                </div>
            `;
        }).join('');

        let attachmentsIndicator = '';
        if (filesToAttach.length > 0) {
            attachmentsIndicator = `
                <div class="attach-indicator">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path>
                        <polyline points="13 2 13 9 20 9"></polyline>
                    </svg>
                    ${filesToAttach.length} attached
                </div>
            `;
        }

        let attachIcon = `
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48"></path>
            </svg>
        `;

        if (isUploading) {
            attachIcon = `
                <svg class="spinner" width="20" height="20" viewBox="0 0 50 50">
                    <circle cx="25" cy="25" r="20"></circle>
                </svg>
            `;
        }

        container.innerHTML = `
            <div class="chat-header">
                <h3>${activeChat.name}</h3>
                <div class="actions"></div>
            </div>
            <div class="messages-container" id="messages-container">
                ${messagesHtml}
            </div>
            <div class="input-area">
                <input type="file" id="file-input" accept="image/*" style="display:none">
                <button class="attach-btn" id="attach-btn" ${isUploading ? 'disabled' : ''}>
                    ${attachIcon}
                </button>
                ${attachmentsIndicator}
                <input type="text" class="message-input" placeholder="Type a message..." id="message-input">
                <button class="send-btn" id="send-btn" ${isUploading ? 'disabled' : ''}>
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="22" y1="2" x2="11" y2="13"></line>
                        <polygon points="22 2 15 22 11 13 2 9 22 2"></polygon>
                    </svg>
                </button>
            </div>
        `;

        // Scroll logic (simple for now)
        const messagesContainer = container.querySelector('#messages-container');
        if (messagesContainer) {
            messagesContainer.scrollTop = messagesContainer.scrollHeight;
        }

        // Attachments Preview Click
        container.querySelectorAll('.message-attachment').forEach(el => {
            el.addEventListener('click', () => {
                const src = el.dataset.src;
                const sender = el.dataset.sender;
                const time = el.dataset.time;

                const img = overlay.querySelector('#overlay-image');
                img.src = src;
                overlay.querySelector('#overlay-sender').textContent = sender;
                overlay.querySelector('#overlay-time').textContent = time;

                // Setup download button
                const dBtn = overlay.querySelector('#overlay-download');
                dBtn.onclick = (e) => {
                    e.stopPropagation();
                    const a = document.createElement('a');
                    a.href = src;
                    a.download = `image-${Date.now()}.png`; // Simple name
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                };

                overlay.classList.add('active');
            });
        });

        const input = container.querySelector('#message-input');
        const sendBtn = container.querySelector('#send-btn');
        const attachBtn = container.querySelector('#attach-btn');
        const fileInput = container.querySelector('#file-input');

        if (input) {
            input.value = currentText;
            if (!isUploading) input.focus();
        }

        const handleSend = () => {
            const text = input.value.trim();
            if (text || filesToAttach.length > 0) {
                store.sendMessage(state.activeChatId, text, filesToAttach);
                input.value = '';
                filesToAttach = []; // Clear attachments
                render(store.state); // Re-render to remove indicator
                input.focus();
            }
        };

        const handleEscape = (e) => {
            if (e.key === 'Escape' && isUploading) {
                // Cancel upload
                isUploading = false;
                // Ideally we should abort the fetch request, but for now we just reset UI
                console.log('Upload cancelled');
                render(store.state);
            }
        };
        document.addEventListener('keydown', handleEscape);


        if (sendBtn) sendBtn.addEventListener('click', handleSend);
        if (input) {
            input.addEventListener('keypress', (e) => {
                if (e.key === 'Enter') handleSend();
            });
        }

        if (attachBtn) {
            attachBtn.addEventListener('click', () => {
                fileInput.click();
            });
        }

        if (fileInput) {
            fileInput.addEventListener('change', async (e) => {
                if (e.target.files.length > 0) {
                    const file = e.target.files[0];
                    isUploading = true;
                    render(store.state); // Show spinner

                    try {
                        const result = await store.uploadImage(file);
                        filesToAttach.push({
                            type: 'image',
                            name: file.name,
                            mimeType: file.type || 'image/png', // fallback
                            fileId: result.id
                        });
                    } catch (err) {
                        alert(`Failed to upload image: ${err.message}`);
                    } finally {
                        isUploading = false;
                        fileInput.value = ''; // Reset input
                        render(store.state); // Update UI
                        input.focus();
                    }
                }
            });
        }
    };

    // Initial render
    render(store.state);

    store.subscribe((state) => {
        render(state);
    });
}
