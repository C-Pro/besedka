package push

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"besedka/internal/models"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/stretchr/testify/require"
)

type mockStorage struct {
	keys         DBVAPIDKeys
	subs          map[string][][]byte
	deletedSubs   []string
	keysNotFound  bool
}

type DBVAPIDKeys struct {
	PrivateKey string
	PublicKey  string
}

func (m *mockStorage) GetVAPIDKeys() (string, string, error) {
	if m.keysNotFound {
		return "", "", models.ErrNotFound
	}
	return m.keys.PrivateKey, m.keys.PublicKey, nil
}

func (m *mockStorage) SaveVAPIDKeys(priv, pub string) error {
	m.keys = DBVAPIDKeys{PrivateKey: priv, PublicKey: pub}
	m.keysNotFound = false
	return nil
}

func (m *mockStorage) GetPushSubscriptions(userID string) ([][]byte, error) {
	return m.subs[userID], nil
}

func (m *mockStorage) DeletePushSubscription(userID string, endpoint string) error {
	m.deletedSubs = append(m.deletedSubs, endpoint)
	return nil
}

type mockNotifier struct {
	resp *http.Response
	err  error
}

func (m *mockNotifier) SendNotification(payload []byte, sub *webpush.Subscription, options *webpush.Options) (*http.Response, error) {
	return m.resp, m.err
}

func TestNewService(t *testing.T) {
	t.Run("Generate keys if not found", func(t *testing.T) {
		ms := &mockStorage{keysNotFound: true}
		service, err := NewService(ms)
		require.NoError(t, err)
		require.NotEmpty(t, service.PublicKey())
		require.False(t, ms.keysNotFound)
		require.NotEmpty(t, ms.keys.PublicKey)
	})

	t.Run("Load existing keys", func(t *testing.T) {
		priv, pub, _ := webpush.GenerateVAPIDKeys()
		ms := &mockStorage{keys: DBVAPIDKeys{PrivateKey: priv, PublicKey: pub}}
		service, err := NewService(ms)
		require.NoError(t, err)
		require.Equal(t, pub, service.PublicKey())
	})
}

func TestSendNotification(t *testing.T) {
	priv, pub, _ := webpush.GenerateVAPIDKeys()
	
	t.Run("Successfully send notification", func(t *testing.T) {
		sub := webpush.Subscription{
			Endpoint: "http://example.com",
		}
		subBytes, _ := json.Marshal(sub)

		ms := &mockStorage{
			keys: DBVAPIDKeys{PrivateKey: priv, PublicKey: pub},
			subs: map[string][][]byte{"user1": {subBytes}},
		}

		service, _ := NewService(ms)
		service.notifier = &mockNotifier{
			resp: &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(bytes.NewReader([]byte("OK"))),
			},
		}

		err := service.SendNotification("user1", []byte(`{"title":"test"}`))
		require.NoError(t, err)
		require.Empty(t, ms.deletedSubs)
	})

	t.Run("Delete subscription on 410 Gone", func(t *testing.T) {
		sub := webpush.Subscription{
			Endpoint: "http://example.com/gone",
		}
		subBytes, _ := json.Marshal(sub)

		ms := &mockStorage{
			keys: DBVAPIDKeys{PrivateKey: priv, PublicKey: pub},
			subs: map[string][][]byte{"user1": {subBytes}},
		}

		service, _ := NewService(ms)
		service.notifier = &mockNotifier{
			resp: &http.Response{
				StatusCode: http.StatusGone,
				Body:       io.NopCloser(bytes.NewReader([]byte("Gone"))),
			},
		}

		err := service.SendNotification("user1", []byte(`{"title":"test"}`))
		require.NoError(t, err)
		require.Contains(t, ms.deletedSubs, "http://example.com/gone")
	})
}
