import { store } from '../state.js';

export function createLogin(container) {
    let mode = 'login'; // 'login', 'setup', 'setup_success'
    let username = '';
    let password = '';
    let otp = '';
    let setupPassword = '';
    let totpSecret = '';
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
        } else if (mode === 'setup') {
            container.innerHTML = `
                <div class="login-container">
                    <div class="login-card">
                        <h2>Complete Setup</h2>
                         ${error ? `<div class="error-message">${error}</div>` : ''}
                        <p class="info-message">Please set a new password.</p>
                        <input type="password" id="setup-pass" placeholder="New Password" value="${setupPassword}">
                        <button id="setup-btn">Complete Setup</button>
                    </div>
                </div>
            `;
        } else {
            // setup_success
            container.innerHTML = `
                <div class="login-container">
                    <div class="login-card">
                        <h2>Setup Complete</h2>
                        <p class="info-message">Registration successful. Please add this secret to your Authenticator App:</p>
                        <div style="background: #f0f0f0; padding: 10px; margin: 10px 0; border-radius: 4px; font-family: monospace; font-weight: bold; text-align: center; color: #333;">${totpSecret}</div>
                        <p class="hint">Then return to login and use your new password and the generated code.</p>
                        <button id="back-btn">Back to Login</button>
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
        } else if (mode === 'setup') {
            const passIn = container.querySelector('#setup-pass');
            const btn = container.querySelector('#setup-btn');

            passIn.addEventListener('input', e => setupPassword = e.target.value);

            const handleSetup = async () => {
                error = '';
                const result = await store.register(username, password, setupPassword);
                if (result.success) {
                    mode = 'setup_success';
                    totpSecret = result.totpSecret;
                    error = '';
                    render();
                } else {
                    error = result.message || 'Setup failed';
                    render();
                }
            };

            btn.addEventListener('click', handleSetup);
        } else {
            const btn = container.querySelector('#back-btn');
            btn.addEventListener('click', () => {
                mode = 'login';
                password = ''; // Clear old password
                otp = '';
                error = '';
                render();
            });
        }
    };

    render();
}
