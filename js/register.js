/* global QRCode */
import { store } from './state.js';

document.addEventListener('DOMContentLoaded', () => {
    const errorDiv = document.getElementById('error-message');
    const registerForm = document.getElementById('register-form');
    const successView = document.getElementById('success-view');

    // Form Inputs
    const usernameInput = document.getElementById('username');
    const currentPasswordInput = document.getElementById('current-password');
    const newPasswordInput = document.getElementById('new-password');

    // Buttons
    const registerBtn = document.getElementById('register-btn');
    const backBtn = document.getElementById('back-btn');
    const successBackBtn = document.getElementById('success-back-btn');

    // Display Areas
    const qrcodeDiv = document.getElementById('qrcode');
    const secretDisplay = document.getElementById('secret-display');

    // Get username from URL params
    const urlParams = new URLSearchParams(window.location.search);
    let username = urlParams.get('username') || '';

    // Set initial value
    if (username) {
        usernameInput.value = username;
    }

    let currentPassword = '';
    let newPassword = '';

    const showError = (msg) => {
        errorDiv.textContent = msg;
        errorDiv.style.display = 'block';
    };

    const hideError = () => {
        errorDiv.style.display = 'none';
        errorDiv.textContent = '';
    };

    // Auto-focus username
    if (usernameInput) {
        usernameInput.focus();
    }

    usernameInput.addEventListener('input', (e) => username = e.target.value);
    currentPasswordInput.addEventListener('input', (e) => currentPassword = e.target.value);
    newPasswordInput.addEventListener('input', (e) => newPassword = e.target.value);

    // Back Buttons
    const goBack = () => {
        window.location.href = 'login.html';
    };
    backBtn.addEventListener('click', goBack);
    successBackBtn.addEventListener('click', goBack);

    const handleRegister = async () => {
        hideError();

        if (!username || !currentPassword || !newPassword) {
            showError('All fields are required');
            return;
        }

        try {
            const result = await store.register(username, currentPassword, newPassword);
            if (result.success) {
                // Show Success View
                registerForm.style.display = 'none';
                successView.style.display = 'block';

                // Show Secret
                secretDisplay.textContent = result.totpSecret;

                // Generate QR Code
                // The OTP Auth URL format: otpauth://totp/Label?secret=SECRET&issuer=Issuer
                const label = `Besedka:${username}`;
                const issuer = 'Besedka';
                const otpAuthUrl = `otpauth://totp/${encodeURIComponent(label)}?secret=${result.totpSecret}&issuer=${encodeURIComponent(issuer)}`;

                // Clear previous if any
                qrcodeDiv.innerHTML = '';
                new QRCode(qrcodeDiv, {
                    text: otpAuthUrl,
                    width: 256,
                    height: 256
                });

            } else {
                showError(result.message || 'Setup failed');
            }
        } catch (err) {
            showError(err.message || 'An error occurred');
        }
    };

    registerBtn.addEventListener('click', handleRegister);

    // Allow Enter key to submit
    const inputs = [usernameInput, currentPasswordInput, newPasswordInput];
    inputs.forEach(input => {
        input.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') handleRegister();
        });
    });
});
