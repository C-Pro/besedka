package chat

import (
	"besedka/internal/models"
	"fmt"
	"log/slog"
	"sync"
)

type storage interface {
	UpsertMessage(message models.Message) error
	ListMessages(chatID string, from, to int64) ([]models.Message, error)
}

type Seq int64

type ChatRecord struct {
	Seq         Seq
	Timestamp   int64
	UserID      string
	Content     string
	Attachments []models.Attachment
}

type Chat struct {
	ID         string
	Name       string
	Records    []ChatRecord
	Members    map[string]bool
	FirstSeq   Seq
	LastSeq    Seq
	LastIndex  int
	MaxRecords int

	RecordCallback func(receiverID string, chatID string, record ChatRecord)

	storage storage
	mux     sync.RWMutex
}

type Config struct {
	ID             string
	MaxRecords     int
	RecordCallback func(receiverID string, chatID string, record ChatRecord)
	Storage        storage
}

func New(config Config) *Chat {
	return &Chat{
		ID:             config.ID,
		MaxRecords:     config.MaxRecords,
		LastIndex:      -1,
		FirstSeq:       0,
		LastSeq:        0,
		Members:        make(map[string]bool),
		RecordCallback: config.RecordCallback,
		storage:        config.Storage,
	}
}

// AddRecord adds a new chat record to the chat:
// - Adding it into Records ring buffer
// - Updating FirstSeq and LastSeq
// - Persisting into storage
// - Sending updates to all connected clients
func (c *Chat) AddRecord(record ChatRecord) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.LastSeq++
	record.Seq = c.LastSeq

	// Persist
	if c.storage != nil {
		err := c.storage.UpsertMessage(models.Message{
			Seq:         int64(record.Seq),
			Timestamp:   record.Timestamp,
			ChatID:      c.ID,
			UserID:      record.UserID,
			Content:     record.Content,
			Attachments: record.Attachments,
		})
		if err != nil {
			slog.Error("failed to persist message", "chatID", c.ID, "error", err)
			return fmt.Errorf("failed to persist message: %w", err)
		}
	}

	// Add record to ring buffer
	switch {
	case len(c.Records) < c.MaxRecords:
		if c.FirstSeq == 0 {
			c.FirstSeq = c.LastSeq
		}
		c.Records = append(c.Records, record)
		c.LastIndex++
	default:
		c.FirstSeq++
		i := (c.LastIndex + 1) % c.MaxRecords
		c.Records[i] = record
		c.LastIndex = i
	}

	for receiverID, online := range c.Members {
		if online && c.RecordCallback != nil {
			c.RecordCallback(receiverID, c.ID, record)
		}
	}
	return nil
}

func (c *Chat) GetRecords(from, to Seq) ([]ChatRecord, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	// If no records in memory and no storage, nothing to return
	if c.LastSeq == 0 {
		return []ChatRecord{}, nil
	}

	// 1. Determine what we can serve from memory
	memFrom := c.FirstSeq
	if len(c.Records) == 0 {
		memFrom = c.LastSeq + 1 // Effectively empty
	}

	var result []ChatRecord

	// 2. Fetch from storage if 'from' is before what we have in memory
	if from < memFrom && c.storage != nil {
		storeTo := to
		if storeTo > memFrom {
			storeTo = memFrom
		}

		msgs, err := c.storage.ListMessages(c.ID, int64(from), int64(storeTo-1))
		if err != nil {
			return nil, fmt.Errorf("storage error: %w", err)
		}

		for _, m := range msgs {
			result = append(result, ChatRecord{
				Seq:         Seq(m.Seq),
				Timestamp:   m.Timestamp,
				UserID:      m.UserID,
				Content:     m.Content,
				Attachments: m.Attachments,
			})
		}
	}

	// 3. Fetch from memory
	// If the request extends into memory range
	if to > memFrom {
		// Adjust 'from' for memory fetch if we already fetched some from storage
		mFrom := from
		if mFrom < memFrom {
			mFrom = memFrom
		}

		if mFrom < to && mFrom >= c.FirstSeq {
			// Memory fetch logic...
			count := int(to - mFrom)

			// Calculate start index in ring buffer
			head := 0
			if len(c.Records) == c.MaxRecords {
				head = (c.LastIndex + 1) % c.MaxRecords
			}
			offset := int(mFrom - c.FirstSeq)
			startIdx := (head + offset) % len(c.Records)

			if startIdx+count <= len(c.Records) {
				result = append(result, c.Records[startIdx:startIdx+count]...)
			} else {
				n1 := len(c.Records) - startIdx
				result = append(result, c.Records[startIdx:]...)
				result = append(result, c.Records[:count-n1]...)
			}
		}
	}

	return result, nil
}

func (c *Chat) GetLastRecords(count int) ([]ChatRecord, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if c.LastSeq == 0 {
		return []ChatRecord{}, nil
	}

	total := int(c.LastSeq - c.FirstSeq + 1)
	if count > total {
		count = total
	}

	// We want [LastSeq - count + 1, LastSeq + 1)
	from := c.LastSeq - Seq(count) + 1

	result := make([]ChatRecord, count)

	head := 0
	if len(c.Records) == c.MaxRecords {
		head = (c.LastIndex + 1) % c.MaxRecords
	}

	offset := int(from - c.FirstSeq)
	startIdx := (head + offset) % len(c.Records)

	if startIdx+count <= len(c.Records) {
		copy(result, c.Records[startIdx:startIdx+count])
	} else {
		n1 := len(c.Records) - startIdx
		copy(result, c.Records[startIdx:])
		copy(result[n1:], c.Records[:count-n1])
	}

	return result, nil
}

func (c *Chat) addMember(userID string, online bool) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.Members[userID] = online
}

func (c *Chat) Join(userID string) {
	c.addMember(userID, true)
}

func (c *Chat) Leave(userID string) {
	c.addMember(userID, false)
}
