import { LocationMap } from './LocationMap.js';
import { imageOverlay, getChatImages } from './ImageOverlay.js';

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
                <div class="section-title">Last Image</div>
                <div class="placeholder-box" id="last-image-box">
                    No images yet
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
                <div id="location-map" class="location-map-container" style="width: 100%; height: 200px; background: #0f172a; border-radius: 8px; overflow: hidden; position: relative;">
                </div>
            </div>
        </div>
    `;

    const locationToggle = container.querySelector('#location-toggle');
    locationToggle.addEventListener('change', (e) => {
        store.toggleLocationSharing(e.target.checked);
    });

    // Last Image preview: shows the most recent image of the active chat and
    // opens the shared overlay (with navigation) when clicked.
    const lastImageBox = container.querySelector('#last-image-box');
    let lastImageKey = null;
    const renderLastImage = (state) => {
        const images = getChatImages(state, state.activeChatId);
        const last = images.length > 0 ? images[images.length - 1] : null;
        const key = `${state.activeChatId || ''}:${last ? last.fileId : ''}`;
        if (key === lastImageKey) return; // avoid needless DOM churn
        lastImageKey = key;
        lastImageBox.replaceChildren();
        lastImageBox.onclick = null;
        if (!last) {
            lastImageBox.classList.remove('has-image');
            lastImageBox.textContent = 'No images yet';
            return;
        }
        lastImageBox.classList.add('has-image');
        const img = document.createElement('img');
        img.src = `${last.src}?thumb=1`; // preview uses the thumbnail
        img.alt = 'Last image';
        lastImageBox.appendChild(img);
        lastImageBox.onclick = () => imageOverlay.open(state.activeChatId, last.fileId);
    };
    renderLastImage(store.state);
    store.subscribe(renderLastImage);

    const mapContainer = container.querySelector('#location-map');
    
    let mapInitialized = false;
    let unsubscribe = null;

    const initMap = () => {
        if (mapInitialized) return;
        mapInitialized = true;
        new LocationMap(mapContainer, store);
        window.removeEventListener('resize', handleResize);
        if (unsubscribe) {
            unsubscribe();
        }
    };

    const handleResize = () => {
        if (window.innerWidth > 768) {
            initMap();
        }
    };

    if (window.innerWidth > 768 || store.state.mobileActiveTab === 'info-panel') {
        initMap();
    } else {
        window.addEventListener('resize', handleResize);
        unsubscribe = store.subscribe((state) => {
            if (state.mobileActiveTab === 'info-panel') {
                initMap();
            }
        });
    }
}
