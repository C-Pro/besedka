package push

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"besedka/internal/models"

	"github.com/SherClockHolmes/webpush-go"
)

type storage interface {
	GetVAPIDKeys() (privateKey, publicKey string, err error)
	SaveVAPIDKeys(privateKey, publicKey string) error
	GetPushSubscriptions(userID string) ([][]byte, error)
	DeletePushSubscription(userID string, endpoint string) error
}

type notifier interface {
	SendNotification(payload []byte, subscription *webpush.Subscription, options *webpush.Options) (*http.Response, error)
}

type defaultNotifier struct{}

func (d defaultNotifier) SendNotification(payload []byte, subscription *webpush.Subscription, options *webpush.Options) (*http.Response, error) {
	return webpush.SendNotification(payload, subscription, options)
}

type Service struct {
	storage    storage
	privateKey string
	publicKey  string
	httpClient *http.Client
	notifier   notifier
}

func NewService(storage storage) (*Service, error) {
	priv, pub, err := storage.GetVAPIDKeys()
	if err != nil {
		// Using direct check for ErrNotFound from models
		if err.Error() == models.ErrNotFound.Error() || err == models.ErrNotFound {
			priv, pub, err = webpush.GenerateVAPIDKeys()
			if err != nil {
				return nil, fmt.Errorf("failed to generate VAPID keys: %w", err)
			}
			if err := storage.SaveVAPIDKeys(priv, pub); err != nil {
				return nil, fmt.Errorf("failed to save VAPID keys: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to load VAPID keys: %w", err)
		}
	}

	return &Service{
		storage:    storage,
		privateKey: priv,
		publicKey:  pub,
		httpClient: &http.Client{},
		notifier:   defaultNotifier{},
	}, nil
}

func (s *Service) PublicKey() string {
	return s.publicKey
}

func (s *Service) SendNotification(userID string, payload []byte) error {
	subs, err := s.storage.GetPushSubscriptions(userID)
	if err != nil {
		slog.Error("failed to get push subscriptions from DB", "userID", userID, "error", err)
		return err
	}

	for _, subData := range subs {
		var sub webpush.Subscription
		if err := json.Unmarshal(subData, &sub); err != nil {
			slog.Error("failed to unmarshal push subscription", "userID", userID, "error", err)
			continue
		}

		resp, err := s.notifier.SendNotification(payload, &sub, &webpush.Options{
			Subscriber:      "besedka-service",
			VAPIDPublicKey:  s.publicKey,
			VAPIDPrivateKey: s.privateKey,
			TTL:             30,
			HTTPClient:      s.httpClient,
		})
		if err != nil {
			slog.Error("failed to send push notification", "userID", userID, "error", err)
			continue
		}

		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			// Subscription is no longer valid
			if err := s.storage.DeletePushSubscription(userID, sub.Endpoint); err != nil {
				slog.Error("failed to delete invalid push subscription", "userID", userID, "error", err)
			}
		}
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close push notification response body", "error", err)
		}
	}

	return nil
}
