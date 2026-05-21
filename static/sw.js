const CACHE_VERSION = '{{CACHE_VERSION}}';
const CACHE_FILES = [
    '/',
    '/js/app.js',
    '/js/state.js',
    '/js/components/ChatList.js',
    '/js/components/ChatWindow.js',
    '/js/components/InfoPanel.js',
    '/js/components/ProfileModal.js',
    '/js/d3.min.js',
    '/css/style.css',
    '/css/layout.css',
    '/css/components.css',
    '/besedka.png',
    '/favicon-32x32.png',
    '/favicon.ico',
    '/site.webmanifest',
    '/world.geojson',
];

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_VERSION)
            .then((cache) => cache.addAll(CACHE_FILES))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys()
            .then((cacheNames) => Promise.all(
                cacheNames
                    .filter((name) => name !== CACHE_VERSION)
                    .map((name) => caches.delete(name))
            ))
            .then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', (event) => {
    if (event.request.method !== 'GET') return;

    const url = new URL(event.request.url);

    // Never cache API or WebSocket requests
    if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws')) return;

    event.respondWith(
        caches.match(event.request).then((cached) => cached || fetch(event.request))
    );
});

async function syncBadge() {
    if (navigator.setAppBadge) {
        try {
            const notifications = await self.registration.getNotifications();
            if (notifications.length > 0) {
                await navigator.setAppBadge(notifications.length);
            } else {
                await navigator.clearAppBadge();
            }
        } catch (e) {
            console.error('Failed to sync app badge:', e);
        }
    }
}

self.addEventListener('push', function (event) {
    console.log('Push message received:', event);
    let data = {};
    try {
        if (event.data) {
            data = event.data.json();
        }
    } catch (e) {
        console.warn('Push payload was not JSON:', event.data.text());
        data = {
            title: '{{CHATNAME}}',
            body: event.data.text()
        };
    }

    console.log('Push payload:', data);
    const options = {
        body: data.body || 'New message received',
        icon: '/besedka.png',
        badge: '/favicon-32x32.png',
        vibrate: [100, 50, 100],
        tag: data.url || 'besedka-notification', // Replace notifications with same tag
        renotify: true, // Vibrate even if replaced
        actions: [
            { action: 'reply', type: 'text', title: 'Reply', placeholder: 'Type your message...' }
        ],
        data: {
            url: data.url || '/'
        }
    };
    event.waitUntil(
        self.registration.showNotification(data.title || '{{CHATNAME}}', options)
            .then(() => syncBadge())
    );
});

self.addEventListener('notificationclick', function (event) {
    if (event.action === 'reply') {
        const replyText = event.reply;
        const url = new URL(event.notification.data.url || '/', self.location.origin);
        const chatId = url.searchParams.get('chat');

        if (chatId && replyText) {
            event.waitUntil(
                fetch(`/api/chats/${chatId}/messages`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: replyText })
                }).then(() => {
                    event.notification.close();
                    return syncBadge();
                }).catch(err => {
                    console.error('Failed to send reply:', err);
                })
            );
        } else {
            event.notification.close();
            event.waitUntil(syncBadge());
        }
        return;
    }

    event.notification.close();
    event.waitUntil(syncBadge());

    const urlToOpen = new URL(event.notification.data.url || '/', self.location.origin).href;

    event.waitUntil(
        clients.matchAll({
            type: 'window',
            includeUncontrolled: true
        }).then(function (windowClients) {
            let clientToFocus = null;

            // 1. Try to find an existing tab that is already at the target URL
            for (let i = 0; i < windowClients.length; i++) {
                const client = windowClients[i];
                if (client.url === urlToOpen && 'focus' in client) {
                    clientToFocus = client;
                    break;
                }
            }

            // 2. If no exact match, try to find a tab at the main app route
            if (!clientToFocus) {
                for (let i = 0; i < windowClients.length; i++) {
                    const client = windowClients[i];
                    try {
                        const pathname = new URL(client.url, self.location.origin).pathname;
                        if ((pathname === '/' || pathname === '/index.html') && 'focus' in client) {
                            clientToFocus = client;
                            break;
                        }
                    } catch (_) { /* skip unparseable client URLs */ }
                }
            }

            // 3. If still no main app client, just find any focusable client
            if (!clientToFocus) {
                for (let i = 0; i < windowClients.length; i++) {
                    const client = windowClients[i];
                    if ('focus' in client) {
                        clientToFocus = client;
                        break;
                    }
                }
            }

            if (clientToFocus) {
                let isAppShell = false;
                try {
                    const clientPathname = new URL(clientToFocus.url, self.location.origin).pathname;
                    isAppShell = clientPathname === '/' || clientPathname === '/index.html';
                } catch (_) { /* treat unparseable as non-app-shell */ }

                if (isAppShell) {
                    clientToFocus.postMessage({ type: 'open_chat', url: urlToOpen });
                    return clientToFocus.focus();
                } else {
                    if ('navigate' in clientToFocus) {
                        return clientToFocus.navigate(urlToOpen).then(client => client ? client.focus() : null);
                    }
                    return clientToFocus.focus();
                }
            }

            // 4. If no tabs are open, open a new one
            if (clients.openWindow) {
                return clients.openWindow(urlToOpen);
            }
        })
    );
});
