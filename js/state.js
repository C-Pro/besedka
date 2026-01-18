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
            return false;
        } catch {
            return false;
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
                    if (json && json.message) {
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
            const users = await response.json();
            this.setState({ users });
        } catch (error) {
            console.error('Fetch users error:', error);
        }
    }

    async fetchChats() {
        try {
            const response = await fetch('/api/chats');
            const chats = await response.json();
            this.setState({ chats });
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
            // Reconnect after delay
            setTimeout(() => this.connectWebSocket(), 3000);
        };
    }

    handleServerMessage(msg) {
        switch (msg.type) {
            case 'presence':
                this.handlePresenceUpdate(msg);
                break;
            case 'messages':
                this.handleNewMessages(msg);
                break;
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
        for (const m of msg.messages) {
            if (m.seq > maxSeq) {
                newMessages.push({
                    id: m.timestamp + m.userId + m.seq, // unique id
                    seq: m.seq,
                    text: m.content,
                    sender: m.userId === this.state.currentUser?.id ? 'me' : m.userId,
                    timestamp: new Date(m.timestamp * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
                    userId: m.userId
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

    sendMessage(chatId, text) {
        this.sendWebSocketMessage({
            type: 'send',
            chatId,
            content: text
        });
    }
}

export const store = new Store();
