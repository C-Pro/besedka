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

    // --- Zoom / pan state -------------------------------------------------
    const MAX_SCALE = 4;
    let scale = 1;
    let translateX = 0;
    let translateY = 0;

    const applyTransform = () => {
        imgEl.style.transform = `translate(${translateX}px, ${translateY}px) scale(${scale})`;
        overlay.classList.toggle('zoomed', scale > 1);
    };

    // Keep the image from being panned entirely out of view. Bounds are based on
    // the unscaled layout size (offsetWidth/Height) grown by the current scale.
    const clampPan = () => {
        const overflowX = Math.max(0, (imgEl.offsetWidth * scale - window.innerWidth) / 2);
        const overflowY = Math.max(0, (imgEl.offsetHeight * scale - window.innerHeight) / 2);
        translateX = Math.max(-overflowX, Math.min(overflowX, translateX));
        translateY = Math.max(-overflowY, Math.min(overflowY, translateY));
    };

    // Zoom to newScale while keeping the point (focalX, focalY) in viewport
    // coordinates stationary under the fingers / cursor.
    const zoomTo = (newScale, focalX, focalY) => {
        newScale = Math.max(1, Math.min(MAX_SCALE, newScale));
        const cx = window.innerWidth / 2;
        const cy = window.innerHeight / 2;
        const fx = focalX - cx;
        const fy = focalY - cy;
        const ratio = newScale / scale;
        translateX = fx - ratio * (fx - translateX);
        translateY = fy - ratio * (fy - translateY);
        scale = newScale;
        if (scale === 1) {
            translateX = 0;
            translateY = 0;
        } else {
            clampPan();
        }
        applyTransform();
    };

    const resetZoom = () => {
        scale = 1;
        translateX = 0;
        translateY = 0;
        applyTransform();
    };

    const show = (index) => {
        if (gallery.length === 0) return;
        currentIndex = Math.max(0, Math.min(index, gallery.length - 1));
        resetZoom();
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
        resetZoom();
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

    // Touch gestures (mobile): pinch to zoom, drag to pan when zoomed, and
    // single-finger swipe to navigate when at 1x. stopPropagation keeps the
    // app-level tab-swipe in app.js from also reacting to the gesture.
    let touchStartX = 0;
    let touchStartY = 0;
    let panLastX = 0;
    let panLastY = 0;
    let isPanning = false;
    let isPinching = false;
    let pinchStartDist = 0;
    let pinchStartScale = 1;
    let lastTapTime = 0;

    const touchDistance = (t0, t1) => Math.hypot(t0.clientX - t1.clientX, t0.clientY - t1.clientY);
    const touchMidpoint = (t0, t1) => ({ x: (t0.clientX + t1.clientX) / 2, y: (t0.clientY + t1.clientY) / 2 });

    overlay.addEventListener('touchstart', (e) => {
        e.stopPropagation();
        if (e.touches.length === 2) {
            isPinching = true;
            isPanning = false;
            pinchStartDist = touchDistance(e.touches[0], e.touches[1]);
            pinchStartScale = scale;
        } else if (e.touches.length === 1) {
            touchStartX = e.changedTouches[0].screenX;
            touchStartY = e.changedTouches[0].screenY;
            panLastX = e.changedTouches[0].clientX;
            panLastY = e.changedTouches[0].clientY;
            isPanning = scale > 1;
        }
    });

    overlay.addEventListener('touchmove', (e) => {
        if (isPinching && e.touches.length === 2) {
            e.preventDefault();
            overlay.classList.add('panning');
            const dist = touchDistance(e.touches[0], e.touches[1]);
            if (pinchStartDist > 0) {
                const mid = touchMidpoint(e.touches[0], e.touches[1]);
                zoomTo(pinchStartScale * (dist / pinchStartDist), mid.x, mid.y);
            }
        } else if (isPanning && e.touches.length === 1) {
            e.preventDefault();
            overlay.classList.add('panning');
            const t = e.touches[0];
            translateX += t.clientX - panLastX;
            translateY += t.clientY - panLastY;
            panLastX = t.clientX;
            panLastY = t.clientY;
            clampPan();
            applyTransform();
        }
    }, { passive: false });

    overlay.addEventListener('touchend', (e) => {
        e.stopPropagation();
        if (e.touches.length === 0) {
            isPanning = false;
            overlay.classList.remove('panning');
        }
        if (isPinching) {
            // Wait until all fingers lift before clearing the pinch flag.
            if (e.touches.length === 0) isPinching = false;
            return;
        }

        const distanceX = e.changedTouches[0].screenX - touchStartX;
        const distanceY = e.changedTouches[0].screenY - touchStartY;
        const moved = Math.abs(distanceX) > 10 || Math.abs(distanceY) > 10;

        // Double-tap toggles between 1x and 2x, centered on the tap.
        if (!moved) {
            const now = e.timeStamp;
            if (now - lastTapTime < 300) {
                lastTapTime = 0;
                const tap = e.changedTouches[0];
                zoomTo(scale > 1 ? 1 : 2, tap.clientX, tap.clientY);
                return;
            }
            lastTapTime = now;
        }

        // When zoomed in, a single-finger drag pans instead of navigating.
        if (scale > 1) return;
        if (Math.abs(distanceX) < MIN_SWIPE_DISTANCE) return;
        if (Math.abs(distanceY) > Math.abs(distanceX)) return;
        if (distanceX < 0) next(); // swipe left -> next
        else prev();               // swipe right -> previous
    });

    // Desktop: wheel / trackpad-pinch zooms toward the cursor, double-click
    // toggles zoom, and click-drag pans while zoomed.
    overlay.addEventListener('wheel', (e) => {
        if (!isActive()) return;
        e.preventDefault();
        const factor = Math.exp(-e.deltaY * 0.002);
        zoomTo(scale * factor, e.clientX, e.clientY);
    }, { passive: false });

    imgEl.addEventListener('dblclick', (e) => {
        e.preventDefault();
        zoomTo(scale > 1 ? 1 : 2, e.clientX, e.clientY);
    });

    let mouseDragging = false;
    let mouseLastX = 0;
    let mouseLastY = 0;
    imgEl.addEventListener('mousedown', (e) => {
        if (scale <= 1) return;
        e.preventDefault();
        mouseDragging = true;
        mouseLastX = e.clientX;
        mouseLastY = e.clientY;
        overlay.classList.add('panning');
    });
    document.addEventListener('mousemove', (e) => {
        if (!mouseDragging) return;
        translateX += e.clientX - mouseLastX;
        translateY += e.clientY - mouseLastY;
        mouseLastX = e.clientX;
        mouseLastY = e.clientY;
        clampPan();
        applyTransform();
    });
    document.addEventListener('mouseup', () => {
        if (!mouseDragging) return;
        mouseDragging = false;
        overlay.classList.remove('panning');
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
