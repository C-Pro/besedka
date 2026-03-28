import { LocationMap } from './LocationMap.js';

export function createInfoPanel(container, store) {
    container.innerHTML = `
        <div class="info-header">
            <span class="info-title">Info</span>
            <div class="profile-menu-container" id="desktop-profile-menu">
                <button type="button" class="avatar profile-avatar" id="desktop-profile-avatar" aria-expanded="false" aria-controls="desktop-profile-dropdown" aria-haspopup="true" aria-label="Profile Menu">?</button>
                <div class="profile-dropdown" id="desktop-profile-dropdown">
                    <button class="profile-menu-item" id="desktop-profile-btn" type="button">Profile</button>
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
                <div class="section-title" style="display: flex; justify-content: space-between; align-items: center;">
                    Location
                    <label class="ios-toggle" title="Share Location">
                        <input type="checkbox" id="location-toggle" aria-label="Share location" ${store.locationSharingEnabled ? 'checked' : ''}>
                        <span class="ios-toggle-slider"></span>
                    </label>
                </div>
                <div id="location-map" class="location-map-container" style="width: 100%; height: 200px; background: var(--bg-secondary); border-radius: 8px; overflow: hidden; position: relative;">
                </div>
            </div>
        </div>
    `;

    const locationToggle = container.querySelector('#location-toggle');
    locationToggle.addEventListener('change', (e) => {
        store.toggleLocationSharing(e.target.checked);
    });

    const mapContainer = container.querySelector('#location-map');
    new LocationMap(mapContainer, store);
    
    // Listen for state changes to update markers if needed, or LocationMap can listen inside.
}
