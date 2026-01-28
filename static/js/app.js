import { store } from './state.js';
import { createChatList } from './components/ChatList.js';
import { createChatWindow } from './components/ChatWindow.js';
import { createInfoPanel } from './components/InfoPanel.js';

const app = document.getElementById('app');

function renderApp() {
    // Create layout structure
    app.innerHTML = `
        <div class="app-layout">
            <div class="mobile-header">
                <button class="hamburger-btn" id="hamburger-btn">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <line x1="3" y1="12" x2="21" y2="12"></line>
                        <line x1="3" y1="6" x2="21" y2="6"></line>
                        <line x1="3" y1="18" x2="21" y2="18"></line>
                    </svg>
                </button>
                <div class="mobile-title"></div>
            </div>
            
            <div class="mobile-menu-overlay" id="mobile-menu-overlay"></div>
            <div class="mobile-menu" id="mobile-menu">
                <div class="mobile-menu-item" data-tab="chat-list">Chats</div>
                <div class="mobile-menu-item" data-tab="chat-window">Message</div>
                <div class="mobile-menu-item" data-tab="info-panel">Info</div>
            </div>

            <div id="sidebar" class="sidebar"></div>
            <div id="chat-area" class="chat-area"></div>
            <div id="info-panel" class="info-panel"></div>
        </div>
    `;

    // Initialize components
    createChatList(document.getElementById('sidebar'));
    createChatWindow(document.getElementById('chat-area'));
    createInfoPanel(document.getElementById('info-panel'));

    // Handle responsive visibility
    const handleVisibility = (state) => {
        const sidebar = document.getElementById('sidebar');
        // Check if elements exist
        if (!sidebar) return;

        const chatArea = document.getElementById('chat-area');
        const infoPanel = document.getElementById('info-panel');
        const mobileMenu = document.getElementById('mobile-menu');
        const overlay = document.getElementById('mobile-menu-overlay');
        const menuItems = document.querySelectorAll('.mobile-menu-item');

        // Reset classes
        sidebar.classList.remove('active');
        chatArea.classList.remove('active');
        infoPanel.classList.remove('active');
        menuItems.forEach(item => item.classList.remove('active'));

        // Close menu
        mobileMenu.classList.remove('open');
        overlay.classList.remove('open');

        // Update mobile title
        const mobileTitle = document.querySelector('.mobile-title');
        if (state.mobileActiveTab === 'chat-list') {
            mobileTitle.textContent = 'Chats';
        } else if (state.mobileActiveTab === 'chat-window') {
            const activeChat = state.chats.find(c => c.id === state.activeChatId);
            mobileTitle.textContent = activeChat ? activeChat.name : 'Message';
        } else if (state.mobileActiveTab === 'info-panel') {
            mobileTitle.textContent = 'Info';
        }

        // Apply active state based on mobile tab
        if (window.innerWidth <= 768) {
            if (state.mobileActiveTab === 'chat-list') {
                sidebar.classList.add('active');
                menuItems[0].classList.add('active');
            } else if (state.mobileActiveTab === 'chat-window') {
                chatArea.classList.add('active');
                menuItems[1].classList.add('active');
            } else if (state.mobileActiveTab === 'info-panel') {
                infoPanel.classList.add('active');
                menuItems[2].classList.add('active');
            }
        } else {
            // Desktop: always show all
            sidebar.classList.add('active');
            chatArea.classList.add('active');
            infoPanel.classList.add('active');
        }
    };

    // Mobile menu listeners
    const hamburgerBtn = document.getElementById('hamburger-btn');
    const mobileMenu = document.getElementById('mobile-menu');
    const overlay = document.getElementById('mobile-menu-overlay');

    const toggleMenu = () => {
        mobileMenu.classList.toggle('open');
        overlay.classList.toggle('open');
    };

    hamburgerBtn.addEventListener('click', toggleMenu);
    overlay.addEventListener('click', toggleMenu);

    document.querySelectorAll('.mobile-menu-item').forEach(item => {
        item.addEventListener('click', () => {
            store.setMobileTab(item.dataset.tab);
            toggleMenu();
        });
    });

    // Swipe Navigation
    let touchStartX = 0;
    let touchEndX = 0;
    const minSwipeDistance = 50;

    const onTouchStart = (e) => {
        touchStartX = e.changedTouches[0].screenX;
    };

    const onTouchEnd = (e) => {
        touchEndX = e.changedTouches[0].screenX;
        handleSwipe();
    };

    document.addEventListener('touchstart', onTouchStart);
    document.addEventListener('touchend', onTouchEnd);

    const handleSwipe = () => {
        const distance = touchEndX - touchStartX;
        if (Math.abs(distance) < minSwipeDistance) return;

        const currentTab = store.state.mobileActiveTab;

        if (distance > 0) { // Swipe Right
            if (currentTab === 'chat-window') store.setMobileTab('chat-list');
            else if (currentTab === 'info-panel') store.setMobileTab('chat-window');
        } else { // Swipe Left
            if (currentTab === 'chat-list') store.setMobileTab('chat-window');
            else if (currentTab === 'chat-window') store.setMobileTab('info-panel');
        }
    };

    // Initial check
    handleVisibility(store.state);

    // Subscribe to state changes
    store.subscribe(handleVisibility);

    // Resize listener
    window.addEventListener('resize', () => {
        handleVisibility(store.state);
    });
}

function init() {
    // Check authentication logic is primarily handled by the server (sending back 401 on data fetch).
    // However, we can try to fetch the initial data. If it fails with 401, redirect to login.

    // We can add a simple check method to store to verify session validity
    store.checkSession().then(isValid => {
        if (!isValid) {
            window.location.href = '/login.html';
        } else {
            renderApp();
            store.fetchUsers();
            store.fetchChats();
            store.connectWebSocket();
        }
    });
}

init();
