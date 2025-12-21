import { store } from '../state.js';

export function createLogin(container) {
    let mode = 'login'; // 'login' or 'setup'
    let username = '';
    let password = '';
    let otp = '';
    let setupPassword = '';
    let setupSecret = '';
    let error = '';

    const render = () => {
        if (mode === 'login') {
            container.innerHTML = `
                <div class="login-container">
                    <div class="login-card">
                        <h2>Login</h2>
                        ${error ? `<div class="error-message">${error}</div>` : ''}
                        <input type="text" id="username" placeholder="Username" value="${username}">
                        <input type="password" id="password" placeholder="Password" value="${password}">
                        <input type="text" id="otp" placeholder="OTP (6 digits, or leave empty if first time)" value="${otp}">
                        <button id="login-btn">Login</button>
                        <p class="hint">Stub users: Alice, Bob, Charlie (pass: password)</p>
                    </div>
                </div>
            `;
        } else {
            container.innerHTML = `
                <div class="login-container">
                    <div class="login-card">
                        <h2>Complete Setup</h2>
                         ${error ? `<div class="error-message">${error}</div>` : ''}
                        <p class="info-message">Please set a new password and configure 2FA.</p>
                        <input type="password" id="setup-pass" placeholder="New Password" value="${setupPassword}">
                        <input type="text" id="setup-secret" placeholder="TOTP Secret (e.g. 20 chars)" value="${setupSecret}">
                        <button id="setup-btn">Complete Setup</button>
                    </div>
                </div>
            `;
        }
        bindEvents();
    };

    const bindEvents = () => {
        if (mode === 'login') {
            const userIn = container.querySelector('#username');
            const passIn = container.querySelector('#password');
            const otpIn = container.querySelector('#otp');
            const btn = container.querySelector('#login-btn');

            userIn.addEventListener('input', e => username = e.target.value);
            passIn.addEventListener('input', e => password = e.target.value);
            otpIn.addEventListener('input', e => otp = e.target.value);

            const handleLogin = async () => {
                error = '';
                render();

                // treat empty otp as 0
                const otpVal = otp ? parseInt(otp) : 0;

                const result = await store.login(username, password, otpVal);
                if (result.success) {
                    // Success is handled by state change/app component
                } else if (result.needRegister) {
                    mode = 'setup';
                    error = 'First login requires setup.';
                    render();
                } else {
                    error = result.message || 'Login failed';
                    render();
                }
            };

            btn.addEventListener('click', handleLogin);
        } else {
            const passIn = container.querySelector('#setup-pass');
            const secretIn = container.querySelector('#setup-secret');
            const btn = container.querySelector('#setup-btn');

            passIn.addEventListener('input', e => setupPassword = e.target.value);
            secretIn.addEventListener('input', e => setupSecret = e.target.value);

            const handleSetup = async () => {
                error = '';
                const success = await store.register(username, setupPassword, setupSecret);
                if (success) {
                    mode = 'login';
                    error = '';
                    password = '';
                    otp = '';
                    username = username; // keep
                    alert('Setup complete. Please login with new password and OTP.');
                    render();
                } else {
                    error = 'Setup failed';
                    render();
                }
            };

            btn.addEventListener('click', handleSetup);
        }
    };

    render();
}
