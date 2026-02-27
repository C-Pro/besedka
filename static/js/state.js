// Simple State Management
class Store {
    constructor() {
        this.state = {
            currentUser: null,
            activeChatId: null,
            viewMode: 'desktop', // 'desktop' or 'mobile'
            mobileActiveTab: 'chat-list', // 'chat-list', 'chat-window', 'info-panel'
            chats: [],
            users: [],
            messages: {} // chatId -> [messages]
        };
        this.listeners = [];
        this.socket = null;
        this.reconnectAttempts = 0;
        this.isReconnecting = false;
    }

    subscribe(listener) {
        this.listeners.push(listener);
        return () => {
            this.listeners = this.listeners.filter(l => l !== listener);
        };
    }

    setState(newState) {
        this.state = { ...this.state, ...newState };
        this.notify();
    }

    notify() {
        this.listeners.forEach(listener => listener(this.state));
    }

    async checkSession() {
        try {
            // We use /api/me to check session and get current user info at the same time
            const response = await fetch('/api/me');
            if (response.status === 401) return false;
            if (response.ok) {
                const user = await response.json();
                this.setState({ currentUser: user });
                return true;
            }
            return true;
        } catch {
            return true;
        }
    }

    // API Actions
    // API Actions
    async login(username, password, otp = 0) {
        try {
            const body = `username=${encodeURIComponent(username)}&password=${encodeURIComponent(password)}&totp=${otp}`;
            const response = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: body
            });

            if (response.status === 401) {
                const text = await response.text();
                // Check if it's a NeedRegister case
                if (text.includes("First login")) {
                    return { success: false, needRegister: true, message: text };
                }

                let errorMessage = text;
                try {
                    const json = JSON.parse(text);
                    if (json?.message) {
                        errorMessage = json.message;
                    }
                } catch {
                    // Not a JSON response, use raw text
                }

                return { success: false, message: errorMessage.replace(/\n/g, '') };
            }

            if (!response.ok) throw new Error('Login failed');

            const data = await response.json();
            this.setState({ currentUser: { id: data.userId } });

            await this.fetchUsers();
            await this.fetchChats();
            this.connectWebSocket();

