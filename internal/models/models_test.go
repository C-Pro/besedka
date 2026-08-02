package models

import (
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
