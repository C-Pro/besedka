import { store } from './state.js';

const errorDiv = document.getElementById('error-message');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const otpInput = document.getElementById('otp');

const showError = (msg) => {
    errorDiv.textContent = msg;
    errorDiv.style.display = 'block';
};

// Auto-focus username
if (usernameInput) {
    usernameInput.focus();
}

const hideError = () => {
    errorDiv.style.display = 'none';
    errorDiv.textContent = '';
};

const handleLogin = async (e) => {
    if (e) e.preventDefault();
    hideError();

    // Read directly from DOM to support password-managers auto-fill without input events
    const currentUsername = usernameInput ? usernameInput.value : '';
    const currentPassword = passwordInput ? passwordInput.value : '';
    const currentOtp = otpInput ? otpInput.value : '';
    const otpVal = currentOtp ? parseInt(currentOtp, 10) : 0;

    try {
        const result = await store.login(currentUsername, currentPassword, otpVal);
        if (result.success) {
            window.location.href = '/';
        } else if (result.needRegister) {
            // Redirect to registration page with username pre-filled
            window.location.href = `register.html?username=${encodeURIComponent(currentUsername)}`;
        } else {
            showError(result.message || 'Login failed');
        }
    } catch (err) {
        showError(err.message || 'An error occurred');
    }
};

const loginForm = document.getElementById('login-form');
if (loginForm) {
    loginForm.addEventListener('submit', handleLogin);
}
