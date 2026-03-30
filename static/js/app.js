import { store } from './state.js';
import { createChatList } from './components/ChatList.js';
import { createChatWindow } from './components/ChatWindow.js';
import { createInfoPanel } from './components/InfoPanel.js';
import { createProfileModal } from './components/ProfileModal.js';

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
                <div class="profile-menu-container" id="mobile-profile-menu">
                    <button type="button" class="avatar profile-avatar" id="mobile-profile-avatar" aria-expanded="false" aria-controls="mobile-profile-dropdown" aria-haspopup="true" aria-label="Profile Menu">?</button>
                    <div class="profile-dropdown" id="mobile-profile-dropdown">
                        <button class="profile-menu-item" id="mobile-profile-btn" type="button">Profile</button>
                        <button class="profile-menu-item disabled" type="button" disabled aria-disabled="true">Settings</button>
                        <button class="profile-menu-item" id="mobile-logoff-btn" type="button">Log Off</button>
                    </div>
                </div>
            </div>

            <div class="mobile-menu-overlay" id="mobile-menu-overlay"></div>
            <div class="mobile-menu" id="mobile-menu">
                <div class="mobile-menu-item" data-tab="chat-list">Chats</div>
                <div class="mobile-menu-item" data-tab="chat-window">Messages</div>
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
    createInfoPanel(document.getElementById('info-panel'), store);

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
            mobileTitle.textContent = activeChat ? activeChat.name : 'Messages';
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

    // History Management
    let isHandlingPopState = false;

    const pushState = (tab, chatId) => {
        const state = { mobileActiveTab: tab, activeChatId: chatId };
        // Don't push if the state is the same as current history state
        if (history.state &&
            history.state.mobileActiveTab === tab &&
            history.state.activeChatId === chatId) return;

        history.pushState(state, '', '');
    };

    window.addEventListener('popstate', (event) => {
        isHandlingPopState = true;
        if (event.state) {
            const { mobileActiveTab, activeChatId } = event.state;

            // If chatId is different, use setActiveChat to trigger join/leave logic
            if (activeChatId !== store.state.activeChatId) {
                store.setActiveChat(activeChatId);
            }
            
            // Ensure mobileActiveTab is correctly synced from history
            if (mobileActiveTab !== store.state.mobileActiveTab) {
                store.setMobileTab(mobileActiveTab);
            }
        } else {
            // Default state
            store.setMobileTab('chat-list');
        }
        isHandlingPopState = false;
    });

    // Initialize initial history state if not present
    if (!history.state) {
        history.replaceState({
            mobileActiveTab: store.state.mobileActiveTab,
            activeChatId: store.state.activeChatId
        }, '', '');
    }

    document.querySelectorAll('.mobile-menu-item').forEach(item => {
        item.addEventListener('click', () => {
            store.setMobileTab(item.dataset.tab);
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
        let newTab = currentTab;

        if (distance > 0) { // Swipe Right
            if (currentTab === 'chat-window') newTab = 'chat-list';
            else if (currentTab === 'info-panel') newTab = 'chat-window';
        } else { // Swipe Left
            if (currentTab === 'chat-list') newTab = 'chat-window';
            else if (currentTab === 'chat-window') newTab = 'info-panel';
        }

        if (newTab !== currentTab) {
            store.setMobileTab(newTab);
        }
    };

    // Initial check
    handleVisibility(store.state);

    // Profile Menu Logic (Event Delegation)
    document.addEventListener('click', (e) => {
        // Handle Mobile Profile Avatar
        const mobileAvatar = e.target.closest('#mobile-profile-avatar');
        if (mobileAvatar) {
            e.stopPropagation();
            const dropdown = document.getElementById('mobile-profile-dropdown');
            const isOpen = dropdown?.classList.toggle('open');
            mobileAvatar.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
            return;
        }

        // Handle Desktop Profile Avatar
        const desktopAvatar = e.target.closest('#desktop-profile-avatar');
        if (desktopAvatar) {
            e.stopPropagation();
            const dropdown = document.getElementById('desktop-profile-dropdown');
            const isOpen = dropdown?.classList.toggle('open');
            desktopAvatar.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
            return;
        }

        // Handle Profile Edit Button
        if (e.target.closest('#mobile-profile-btn') || e.target.closest('#desktop-profile-btn')) {
            createProfileModal(store);

            // Close dropdowns
            document.getElementById('mobile-profile-dropdown')?.classList.remove('open');
            document.getElementById('desktop-profile-dropdown')?.classList.remove('open');
            document.getElementById('mobile-profile-avatar')?.setAttribute('aria-expanded', 'false');
            document.getElementById('desktop-profile-avatar')?.setAttribute('aria-expanded', 'false');
            return;
        }

        // Handle Logoff Buttons
        if (e.target.closest('#mobile-logoff-btn') || e.target.closest('#desktop-logoff-btn')) {
            store.logoff();
            return;
        }

        // Close dropdowns when clicking outside
        const mobileDropdown = document.getElementById('mobile-profile-dropdown');
        const desktopDropdown = document.getElementById('desktop-profile-dropdown');

        if (mobileDropdown && !mobileDropdown.contains(e.target)) {
            mobileDropdown.classList.remove('open');
            document.getElementById('mobile-profile-avatar')?.setAttribute('aria-expanded', 'false');
        }
        if (desktopDropdown && !desktopDropdown.contains(e.target)) {
            desktopDropdown.classList.remove('open');
            document.getElementById('desktop-profile-avatar')?.setAttribute('aria-expanded', 'false');
        }
    });

    // Subscribe to state changes to update profile initial
    const updateProfileIcons = (state) => {
        if (state.currentUser?.name || state.currentUser?.id) {
            const initial = (state.currentUser.name || '?').charAt(0).toUpperCase();

            let fullUser = null;
            if (state.currentUser.id && state.users) {
                fullUser = state.users.find(u => u.id === state.currentUser.id);
            }
            const avatarUrl = fullUser?.avatarUrl || state.currentUser.avatarUrl;

            const updateAvatarNode = (avatarNode) => {
                if (!avatarNode) return;
                avatarNode.innerHTML = '';
                if (avatarUrl) {
                    avatarNode.style.padding = '0';
                    const img = document.createElement('img');
                    img.src = avatarUrl;
                    img.alt = 'Profile';
                    img.style.width = '100%';
                    img.style.height = '100%';
                    img.style.borderRadius = '50%';
                    img.style.objectFit = 'cover';
                    avatarNode.appendChild(img);
                } else {
                    avatarNode.textContent = initial;
                }
            };

            updateAvatarNode(document.getElementById('mobile-profile-avatar'));
            updateAvatarNode(document.getElementById('desktop-profile-avatar'));
        }
    };

    // Subscribe to state changes
    store.subscribe(state => {
        handleVisibility(state);
        updateProfileIcons(state);

        // Sync history if not currently handling a popstate event
        if (!isHandlingPopState) {
            pushState(state.mobileActiveTab, state.activeChatId);
        }
    });

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

            // Setup periodic session check (every 5 minutes)
            setInterval(() => {
                store.checkSession().then(valid => {
                    if (!valid) {
                        window.location.href = '/login.html';
                    }
                });
            }, 300000); // 5 minutes
        }
    });
}

init();
