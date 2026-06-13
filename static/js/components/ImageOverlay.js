import { store } from '../state.js';

// Builds the ordered list of images for a chat from the messages loaded so far.
// This is the set of "loaded" images the overlay can navigate between.
export function getChatImages(state, chatId) {
    const messages = state.messages[chatId] || [];
    const items = [];
    for (const msg of messages) {
        for (const att of (msg.attachments || [])) {
            if (att.type === 'image' && att.fileId) {
                const isMe = msg.sender === 'me';
                const user = isMe ? null : state.users.find(u => u.id === msg.userId);
                items.push({
                    fileId: att.fileId,
                    // Full-resolution URL for the overlay; chat thumbnails append ?thumb=1.
                    src: `/api/images/${encodeURIComponent(att.fileId)}`,
                    sender: isMe ? 'You' : (user ? user.displayName : msg.userId),
                    time: msg.timestamp
                });
            }
        }
    }
    return items;
}

function createImageOverlay() {
    const MIN_SWIPE_DISTANCE = 50;

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
    }

    const imgEl = overlay.querySelector('#overlay-image');
    const senderEl = overlay.querySelector('#overlay-sender');
    const timeEl = overlay.querySelector('#overlay-time');
    const downloadBtn = overlay.querySelector('#overlay-download');
    const controls = overlay.querySelector('#overlay-controls');

    let gallery = [];
    let currentIndex = 0;

    const show = (index) => {
        if (gallery.length === 0) return;
        currentIndex = Math.max(0, Math.min(index, gallery.length - 1));
        const item = gallery[currentIndex];
        imgEl.src = item.src;
        senderEl.textContent = item.sender;
        timeEl.textContent = item.time;
        overlay.classList.toggle('single', gallery.length <= 1);
        downloadBtn.onclick = (e) => {
            e.stopPropagation();
            const a = document.createElement('a');
            a.href = item.src;
            a.download = `image-${Date.now()}.png`;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
        };
    };

    const next = () => show(currentIndex + 1);
    const prev = () => show(currentIndex - 1);

    const open = (chatId, fileId) => {
        gallery = getChatImages(store.state, chatId);
        if (gallery.length === 0) return;
        const idx = gallery.findIndex(item => item.fileId === fileId);
        show(idx === -1 ? 0 : idx);
        overlay.classList.add('active');
    };

    const close = () => {
        overlay.classList.remove('active');
        imgEl.src = '';
        gallery = [];
        currentIndex = 0;
    };

    const isActive = () => overlay.classList.contains('active');

    // Close interactions
    overlay.querySelector('#overlay-close').addEventListener('click', close);
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) close();
    });

    // Keyboard navigation (desktop): arrows step through, Escape closes.
    document.addEventListener('keydown', (e) => {
        if (!isActive()) return;
        if (e.key === 'Escape') close();
        else if (e.key === 'ArrowRight') next();
        else if (e.key === 'ArrowLeft') prev();
    });

    // Touch swipe navigation (mobile). stopPropagation keeps the app-level
    // tab-swipe in app.js from also reacting to the gesture.
    let touchStartX = 0;
    let touchStartY = 0;
    overlay.addEventListener('touchstart', (e) => {
        e.stopPropagation();
        touchStartX = e.changedTouches[0].screenX;
        touchStartY = e.changedTouches[0].screenY;
    });
    overlay.addEventListener('touchend', (e) => {
        e.stopPropagation();
        const distanceX = e.changedTouches[0].screenX - touchStartX;
        const distanceY = e.changedTouches[0].screenY - touchStartY;
        if (Math.abs(distanceX) < MIN_SWIPE_DISTANCE) return;
        if (Math.abs(distanceY) > Math.abs(distanceX)) return;
        if (distanceX < 0) next(); // swipe left -> next
        else prev();               // swipe right -> previous
    });

    // Auto-fade the controls after a few seconds of inactivity.
    let fadeTimer;
    const resetFade = () => {
        controls.classList.remove('faded');
        clearTimeout(fadeTimer);
        fadeTimer = setTimeout(() => {
            controls.classList.add('faded');
        }, 3000);
    };
    overlay.addEventListener('mousemove', resetFade);
    overlay.addEventListener('click', resetFade);

    return { open, close };
}

export const imageOverlay = createImageOverlay();
