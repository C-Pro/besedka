package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserSettingsThemeDefaultsAndValidation(t *testing.T) {
	settings := DefaultUserSettings()
	if settings.Appearance.Theme != ThemeDark {
		t.Fatalf("default theme = %q, want %q", settings.Appearance.Theme, ThemeDark)
	}

	legacy := NormalizeUserSettings(UserSettings{})
	if legacy.Appearance.Theme != ThemeDark {
		t.Fatalf("normalized legacy theme = %q, want %q", legacy.Appearance.Theme, ThemeDark)
	}

	for _, theme := range []string{ThemeDark, ThemeLight, ThemeSystem} {
		if !IsValidTheme(theme) {
			t.Errorf("expected %q to be valid", theme)
		}
	}
	for _, theme := range []string{"", "auto", "sepia"} {
		if IsValidTheme(theme) {
			t.Errorf("expected %q to be invalid", theme)
		}
	}
}

func TestIsMessageVisible(t *testing.T) {
	humanUser := User{ID: "h1", UserName: "alice", Type: UserTypeHuman}
	webhookUser := User{ID: "w1", UserName: "wh", Type: UserTypeWebhook}
	botReadAll := User{ID: "b1", UserName: "allbot", Type: UserTypeBot, BotPermissions: BotPermissions{ReadAll: true}}
	botReadMentions := User{ID: "b2", UserName: "mentionbot", Type: UserTypeBot, BotPermissions: BotPermissions{ReadMentions: true}}
	botNoRead := User{ID: "b3", UserName: "noreadbot", Type: UserTypeBot, BotPermissions: BotPermissions{}}

	// Human user
	if !IsMessageVisible("townhall", nil, humanUser) {
		t.Errorf("expected human user to see townhall message")
	}

	// Webhook user
	if IsMessageVisible("townhall", nil, webhookUser) {
		t.Errorf("expected webhook user to not see townhall message")
	}

	// Bot in townhall with ReadAll
	if !IsMessageVisible("townhall", nil, botReadAll) {
		t.Errorf("expected bot with ReadAll to see townhall message")
	}

	// Bot in townhall with ReadMentions
	if IsMessageVisible("townhall", []string{"other"}, botReadMentions) {
		t.Errorf("expected bot with ReadMentions to not see unmentioned message")
	}
	if !IsMessageVisible("townhall", []string{"mentionbot"}, botReadMentions) {
		t.Errorf("expected bot with ReadMentions to see mentioned message")
	}

	// Bot in townhall with no read perms
	if IsMessageVisible("townhall", []string{"noreadbot"}, botNoRead) {
		t.Errorf("expected bot with no read perms to not see townhall message")
	}

	// Bot in DM
	if !IsMessageVisible("dm_h1_b3", nil, botNoRead) {
		t.Errorf("expected bot to see DM message regardless of townhall permissions")
	}
}

