import { store, bufferToBase64URL, base64URLToBuffer } from './state.js';

const errorDiv = document.getElementById('error-message');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const otpInput = document.getElementById('otp');
const passkeyLoginBtn = document.getElementById('passkey-login-btn');

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
            window.location.replace('/');
        } else if (result.needRegister) {
            // Redirect to registration page with username pre-filled
            window.location.replace(`register.html?username=${encodeURIComponent(currentUsername)}`);
        } else {
            showError(result.message || 'Login failed');
        }
    } catch (err) {
        showError(err.message || 'An error occurred');
    }
};

const handlePasskeyLogin = async () => {
    hideError();
    passkeyLoginBtn.disabled = true;
    try {
        const options = await store.beginPasskeyLogin();
        
        // decode challenge
        options.publicKey.challenge = base64URLToBuffer(options.publicKey.challenge);
        if (options.publicKey.allowCredentials) {
            for (let c of options.publicKey.allowCredentials) {
                c.id = base64URLToBuffer(c.id);
            }
        }
        
        const credential = await navigator.credentials.get(options);
        
        const assertionObj = {
            id: credential.id,
            rawId: bufferToBase64URL(credential.rawId),
            type: credential.type,
            response: {
                authenticatorData: bufferToBase64URL(credential.response.authenticatorData),
                clientDataJSON: bufferToBase64URL(credential.response.clientDataJSON),
                signature: bufferToBase64URL(credential.response.signature),
                userHandle: credential.response.userHandle ? bufferToBase64URL(credential.response.userHandle) : null
            }
        };
        
        const result = await store.finishPasskeyLogin(assertionObj);
        if (result?.success) {
            window.location.replace('/');
        } else {
            showError(result.message || 'Passkey login failed');
        }
    } catch (err) {
        console.error(err);
        showError(err.message || 'Passkey login failed');
    } finally {
        passkeyLoginBtn.disabled = false;
    }
};

const loginForm = document.getElementById('login-form');
if (loginForm) {
    loginForm.addEventListener('submit', handleLogin);
}

if (passkeyLoginBtn) {
    if (window.PublicKeyCredential) {
        passkeyLoginBtn.addEventListener('click', handlePasskeyLogin);
    } else {
        passkeyLoginBtn.style.display = 'none';
    }
}
