// Settings dialog. Currently hosts notification-sound preferences; designed so
// further settings sections can be appended over time. Mirrors ProfileModal's
// overlay/container/close conventions.

const SOUND_TOGGLES = [
    { key: 'soundAllMessages', label: 'Play sound for all incoming messages' },
    { key: 'soundDirectMessages', label: 'Play sound for direct messages' },
    { key: 'soundMentions', label: 'Play sound for mentions' },
    { key: 'suppressWhenChatOpen', label: 'Mute when the chat is already open' }
];

export function createSettingsModal(store) {
    if (document.getElementById('settings-modal')) return;

    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'settings-modal-overlay';

    const modal = document.createElement('div');
    modal.className = 'modal-container';
    modal.id = 'settings-modal';

    const notifications = store.settings.notifications;
    const rows = SOUND_TOGGLES.map(({ key, label }) => `
        <div class="settings-row">
            <span class="settings-row-label">${label}</span>
            <label class="ios-toggle">
                <input type="checkbox" data-setting="${key}" ${notifications[key] ? 'checked' : ''} aria-label="${label}">
                <span class="ios-toggle-slider"></span>
            </label>
        </div>
    `).join('');

    modal.innerHTML = `
        <div class="modal-header">
            <h2>Settings</h2>
            <button class="modal-close-btn" id="settings-modal-close" aria-label="Close">&times;</button>
        </div>
        <div class="modal-body">
            <div class="profile-section">
                <h3>Notification Sounds</h3>
                ${rows}
                <button class="btn btn-secondary" id="settings-test-sound" style="margin-top: 10px;">Test sound</button>
            </div>
        </div>
    `;

    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    const closeBtn = document.getElementById('settings-modal-close');
    const bgOverlay = document.getElementById('settings-modal-overlay');

    const closeModal = () => {
        overlay.remove();
        document.removeEventListener('keydown', handleEsc);
    };
    const handleEsc = (e) => {
        if (e.key === 'Escape') closeModal();
    };

    closeBtn.addEventListener('click', closeModal);
    bgOverlay.addEventListener('click', (e) => {
        if (e.target === bgOverlay) closeModal();
    });
    document.addEventListener('keydown', handleEsc);

    modal.querySelectorAll('input[data-setting]').forEach((input) => {
        input.addEventListener('change', async (e) => {
            const key = e.target.dataset.setting;
            const value = e.target.checked;
            try {
                await store.setNotificationSetting(key, value);
            } catch {
                // Persistence failed; reflect the reverted store state.
                e.target.checked = store.settings.notifications[key];
            }
        });
    });

    document.getElementById('settings-test-sound').addEventListener('click', () => {
        store.playNotificationSound();
    });
}
