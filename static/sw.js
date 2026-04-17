self.addEventListener('install', (event) => {
  console.log('Service Worker installing...');
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  console.log('Service Worker activating...');
});

self.addEventListener('fetch', (event) => {
  // Dummy fetch handler
});

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
      title: 'Besedka',
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
    data: {
      url: data.url || '/'
    }
  };
  event.waitUntil(
    self.registration.showNotification(data.title || 'Besedka', options)
  );
});

self.addEventListener('notificationclick', function(event) {
  event.notification.close();
  
  const urlToOpen = new URL(event.notification.data.url || '/', self.location.origin).href;

  event.waitUntil(
    clients.matchAll({
      type: 'window',
      includeUncontrolled: true
    }).then(function(windowClients) {
      // 1. Try to find an existing tab that is already at the target URL
      for (let i = 0; i < windowClients.length; i++) {
        const client = windowClients[i];
        if (client.url === urlToOpen && 'focus' in client) {
          return client.focus();
        }
      }
      
      // 2. If no exact match, try to find ANY tab of this app and navigate it
      for (let i = 0; i < windowClients.length; i++) {
        const client = windowClients[i];
        if ('navigate' in client && 'focus' in client) {
          return client.navigate(urlToOpen).then(c => c.focus());
        }
      }

      // 3. If no tabs are open, open a new one
      if (clients.openWindow) {
        return clients.openWindow(urlToOpen);
      }
    })
  );
});
