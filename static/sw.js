self.addEventListener('install', (event) => {
  console.log('Service Worker installing...');
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  console.log('Service Worker activating...');
});

self.addEventListener('fetch', (event) => {
  // Dummy fetch handler to satisfy PWA installability requirements
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

self.addEventListener('push', function(event) {
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

self.addEventListener('notificationclick', function(event) {
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
    }).then(function(windowClients) {
      let clientToFocus = null;
      
      // 1. Try to find an existing tab that is already at the target URL
      for (let i = 0; i < windowClients.length; i++) {
        const client = windowClients[i];
        if (client.url === urlToOpen && 'focus' in client) {
          clientToFocus = client;
          break;
        }
      }
      
      // 2. If no exact match, try to find ANY tab of this app
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
        clientToFocus.postMessage({ type: 'open_chat', url: urlToOpen });
        return clientToFocus.focus();
      }

      // 3. If no tabs are open, open a new one
      if (clients.openWindow) {
        return clients.openWindow(urlToOpen);
      }
    })
  );
});
