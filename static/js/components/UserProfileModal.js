import { createMusicPlayer } from './MusicPlayer.js';
import { createProfileSettingsModal } from './ProfileSettingsModal.js';

/**
 * Creates and displays the public User Profile View modal.
 * @param {Object} store - The central state store.
 * @param {string} userId - The target user ID to display.
 */
export async function createUserProfileModal(store, userId) {
    if (!userId) return;

    // Remove existing profile modal if present
    const existingOverlay = document.getElementById('user-profile-modal-overlay');
    if (existingOverlay) {
        existingOverlay.remove();
    }

    // Refresh users to ensure latest profile details (song, avatar, display name)
    try {
        await store.fetchUsers();
    } catch (e) {
        console.error("Failed to refresh users:", e);
    }

    const state = store.state;
    const currentUser = state.currentUser;
    const isSelf = currentUser && currentUser.id === userId;

    // Find full user details
    let targetUser = isSelf ? currentUser : state.users.find(u => u.id === userId);
    if (!targetUser && isSelf) targetUser = currentUser;

    if (!targetUser) return;

    // If user object from state has matching ID, resolve updated details
    const fullUserInList = state.users.find(u => u.id === userId);
    const avatarUrl = fullUserInList?.avatarUrl || targetUser.avatarUrl;
    const displayName = fullUserInList?.displayName || targetUser.displayName || targetUser.name || 'User';
    const userName = fullUserInList?.userName || targetUser.userName || '';
    const isOnline = fullUserInList?.presence?.online ?? targetUser.presence?.online ?? false;
    const songUrl = fullUserInList?.songUrl || targetUser.songUrl;
    const songTitle = fullUserInList?.songTitle || targetUser.songTitle;
    const songArtist = fullUserInList?.songArtist || targetUser.songArtist;
    const bio = fullUserInList?.bio || targetUser.bio || '';

    // Modal elements
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'user-profile-modal-overlay';

    const modal = document.createElement('div');
    modal.className = 'modal-container user-profile-card-modal';
    modal.id = 'user-profile-modal';

    let musicPlayer = null;

    modal.innerHTML = `
        <div class="modal-header">
            <h2>User Profile</h2>
            <button class="modal-close-btn" id="user-profile-modal-close" aria-label="Close">&times;</button>
        </div>
        <div class="modal-body user-profile-body">
            <div class="user-profile-header-card">
                <div class="user-profile-avatar-container">
                    ${avatarUrl 
                        ? `<img src="${avatarUrl}" alt="${displayName} Avatar" class="user-profile-avatar-img">`
                        : `<div class="avatar-placeholder user-profile-avatar-placeholder">${displayName.charAt(0).toUpperCase()}</div>`
                    }
                    <span class="user-profile-status-indicator ${isOnline ? 'online' : 'offline'}" title="${isOnline ? 'Online' : 'Offline'}"></span>
                </div>
                <div class="user-profile-identity">
                    <h3 class="user-profile-display-name">${displayName}</h3>
                    ${userName ? `<div class="user-profile-username">@${userName}</div>` : ''}
                    <div class="user-profile-status-badge ${isOnline ? 'status-online' : 'status-offline'}">
                        <span class="status-dot"></span> ${isOnline ? 'Online' : 'Offline'}
                    </div>
                    ${bio ? `<div class="user-profile-bio">${bio}</div>` : ''}
                </div>
            </div>

            ${songUrl ? `
            <div class="user-profile-song-section">
                <h4>Profile Song</h4>
                <div id="user-profile-music-container" class="user-profile-music-container">
                    <!-- Music player will be attached here -->
                </div>
            </div>
            ` : ''}

            <div class="user-profile-actions">
                ${isSelf 
                    ? `<button type="button" class="btn btn-primary" id="open-profile-settings-btn">⚙️ Edit Profile Settings</button>`
                    : `<button type="button" class="btn btn-primary" id="profile-send-message-btn">💬 Send Message</button>`
                }
            </div>
        </div>
    `;

    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    // Attach Music Player if song exists
    const musicContainer = modal.querySelector('#user-profile-music-container');
    if (musicContainer && songUrl) {
        musicPlayer = createMusicPlayer({ songUrl, title: songTitle, artist: songArtist });
        musicContainer.appendChild(musicPlayer);
    }

    // Modal Close Logic & Cleanup
    const closeModal = () => {
        if (musicPlayer && typeof musicPlayer.destroy === 'function') {
            musicPlayer.destroy();
        }
        overlay.remove();
        document.removeEventListener('keydown', handleEsc);
    };

    const handleEsc = (e) => {
        if (e.key === 'Escape') closeModal();
    };

    modal.querySelector('#user-profile-modal-close').addEventListener('click', closeModal);
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) closeModal();
    });
    document.addEventListener('keydown', handleEsc);

    // Action button listeners
    if (isSelf) {
        const editBtn = modal.querySelector('#open-profile-settings-btn');
        if (editBtn) {
            editBtn.addEventListener('click', () => {
                closeModal();
                createProfileSettingsModal(store);
            });
        }
    } else {
        const sendMsgBtn = modal.querySelector('#profile-send-message-btn');
        if (sendMsgBtn) {
            sendMsgBtn.addEventListener('click', () => {
                closeModal();
                // Find or construct DM chat ID
                const currentUserId = currentUser.id;
                const dmChat = state.chats.find(c => c.isDm && c.id.includes(userId));
                if (dmChat) {
                    store.setActiveChat(dmChat.id);
                } else {
                    const sortedIds = [currentUserId, userId].sort();
                    const dmId = `dm_${sortedIds[0]}_${sortedIds[1]}`;
                    store.setActiveChat(dmId);
                }
            });
        }
    }
}