func TestServerMessageJSONOmitempty(t *testing.T) {
	locMsg := ServerMessage{
		Type: ServerMessageTypeLocation,
		UserLocations: []UserLocation{
			{UserID: "u1", Location: Location{Lat: 1.0, Lng: 2.0}},
		},
	}
	data, err := json.Marshal(locMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	str := string(data)
	if strings.Contains(str, `"user"`) {
		t.Errorf("expected 'user' to be omitted, got: %s", str)
	}
	if strings.Contains(str, `"chat"`) {
		t.Errorf("expected 'chat' to be omitted, got: %s", str)
	}

	newMsg := ServerMessage{
		Type: ServerMessageTypeNew,
		User: &User{ID: "u1", UserName: "alice"},
		Chat: &Chat{ID: "c1", Name: "Townhall"},
	}
	data, err = json.Marshal(newMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}

	str = string(data)
	if !strings.Contains(str, `"user"`) || !strings.Contains(str, `"chat"`) {
		t.Errorf("expected 'user' and 'chat' to be present, got: %s", str)
	}
}

func TestUserInfoJSONSerialization(t *testing.T) {
	// User with empty TimeZone and PreferredLanguage should omit them
	uEmpty := User{ID: "u1", UserName: "alice"}
	data, err := json.Marshal(uEmpty)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	str := string(data)
	if strings.Contains(str, `"timeZone"`) || strings.Contains(str, `"preferredLanguage"`) {
		t.Errorf("expected timeZone and preferredLanguage to be omitted, got: %s", str)
	}

	// User with TimeZone and PreferredLanguage
	uFull := User{
		ID:                "u2",
		UserName:          "bob",
		TimeZone:          "Asia/Tokyo",
		PreferredLanguage: "ja-JP",
	}
	data, err = json.Marshal(uFull)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	str = string(data)
	if !strings.Contains(str, `"timeZone":"Asia/Tokyo"`) || !strings.Contains(str, `"preferredLanguage":"ja-JP"`) {
		t.Errorf("expected timeZone and preferredLanguage to be present, got: %s", str)
	}

	// ClientMessage with userInfo
	enabled := true
	clientMsg := ClientMessage{
		Type:              ClientMessageTypeUserInfo,
		TimeZone:          "Europe/London",
		PreferredLanguage: "en-GB",
		SharingEnabled:    &enabled,
	}
	data, err = json.Marshal(clientMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	str = string(data)
	if !strings.Contains(str, `"type":"userInfo"`) ||
		!strings.Contains(str, `"timeZone":"Europe/London"`) ||
		!strings.Contains(str, `"preferredLanguage":"en-GB"`) ||
		!strings.Contains(str, `"sharingEnabled":true`) {
		t.Errorf("expected userInfo fields present in clientMsg, got: %s", str)
	}

	// Deserialization check
	var decoded ClientMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if decoded.Type != ClientMessageTypeUserInfo ||
		decoded.TimeZone != "Europe/London" ||
		decoded.PreferredLanguage != "en-GB" ||
		decoded.SharingEnabled == nil || !*decoded.SharingEnabled {
		t.Errorf("unexpected decoded clientMsg: %+v", decoded)
	}
}

func TestProgressMessageJSONSerialization(t *testing.T) {
	// Standard legacy message should omit type and progress
	legacyMsg := Message{
		Seq:       1,
		Timestamp: 1700000000,
		ChatID:    "townhall",
		UserID:    "alice",
		Content:   "Hello world",
	}
	data, err := json.Marshal(legacyMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	str := string(data)
	if strings.Contains(str, `"type"`) || strings.Contains(str, `"progress"`) {
		t.Errorf("expected type and progress to be omitted, got: %s", str)
	}

	// Root progress message
	rootMsg := Message{
		Seq:       10,
		Timestamp: 1700000010,
		ChatID:    "townhall",
		UserID:    "newsbot",
		Type:      MessageTypeProgress,
		Progress: &ProgressData{
			CardStatus: ProgressStatusRunning,
			Title:      "Compiling news report...",
			Steps: []ProgressStep{
				{
					ID:          "step-1",
					Title:       "Gathering preferences",
					Description: "Looking up saved topics",
					Status:      ProgressStatusCompleted,
				},
				{
					ID:     "step-2",
					Title:  "Fetching news sources",
					Status: ProgressStatusRunning,
				},
			},
		},
	}
	data, err = json.Marshal(rootMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var decodedMsg Message
	if err := json.Unmarshal(data, &decodedMsg); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if decodedMsg.Type != MessageTypeProgress {
		t.Errorf("expected type %q, got %q", MessageTypeProgress, decodedMsg.Type)
	}
	if decodedMsg.Progress == nil || decodedMsg.Progress.CardStatus != ProgressStatusRunning {
		t.Errorf("expected cardStatus running, got %+v", decodedMsg.Progress)
	}
	if len(decodedMsg.Progress.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(decodedMsg.Progress.Steps))
	}
	if decodedMsg.Progress.Steps[0].ID != "step-1" || decodedMsg.Progress.Steps[0].Status != ProgressStatusCompleted {
		t.Errorf("unexpected step 0: %+v", decodedMsg.Progress.Steps[0])
	}

	// ClientMessage with progress and messageType
	clientMsg := ClientMessage{
		Type:        ClientMessageTypeSend,
		ChatID:      "townhall",
		MessageType: MessageTypeProgress,
		Progress: &ProgressData{
			ParentSeq: 10,
			Step: &ProgressStep{
				ID:     "step-2",
				Title:  "Fetching news sources",
				Status: ProgressStatusCompleted,
			},
			CardStatus: ProgressStatusCompleted,
		},
	}
	data, err = json.Marshal(clientMsg)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	var decodedClient ClientMessage
	if err := json.Unmarshal(data, &decodedClient); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if decodedClient.Type != ClientMessageTypeSend {
		t.Errorf("expected client message type 'send', got %s", decodedClient.Type)
	}
	if decodedClient.MessageType != MessageTypeProgress {
		t.Errorf("expected messageType 'progress', got %s", decodedClient.MessageType)
	}
	if decodedClient.Progress == nil || decodedClient.Progress.ParentSeq != 10 {
		t.Errorf("unexpected progress payload: %+v", decodedClient.Progress)
	}
}
