export function createProfileModal(store) {
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
            <button class="modal-close-btn" id="profile-modal-close" aria-label="Close">&times;</button>
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

    if (currentUser) {
        displayNameInput.value = currentUser.name || '';

        // Find full user object to get avatarUrl
        const fullUser = store.state.users.find(u => u.id === currentUser.id);
        if (fullUser?.avatarUrl) {
            avatarPreview.innerHTML = `<img src="${fullUser.avatarUrl}" alt="Avatar" class="avatar-image-full">`;
        } else {
            avatarPreview.innerHTML = `<div class="avatar-placeholder">${(currentUser.name || '?').charAt(0).toUpperCase()}</div>`;
        }
    }

    // --- References ---
    const closeBtn = document.getElementById('profile-modal-close');
    const bgOverlay = document.getElementById('profile-modal-overlay');

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

    // Password Reset
    const passwordResetBtn = document.getElementById('password-reset-btn');
    const passwordResetError = document.getElementById('password-reset-error');
    const passwordResetSuccess = document.getElementById('password-reset-success');
    const passwordResetLink = document.getElementById('password-reset-link');
    const logoutAfterResetBtn = document.getElementById('logout-after-reset-btn');

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
        passwordResetError.style.display = 'none';
        // intentional: don't hide password reset success if it's there
    };

    // --- Event Listeners ---
    closeBtn.addEventListener('click', closeModal);
    bgOverlay.addEventListener('click', (e) => {
        if (e.target === bgOverlay) closeModal();
    });
    document.addEventListener('keydown', handleEsc);

    // Avatar Upload Logic
    avatarInput.addEventListener('change', (e) => {
        resetMessages();
        if (e.target.files?.[0]) {
            selectedAvatarFile = e.target.files[0];

            // Client-side validation: size <= 5MB
            if (selectedAvatarFile.size > 5 * 1024 * 1024) {
                avatarError.textContent = "File must be smaller than 5MB";
                avatarError.style.display = 'block';
                avatarSaveBtn.disabled = true;
                return;
            }

            // Preview
            const reader = new FileReader();
            reader.onload = (re) => {
                avatarPreview.innerHTML = `<img src="${re.target.result}" alt="Preview" class="avatar-image-full">`;
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
            // Refresh users
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
            // Update local state with server-returned display name (if any) and re-fetch users so avatar placeholder is correct
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

    // Password Reset Logic
    passwordResetBtn.addEventListener('click', async () => {
        if (!confirm("Are you sure you want to reset your password? This will log out all your sessions.")) {
            return;
        }

        resetMessages();
        passwordResetBtn.disabled = true;
        passwordResetBtn.textContent = 'Resetting...';

        try {
            const result = await store.resetPassword();

            // Show setup link
            passwordResetSuccess.style.display = 'block';
            passwordResetLink.href = result.setupLink;
            passwordResetLink.textContent = window.location.origin + result.setupLink;

            // Hide the button to prevent double-clicks
            passwordResetBtn.style.display = 'none';

            // After a successful password reset, the server clears auth cookies and
            // disconnects websockets. Ensure the client no longer behaves as authenticated.
            if (store) {
                if (typeof store.logout === 'function') {
                    // Prefer a dedicated logout flow if available.
                    await store.logout();
                } else {
                    // Fallback: clear local user state and disconnect websockets if supported.
                    if (store.state && Object.hasOwn(store.state, 'currentUser')) {
                        store.state.currentUser = null;
                    }
                    if (typeof store.disconnectWebSocket === 'function') {
                        store.disconnectWebSocket();
                    }
                }
            }
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
}
