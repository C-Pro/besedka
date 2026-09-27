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
    const theme = store.settings.appearance?.theme || 'dark';
    const fontScale = window.besedkaAppearance?.getFontScale?.() || 100;
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
                <h3>Appearance</h3>
                <fieldset class="theme-selector">
                    <legend class="visually-hidden">Color theme</legend>
                    ${[
                        ['dark', 'Dark'],
                        ['light', 'Light'],
                        ['system', 'System']
                    ].map(([value, label]) => `
                        <label class="theme-option">
                            <input type="radio" name="theme" value="${value}" ${theme === value ? 'checked' : ''}>
                            <span>${label}</span>
                        </label>
                    `).join('')}
                </fieldset>
                <p class="settings-help">System follows this device's appearance setting.</p>
                <div class="font-size-setting">
                    <div class="font-size-setting-header">
                        <label for="font-size-slider">Text size</label>
                        <output id="font-size-output" for="font-size-slider">${fontScale}%</output>
                    </div>
                    <input class="font-size-slider" id="font-size-slider" type="range"
                        min="80" max="150" step="10" value="${fontScale}"
                        aria-describedby="font-size-help" aria-valuetext="${fontScale} percent">
                    <div class="font-size-scale-labels" aria-hidden="true">
                        <span>Smaller</span>
                        <span>Larger</span>
                    </div>
                    <div class="font-size-setting-footer">
                        <p class="settings-help" id="font-size-help">Adjusts text throughout the app.</p>
                        <button class="settings-reset-btn" id="font-size-reset" type="button" ${fontScale === 100 ? 'disabled' : ''}>Reset</button>
                    </div>
                </div>
            </div>
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
        window.removeEventListener('besedka-appearance-change', handleAppearanceChange);
    };
    const handleEsc = (e) => {
        if (e.key === 'Escape') closeModal();
    };

    closeBtn.addEventListener('click', closeModal);
    bgOverlay.addEventListener('click', (e) => {
        if (e.target === bgOverlay) closeModal();
    });
    document.addEventListener('keydown', handleEsc);

    modal.querySelectorAll('input[name="theme"]').forEach((input) => {
        input.addEventListener('change', async (e) => {
            if (!e.target.checked) return;
            const activeControl = e.target;
            const controls = Array.from(modal.querySelectorAll('input[name="theme"]'));
            controls.forEach(control => { control.disabled = true; });
            try {
                await store.setThemePreference(e.target.value);
            } catch {
                controls.forEach(control => {
                    control.checked = control.value === store.settings.appearance.theme;
                });
            } finally {
                controls.forEach(control => { control.disabled = false; });
                if (modal.isConnected) activeControl.focus();
            }
        });
    });

    const fontSizeSlider = modal.querySelector('#font-size-slider');
    const fontSizeOutput = modal.querySelector('#font-size-output');
    const fontSizeReset = modal.querySelector('#font-size-reset');

    const updateFontScaleDisplay = (value) => {
        fontSizeOutput.value = `${value}%`;
        fontSizeOutput.textContent = `${value}%`;
        fontSizeSlider.setAttribute('aria-valuetext', `${value} percent`);
        fontSizeReset.disabled = Number(value) === 100;
    };

    const handleAppearanceChange = (event) => {
        if (event.detail?.changedProperty !== 'fontScale') return;
        const value = event.detail.fontScale;
        fontSizeSlider.value = String(value);
        updateFontScaleDisplay(value);
    };
    window.addEventListener('besedka-appearance-change', handleAppearanceChange);

    fontSizeSlider.addEventListener('input', (event) => {
        const value = Number(event.target.value);
        if (!window.besedkaAppearance?.isValidFontScale?.(value)) return;
        if (!window.besedkaAppearance?.setFontScale) return;
        window.besedkaAppearance.setFontScale(value);
        updateFontScaleDisplay(value);
    });

    fontSizeReset.addEventListener('click', () => {
        if (!window.besedkaAppearance?.setFontScale) return;
        fontSizeSlider.value = '100';
        window.besedkaAppearance.setFontScale(100);
        updateFontScaleDisplay(100);
    });

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