            return { success: true };
        } catch (error) {
            console.error('Login error:', error);
            return { success: false, message: error.message };
        }
    }

    async logoff() {
        try {
            const response = await fetch('/api/logoff', { method: 'POST' });
            if (response.ok) {
                // Clear state
                this.setState({
                    currentUser: null,
                    chats: [],
                    users: [],
                    messages: {},
                    activeChatId: null
                });

                // Close websocket if open
                if (this.socket) {
                    this.socket.close();
                    this.socket = null;
                }

                window.location.href = '/login.html';
            } else {
                console.error('Logoff failed', await response.text());
            }
        } catch (error) {
            console.error('Logoff error:', error);
        }
    }

    async uploadImage(file) {
        try {
            const formData = new FormData();
            formData.append('file', file);

            const response = await fetch('/api/upload/image', {
                method: 'POST',
                body: file
            });

            if (!response.ok) {
                throw new Error('Upload failed');
            }

            return await response.json();
        } catch (error) {
            console.error('Upload error:', error);
            throw error;
        }
    }

    async register(username, oldPassword, newPassword) {
        try {
            const response = await fetch('/api/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password: oldPassword, newPassword })
            });

            const data = await response.json();

            if (!response.ok || !data.success) {
                throw new Error(data.message || 'Registration failed');
            }
            return { success: true, totpSecret: data.totpSecret };
        } catch (error) {
            console.error('Registration error:', error);
            return { success: false, message: error.message };
        }
    }

    async fetchUsers() {
        try {
            const response = await fetch('/api/users');
            if (response.status === 401) {
                window.location.href = '/login.html';
                return;
            }
            const users = await response.json();
            this.setState({ users });
        } catch (error) {
            console.error('Fetch users error:', error);
        }
    }

    async fetchChats() {
        try {
            const response = await fetch('/api/chats');
            if (response.status === 401) {
                window.location.href = '/login.html';
                return;
            }
            const chats = await response.json();
            this.setState({ chats });

            // Set Townhall as active chat if none selected
            if (!this.state.activeChatId) {
                const townhall = chats.find(c => c.id === 'townhall');
                if (townhall) {
                    this.setActiveChat(townhall.id);
                }
            }
        } catch (error) {
            console.error('Fetch chats error:', error);
        }
    }

    connectWebSocket() {
        if (this.socket) return;

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        this.socket = new WebSocket(`${protocol}//${window.location.host}/api/chat`);

        this.socket.onopen = () => {
            console.log('WebSocket connected');
            this.reconnectAttempts = 0;

            // If this was a reconnection, refresh data
            if (this.isReconnecting) {
                this.fetchUsers();
                this.fetchChats();
                this.isReconnecting = false;
            }

            // Join active chat if any
            if (this.state.activeChatId) {
                this.sendWebSocketMessage({ type: 'join', chatId: this.state.activeChatId });
            }
        };

        this.socket.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            this.handleServerMessage(msg);
        };

        this.socket.onclose = () => {
            console.log('WebSocket disconnected');
            this.socket = null;
            this.isReconnecting = true;

            // Exponential backoff for reconnection (max 30s)
            const delay = Math.min(30000, 1000 * 2 ** this.reconnectAttempts);
            this.reconnectAttempts++;

            setTimeout(() => this.connectWebSocket(), delay);
        };
    }

    handleServerMessage(msg) {
        switch (msg.type) {
            case 'presence':
            case 'online':
            case 'offline':
                this.handlePresenceUpdate(msg);
                break;
            case 'messages':
                this.handleNewMessages(msg);
                break;
            case 'new':
                this.handleNewUser(msg);
                break;
            case 'deleted':
                this.handleUserDeleted(msg);
                break;
        }
    }

    handleNewUser(msg) {
        // Add new user if not exists
        if (!this.state.users.find(u => u.id === msg.user.id)) {
            this.setState({
                users: [...this.state.users, msg.user]
            });
        }

        // Add new chat if not exists
        if (msg.chat && !this.state.chats.find(c => c.id === msg.chat.id)) {
            this.setState({
                chats: [...this.state.chats, msg.chat]
            });
        }
    }

    handleUserDeleted(msg) {
        const userId = msg.userId;

        // Remove user
        this.setState({
            users: this.state.users.filter(u => u.id !== userId)
        });

        // Remove chat (DM with this user)
        // DMs have ID format "dm_u1_u2"
        this.setState({
            chats: this.state.chats.filter(c => {
                if (c.isDm && c.id.includes(userId)) {
                    return false;
                }
                return true;
            })
        });

        // If active chat was with this user, switch to townhall
        if (this.state.activeChatId?.includes(userId) && this.state.chats.find(c => c.id === this.state.activeChatId)?.isDm) {
            this.setActiveChat('townhall');
        }
    }

    handlePresenceUpdate(msg) {
        const users = this.state.users.map(u => {
            if (u.id === msg.userId) {
                return { ...u, presence: { ...u.presence, online: msg.online } };
            }
            return u;
        });

        // Also update chats if it's a DM
        const chats = this.state.chats.map(c => {
            if (c.isDm && c.id.includes(msg.userId)) { // Simplified check
                return { ...c, online: msg.online };
            }
            return c;
        });

        this.setState({ users, chats });
    }

    handleNewMessages(msg) {
        const chatId = msg.chatId;
        const currentMessages = this.state.messages[chatId] || [];

        // Find the maximum sequence number we currently have for this chat
        let maxSeq = -1;
        if (currentMessages.length > 0) {
            // Assuming messages are stored in order, but let's be safe
            for (const m of currentMessages) {
                if (m.seq > maxSeq) maxSeq = m.seq;
            }
        }

        // Filter out messages that we already have (seq <= maxSeq)
        // Note: m.seq comes from server and should be present.
        const newMessages = [];
        for (const m of (msg.messages || [])) {
            if (m.seq > maxSeq) {
                newMessages.push({
                    id: m.timestamp + m.userId + m.seq, // unique id
                    seq: m.seq,
                    text: m.content,
                    sender: m.userId === this.state.currentUser?.id ? 'me' : m.userId,
                    timestamp: (() => {
                        const d = new Date(m.timestamp * 1000);
                        const pad = (n) => n.toString().padStart(2, '0');
                        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
                    })(),
                    userId: m.userId,
                    attachments: m.attachments || []
                });
            }
        }

        if (newMessages.length === 0) return;

        // Sort new messages by seq just in case
        newMessages.sort((a, b) => a.seq - b.seq);

        this.setState({
            messages: {
                ...this.state.messages,
                [chatId]: [...currentMessages, ...newMessages]
            }
        });
    }

    // UI Actions
    setActiveChat(chatId) {
        if (this.state.activeChatId === chatId) return;

        // Leave previous chat
        if (this.state.activeChatId) {
            this.sendWebSocketMessage({ type: 'leave', chatId: this.state.activeChatId });
        }

        this.setState({
            activeChatId: chatId,
            mobileActiveTab: 'chat-window'
        });

        // Join new chat
        if (chatId) {
            this.sendWebSocketMessage({ type: 'join', chatId: chatId });
        }
    }

    sendWebSocketMessage(msg) {
        if (this.socket && this.socket.readyState === WebSocket.OPEN) {
            this.socket.send(JSON.stringify(msg));
        }
    }

    setMobileTab(tab) {
        this.setState({ mobileActiveTab: tab });
    }

    sendMessage(chatId, text, attachments = []) {
        this.sendWebSocketMessage({
            type: 'send',
            chatId,
            content: text,
            attachments
        });
    }
}

export const store = new Store();
