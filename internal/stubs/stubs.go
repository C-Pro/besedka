package stubs

import (
	"besedka/internal/models"
	"time"
)

var Users = []models.User{
	{ID: "1", DisplayName: "Alice", AvatarURL: "https://api.dicebear.com/7.x/avataaars/svg?seed=Alice", Presence: models.Presence{Online: true, LastSeen: time.Now().Unix()}},
	{ID: "2", DisplayName: "Bob", AvatarURL: "https://api.dicebear.com/7.x/avataaars/svg?seed=Bob", Presence: models.Presence{Online: false, LastSeen: time.Now().Add(-1 * time.Hour).Unix()}},
	{ID: "3", DisplayName: "Charlie", AvatarURL: "https://api.dicebear.com/7.x/avataaars/svg?seed=Charlie", Presence: models.Presence{Online: true, LastSeen: time.Now().Unix()}},
}

var Chats = []models.Chat{
	{ID: "townhall", Name: "Townhall", LastSeq: 0, IsDM: false},
	{ID: "dm_1_2", Name: "Alice", LastSeq: 2, IsDM: true, Online: true},
	{ID: "dm_1_3", Name: "Charlie", LastSeq: 0, IsDM: true, Online: true},
}
