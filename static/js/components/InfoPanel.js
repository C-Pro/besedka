export function createInfoPanel(container) {
    container.innerHTML = `
        <div class="info-header">
            <span class="info-title">Info</span>
            <div class="profile-menu-container" id="desktop-profile-menu">
                <button type="button" class="avatar profile-avatar" id="desktop-profile-avatar" aria-expanded="false" aria-controls="desktop-profile-dropdown" aria-haspopup="true" aria-label="Profile Menu">?</button>
                <div class="profile-dropdown" id="desktop-profile-dropdown">
                    <button class="profile-menu-item disabled" type="button" disabled aria-disabled="true">Profile</button>
                    <button class="profile-menu-item disabled" type="button" disabled aria-disabled="true">Settings</button>
                    <button class="profile-menu-item" id="desktop-logoff-btn" type="button">Log Off</button>
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
}
