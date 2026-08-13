import { bufferToBase64URL, base64URLToBuffer } from '../state.js';
import { createUserProfileModal } from './UserProfileModal.js';
import { extractID3FromBuffer } from '../id3.js';

export function createProfileSettingsModal(store) {
    // Check if modal already exists
    if (document.getElementById('profile-modal')) return;

    // Create modal overlay
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay';
    overlay.id = 'profile-modal-overlay';

    // Create modal container
    const modal = document.createElement('div');
    modal.className = 'modal-container';
    modal.id = 'profile-modal';

    // Build modal HTML
    modal.innerHTML = `
        <div class="modal-header">
            <h2>Profile Settings</h2>
            <div style="display: flex; gap: 8px; align-items: center;">
                <button type="button" class="btn btn-secondary" id="view-public-profile-btn" style="font-size: 0.85rem; padding: 4px 10px;">👁️ View Public Profile</button>
                <button class="modal-close-btn" id="profile-modal-close" aria-label="Close">&times;</button>
            </div>
        </div>
        <div class="modal-body profile-modal-body">
            
            <!-- Avatar Section -->
            <div class="profile-section">
                <h3>Avatar</h3>
                <div class="avatar-edit-container">
                    <div class="current-avatar-preview" id="profile-avatar-preview">
                        <!-- Filled by state -->
                    </div>
                    <div class="avatar-upload-actions">
                        <label class="btn btn-secondary" for="avatar-upload-input">Choose Image</label>
                        <input type="file" id="avatar-upload-input" accept="image/jpeg, image/png, image/gif, image/webp" style="display: none;">
                        <button class="btn btn-primary" id="avatar-save-btn" disabled>Upload</button>
                    </div>
                </div>
                <div id="avatar-error" class="error-text" style="display:none;"></div>
                <div id="avatar-success" class="success-text" style="display:none;">Avatar updated successfully!</div>
            </div>

            <!-- Display Name Section -->
            <div class="profile-section">
                <h3>Display Name</h3>
                <div class="input-group">
                    <input type="text" id="profile-display-name-input" class="form-control" placeholder="New display name">
                    <button class="btn btn-primary" id="display-name-save-btn">Save</button>
                </div>
                <div id="display-name-error" class="error-text" style="display:none;"></div>
                <div id="display-name-success" class="success-text" style="display:none;">Display name updated!</div>
            </div>

            <!-- Bio / Status Section -->
            <div class="profile-section">
                <h3>Bio / Status</h3>
                <p class="text-muted">Short status or bio featured on your public profile (up to 128 characters).</p>
                <div class="input-group">
                    <input type="text" id="profile-bio-input" class="form-control" maxlength="128" placeholder="What's on your mind?">
                    <button class="btn btn-primary" id="bio-save-btn">Save</button>
                </div>
                <div style="font-size: 0.8rem; text-align: right; color: var(--text-muted, #8e8e93); margin-top: 4px;">
                    <span id="bio-char-count">0</span> / 128
                </div>
                <div id="bio-error" class="error-text" style="display:none;"></div>
                <div id="bio-success" class="success-text" style="display:none;">Bio updated!</div>
            </div>

            <!-- Profile Song Section -->
            <div class="profile-section">
                <h3>Profile Song</h3>
                <p class="text-muted">Choose an audio track to feature on your profile page.</p>
                <div class="song-edit-container">
                    <div class="song-file-actions" style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-bottom: 10px;">
                        <label class="btn btn-secondary" for="song-file-input" id="song-file-label">Choose Audio File</label>
                        <input type="file" id="song-file-input" accept="audio/*" style="display: none;">
                        <span id="selected-song-file-name" class="text-muted" style="font-size: 0.85rem;">No file chosen</span>
                    </div>

                    <div id="song-metadata-container" style="display: none;">
                        <div class="input-group" style="margin-bottom: 8px;">
                            <input type="text" id="song-title-input" class="form-control" placeholder="Song Title (e.g. My Anthem)">
                        </div>
                        <div class="input-group" style="margin-bottom: 8px;">
                            <input type="text" id="song-artist-input" class="form-control" placeholder="Artist Name">
                        </div>
                    </div>

                    <div style="margin-top: 10px; display: flex; gap: 8px;">
                        <button class="btn btn-primary" id="song-save-btn" style="display:none;">Save Song</button>
                        <button class="btn btn-danger" id="song-remove-btn" style="display:none;">Remove Song</button>
                    </div>
                </div>
                <div id="song-error" class="error-text" style="display:none; margin-top:6px;"></div>
                <div id="song-success" class="success-text" style="display:none; margin-top:6px;">Profile song saved!</div>
            </div>

            <!-- Passkeys Section -->
            <div class="profile-section">
                <h3>Passkeys</h3>
                <p class="text-muted">Use passkeys for passwordless sign-in. Your device will prompt you to save a passkey.</p>
                <div id="passkeys-list" class="passkeys-list">
                    <!-- Loaded dynamically -->
                </div>
                <button class="btn btn-secondary" id="passkey-register-btn" style="margin-top: 10px;">Register New Passkey</button>
                <div id="passkey-error" class="error-text" style="display:none;"></div>
                <div id="passkey-success" class="success-text" style="display:none;"></div>
            </div>

            <!-- Password Reset Section -->
            <div class="profile-section danger-zone">
                <h3 class="danger-text">Reset Password</h3>
                <p class="text-muted">This will invalidate your current password and log out all active sessions. You will be provided with a new setup link to choose a new password and configure 2FA.</p>
                <button class="btn btn-danger" id="password-reset-btn">Reset Password</button>
                <div id="password-reset-error" class="error-text" style="display:none;"></div>
                <div id="password-reset-success" class="success-text" style="display:none; margin-top: 10px; word-break: break-all;">
                    Password reset! Copy this link to setup your new password: <br>
                    <a href="#" id="password-reset-link" target="_blank" rel="noopener noreferrer" style="font-weight: bold;"></a>
                    <br><br>
                    <button class="btn btn-secondary" id="logout-after-reset-btn">Return to Login</button>
                </div>
            </div>
            
        </div>
    `;

    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    // Initial State Population
    const currentUser = store.state.currentUser;
    const displayNameInput = document.getElementById('profile-display-name-input');
    const avatarPreview = document.getElementById('profile-avatar-preview');
    const bioInput = document.getElementById('profile-bio-input');
    const bioCharCount = document.getElementById('bio-char-count');
    const songTitleInput = document.getElementById('song-title-input');
    const songArtistInput = document.getElementById('song-artist-input');
    const songMetadataContainer = document.getElementById('song-metadata-container');
    const songSaveBtn = document.getElementById('song-save-btn');
    const songRemoveBtn = document.getElementById('song-remove-btn');

    let fullUser = null;
    if (currentUser) {
        displayNameInput.value = currentUser.name || '';
        fullUser = store.state.users.find(u => u.id === currentUser.id);

        avatarPreview.innerHTML = '';
        if (fullUser?.avatarUrl) {
            const img = document.createElement('img');
            img.src = fullUser.avatarUrl;
            img.alt = 'Avatar';
            img.width = 64;
            img.height = 64;
            img.className = 'avatar-image-full';
            avatarPreview.appendChild(img);
        } else {
            const div = document.createElement('div');
            div.className = 'avatar-placeholder';
            div.textContent = (currentUser.name || '?').charAt(0).toUpperCase();
            avatarPreview.appendChild(div);
        }

        if (fullUser?.bio) {
            bioInput.value = fullUser.bio;
        }
        if (bioCharCount) {
            bioCharCount.textContent = bioInput.value.length;
        }

        if (fullUser?.songUrl) {
            songMetadataContainer.style.display = 'block';
            songSaveBtn.style.display = 'inline-block';
            songRemoveBtn.style.display = 'inline-block';
            if (fullUser?.songTitle) songTitleInput.value = fullUser.songTitle;
            if (fullUser?.songArtist) songArtistInput.value = fullUser.songArtist;
        } else {
            songMetadataContainer.style.display = 'none';
            songSaveBtn.style.display = 'none';
            songRemoveBtn.style.display = 'none';
        }
    }

    // --- References ---
    const closeBtn = document.getElementById('profile-modal-close') || document.getElementById('profile-settings-modal-close');
    const bgOverlay = document.getElementById('profile-modal-overlay') || document.getElementById('profile-settings-modal-overlay');
    const viewPublicProfileBtn = document.getElementById('view-public-profile-btn');

    // Avatar
    const avatarInput = document.getElementById('avatar-upload-input');
    const avatarSaveBtn = document.getElementById('avatar-save-btn');
    const avatarError = document.getElementById('avatar-error');
    const avatarSuccess = document.getElementById('avatar-success');
    let selectedAvatarFile = null;

    // Display Name
    const displayNameSaveBtn = document.getElementById('display-name-save-btn');
    const displayNameError = document.getElementById('display-name-error');
    const displayNameSuccess = document.getElementById('display-name-success');

    // Bio
    const bioSaveBtn = document.getElementById('bio-save-btn');
    const bioError = document.getElementById('bio-error');
    const bioSuccess = document.getElementById('bio-success');

    // Song
    const songFileInput = document.getElementById('song-file-input');
    const songFileName = document.getElementById('selected-song-file-name');
    const songError = document.getElementById('song-error');
    const songSuccess = document.getElementById('song-success');
    let selectedSongFile = null;

    // Password Reset
    const passwordResetBtn = document.getElementById('password-reset-btn');
    const passwordResetError = document.getElementById('password-reset-error');
    const passwordResetSuccess = document.getElementById('password-reset-success');
    const passwordResetLink = document.getElementById('password-reset-link');
    const logoutAfterResetBtn = document.getElementById('logout-after-reset-btn');

    // Passkeys
    const passkeysList = document.getElementById('passkeys-list');
    const passkeyRegisterBtn = document.getElementById('passkey-register-btn');
    const passkeyError = document.getElementById('passkey-error');
    const passkeySuccess = document.getElementById('passkey-success');

    // --- Helpers ---
    const closeModal = () => {
        overlay.remove();
        document.removeEventListener('keydown', handleEsc);
    };

    const handleEsc = (e) => {
        if (e.key === 'Escape') closeModal();
    };

    const resetMessages = () => {
        avatarError.style.display = 'none';
        avatarSuccess.style.display = 'none';
        displayNameError.style.display = 'none';
        displayNameSuccess.style.display = 'none';
        if (bioError) bioError.style.display = 'none';
        if (bioSuccess) bioSuccess.style.display = 'none';
        songError.style.display = 'none';
        songSuccess.style.display = 'none';
        passwordResetError.style.display = 'none';
        passkeyError.style.display = 'none';
        passkeySuccess.style.display = 'none';
    };

    // --- Event Listeners ---
    closeBtn.addEventListener('click', closeModal);
    if (viewPublicProfileBtn) {
        viewPublicProfileBtn.addEventListener('click', () => {
            if (currentUser?.id) {
                createUserProfileModal(store, currentUser.id);
            }
        });
    }
    bgOverlay.addEventListener('click', (e) => {
        if (e.target === bgOverlay) closeModal();
    });
    document.addEventListener('keydown', handleEsc);

    // Avatar Upload Logic
    avatarInput.addEventListener('change', (e) => {
        resetMessages();
        if (e.target.files?.[0]) {
            selectedAvatarFile = e.target.files[0];

            if (selectedAvatarFile.size > 5 * 1024 * 1024) {
                avatarError.textContent = "File must be smaller than 5MB";
                avatarError.style.display = 'block';
                avatarSaveBtn.disabled = true;
                return;
            }

            const reader = new FileReader();
            reader.onload = (re) => {
                avatarPreview.innerHTML = '';
                const img = document.createElement('img');
                img.src = re.target.result;
                img.alt = 'Preview';
                img.width = 64;
                img.height = 64;
                img.className = 'avatar-image-full';
                avatarPreview.appendChild(img);
            };
            reader.readAsDataURL(selectedAvatarFile);

            avatarSaveBtn.disabled = false;
        } else {
            selectedAvatarFile = null;
            avatarSaveBtn.disabled = true;
        }
    });

    avatarSaveBtn.addEventListener('click', async () => {
        if (!selectedAvatarFile) return;

        resetMessages();
        avatarSaveBtn.disabled = true;
        avatarSaveBtn.textContent = 'Uploading...';

        try {
            await store.uploadAvatar(selectedAvatarFile);
            avatarSuccess.style.display = 'block';
            selectedAvatarFile = null;
            avatarInput.value = '';
            await store.fetchUsers();
        } catch (error) {
            avatarError.textContent = error.message || "Failed to upload avatar";
            avatarError.style.display = 'block';
        } finally {
            avatarSaveBtn.textContent = 'Upload';
            avatarSaveBtn.disabled = selectedAvatarFile === null;
        }
    });

    // Display Name Logic
    displayNameSaveBtn.addEventListener('click', async () => {
        const newName = displayNameInput.value.trim();
        if (!newName) {
            displayNameError.textContent = "Display name cannot be empty";
            displayNameError.style.display = 'block';
            return;
        }

        resetMessages();
        displayNameSaveBtn.disabled = true;
        displayNameSaveBtn.textContent = 'Saving...';

        try {
            const result = await store.updateDisplayName(newName);
            displayNameSuccess.style.display = 'block';
            const updatedName = result?.displayName ? result.displayName : newName;
            store.setState({ currentUser: { ...store.state.currentUser, name: updatedName } });
            await store.fetchUsers();
        } catch (error) {
            displayNameError.textContent = error.message || "Failed to update display name";
            displayNameError.style.display = 'block';
        } finally {
            displayNameSaveBtn.textContent = 'Save';
            displayNameSaveBtn.disabled = false;
        }
    });

    displayNameInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            displayNameSaveBtn.click();
        }
    });

    // Bio / Status Logic
    if (bioInput) {
        bioInput.addEventListener('input', () => {
            if (bioCharCount) {
                bioCharCount.textContent = bioInput.value.length;
            }
        });
        bioInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                bioSaveBtn?.click();
            }
        });
    }

    if (bioSaveBtn) {
        bioSaveBtn.addEventListener('click', async () => {
            const bioText = bioInput.value.trim();
            resetMessages();
            bioSaveBtn.disabled = true;
            bioSaveBtn.textContent = 'Saving...';

            try {
                const result = await store.updateBio(bioText);
                bioSuccess.style.display = 'block';
                if (result?.bio !== undefined && bioInput) {
                    bioInput.value = result.bio;
                    if (bioCharCount) bioCharCount.textContent = result.bio.length;
                }
                await store.fetchUsers();
            } catch (error) {
                bioError.textContent = error.message || "Failed to update bio";
                bioError.style.display = 'block';
            } finally {
                bioSaveBtn.textContent = 'Save';
                bioSaveBtn.disabled = false;
            }
        });
    }

    // Profile Song Logic
    songFileInput.addEventListener('change', (e) => {
        resetMessages();
        if (e.target.files?.[0]) {
            selectedSongFile = e.target.files[0];
            songFileName.textContent = selectedSongFile.name;

            // Extract ID3 tags or fallback filename to pre-fill metadata fields
            const reader = new FileReader();
            reader.onload = (re) => {
                const meta = extractID3FromBuffer(re.target.result, selectedSongFile.name);
                if (meta.title) songTitleInput.value = meta.title;
                if (meta.artist) songArtistInput.value = meta.artist;
                songMetadataContainer.style.display = 'block';
                songSaveBtn.style.display = 'inline-block';
            };
            const slice = selectedSongFile.slice(0, 65536);
            reader.readAsArrayBuffer(slice);
        } else {
            selectedSongFile = null;
            songFileName.textContent = 'No file chosen';
            if (!fullUser?.songUrl) {
                songMetadataContainer.style.display = 'none';
                songSaveBtn.style.display = 'none';
            }
        }
    });

    songSaveBtn.addEventListener('click', async () => {
        const title = songTitleInput.value.trim();
        const artist = songArtistInput.value.trim();
        const fileToUpload = selectedSongFile || songFileInput.files?.[0];

        resetMessages();
        songSaveBtn.disabled = true;
        songSaveBtn.textContent = 'Saving...';

        try {
            await store.updateProfileSong({
                file: fileToUpload,
                title: title,
                artist: artist,
                url: fullUser?.songUrl || ''
            });

            songSuccess.textContent = 'Profile song saved!';
            songSuccess.style.display = 'block';
            selectedSongFile = null;
            songFileInput.value = '';
            songFileName.textContent = 'No file chosen';
            songRemoveBtn.style.display = 'inline-block';
            await store.fetchUsers();
        } catch (error) {
            songError.textContent = error.message || "Failed to save profile song";
            songError.style.display = 'block';
        } finally {
            songSaveBtn.textContent = 'Save Song';
            songSaveBtn.disabled = false;
        }
    });

    songRemoveBtn.addEventListener('click', async () => {
        resetMessages();
        songRemoveBtn.disabled = true;
        try {
            await store.updateProfileSong({ file: null, title: '', artist: '', url: '' });
            songSuccess.textContent = 'Profile song removed!';
            songSuccess.style.display = 'block';
            songTitleInput.value = '';
            songArtistInput.value = '';
            songMetadataContainer.style.display = 'none';
            songSaveBtn.style.display = 'none';
            songRemoveBtn.style.display = 'none';
            await store.fetchUsers();
        } catch (error) {
            songError.textContent = error.message || "Failed to remove profile song";
            songError.style.display = 'block';
        } finally {
            songRemoveBtn.disabled = false;
        }
    });

    // Password Reset Logic
    passwordResetBtn.addEventListener('click', async () => {
        if (!confirm("Are you sure you want to reset your password? You will be logged out immediately.")) {
            return;
        }

        resetMessages();
        passwordResetBtn.disabled = true;
        passwordResetBtn.textContent = 'Resetting...';

        try {
            const data = await store.resetPassword();
            const link = data.setupLink || data.setupUrl || '';
            const fullLink = link.startsWith('http') ? link : (window.location.origin + link);
            passwordResetBtn.style.display = 'none';
            passwordResetSuccess.style.display = 'block';
            passwordResetLink.href = fullLink;
            passwordResetLink.textContent = fullLink;
        } catch (error) {
            passwordResetError.textContent = error.message || "Failed to reset password";
            passwordResetError.style.display = 'block';
            passwordResetBtn.disabled = false;
            passwordResetBtn.textContent = 'Reset Password';
        }
    });

    logoutAfterResetBtn.addEventListener('click', () => {
        window.location.href = '/login.html';
    });

    // Passkeys Logic
    async function loadPasskeys() {
        try {
            const passkeys = await store.getPasskeys();
            passkeysList.innerHTML = '';
            if (!passkeys || passkeys.length === 0) {
                passkeysList.innerHTML = '<p class="text-muted">No passkeys registered.</p>';
                return;
            }

            passkeys.forEach(pk => {
                const item = document.createElement('div');
                item.className = 'passkey-item';

                const nameSpan = document.createElement('span');
                nameSpan.textContent = pk.name || 'Unnamed Passkey';

                const delBtn = document.createElement('button');
                delBtn.className = 'btn btn-danger btn-sm';
                delBtn.textContent = 'Remove';
                delBtn.onclick = async () => {
                    if (confirm('Remove this passkey?')) {
                        try {
                            await store.deletePasskey(pk.id);
                            loadPasskeys();
                        } catch (e) {
                            passkeyError.textContent = e.message;
                            passkeyError.style.display = 'block';
                        }
                    }
                };

                item.appendChild(nameSpan);
                item.appendChild(delBtn);
                passkeysList.appendChild(item);
            });
        } catch (e) {
            console.error('Failed to load passkeys:', e);
        }
    }

    if (window.PublicKeyCredential) {
        loadPasskeys();
        passkeyRegisterBtn.addEventListener('click', async () => {
            resetMessages();
            try {
                const options = await store.beginPasskeyRegistration();
                options.publicKey.challenge = base64URLToBuffer(options.publicKey.challenge);
                options.publicKey.user.id = base64URLToBuffer(options.publicKey.user.id);
                if (options.publicKey.excludeCredentials) {
                    options.publicKey.excludeCredentials.forEach(c => {
                        c.id = base64URLToBuffer(c.id);
                    });
                }

                const cred = await navigator.credentials.create({ publicKey: options.publicKey });
                const attestationObj = {
                    id: cred.id,
                    rawId: bufferToBase64URL(cred.rawId),
                    type: cred.type,
                    response: {
                        attestationObject: bufferToBase64URL(cred.response.attestationObject),
                        clientDataJSON: bufferToBase64URL(cred.response.clientDataJSON)
                    }
                };

                const passkeyName = prompt('Enter a name for this passkey:', 'My Passkey') || 'My Passkey';
                await store.finishPasskeyRegistration(passkeyName, attestationObj);

                passkeySuccess.textContent = 'Passkey registered successfully!';
                passkeySuccess.style.display = 'block';
                loadPasskeys();
            } catch (e) {
                console.error('Passkey registration error:', e);
                passkeyError.textContent = e.message || 'Registration failed';
                passkeyError.style.display = 'block';
            }
        });
    } else {
        const passkeySection = passkeysList.closest('.profile-section');
        if (passkeySection) {
            passkeySection.style.display = 'none';
        }
    }
}

// Alias for backwards compatibility
export const createProfileModal = createProfileSettingsModal;
