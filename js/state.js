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

    // API Actions
    async login(username, password) {
        try {
            const response = await fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: `username=${encodeURIComponent(username)}&password=${encodeURIComponent(password)}`
            });

            if (!response.ok) throw new Error('Login failed');

            const data = await response.json();
            this.setState({ currentUser: { id: data.userId } });

            await this.fetchUsers();
            await this.fetchChats();
            this.connectWebSocket();

            return true;
        } catch (error) {
            console.error('Login error:', error);
            return false;
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
        const newMessages = msg.messages.map(m => ({
            id: m.timestamp + m.userId, // simple unique id
            text: m.content,
            sender: m.userId === this.state.currentUser?.id ? 'me' : m.userId, // We'll resolve names in UI
            timestamp: new Date(m.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
            userId: m.userId
        }));

        const currentMessages = this.state.messages[chatId] || [];

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
