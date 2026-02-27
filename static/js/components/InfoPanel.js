import { store } from '../state.js';

export function createInfoPanel(container) {
    const render = () => {
        container.innerHTML = `
            <div class="info-header">
                <span class="info-title">Info</span>
                <div class="profile-menu-container" id="desktop-profile-menu">
                    <div class="avatar profile-avatar" id="desktop-profile-avatar">?</div>
                    <div class="profile-dropdown" id="desktop-profile-dropdown">
                        <button class="profile-menu-item disabled">Profile</button>
                        <button class="profile-menu-item disabled">Settings</button>
                        <button class="profile-menu-item" id="desktop-logoff-btn">Log Off</button>
                    </div>
                </div>
            </div>
            <div class="info-content">
                <div class="info-section">
                    <div class="section-title">Last Document</div>
                    <div class="placeholder-box">
                        No documents yet
                    </div>
                </div>
                <div class="info-section">
                    <div class="section-title">Location</div>
                    <div class="placeholder-box">
                        Map View
                    </div>
                </div>
            </div>
        `;
    };

    render(store.state);
    store.subscribe(render);
}
