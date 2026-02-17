import { store } from './state.js';

document.addEventListener('DOMContentLoaded', () => {
    const errorDiv = document.getElementById('error-message');
    const usernameInput = document.getElementById('username');
    const passwordInput = document.getElementById('password');
    const otpInput = document.getElementById('otp');
    const loginBtn = document.getElementById('login-btn');

    let username = '';
    let password = '';
    let otp = '';

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

    usernameInput.addEventListener('input', (e) => username = e.target.value);
    passwordInput.addEventListener('input', (e) => password = e.target.value);
    otpInput.addEventListener('input', (e) => otp = e.target.value);

    const handleLogin = async (e) => {
        if (e) e.preventDefault();
        hideError();
        const otpVal = otp ? parseInt(otp, 10) : 0;

        try {
            const result = await store.login(username, password, otpVal);
            if (result.success) {
                window.location.href = '/';
            } else if (result.needRegister) {
                // Redirect to registration page with username pre-filled
                window.location.href = `register.html?username=${encodeURIComponent(username)}`;
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
});
