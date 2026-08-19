package models

import (
	"encoding/json"
	"strings"
	"testing"
)

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
