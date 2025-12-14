package chat

import (
	"sync"
)

type Seq int64

type ChatRecord struct {
	Seq       Seq
	Timestamp int64
	UserID    string
	Content   string
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

	mux sync.RWMutex
}

type Config struct {
	ID             string
	MaxRecords     int
	RecordCallback func(receiverID string, chatID string, record ChatRecord)
}

func New(config Config) *Chat {
	return &Chat{
		ID:             config.ID,
		MaxRecords:     config.MaxRecords,
		LastIndex:      -1,
		FirstSeq:       -1,
		LastSeq:        -1,
		Members:        make(map[string]bool),
		RecordCallback: config.RecordCallback,
	}
}

// AddRecord adds a new chat record to the chat:
// - Adding it into Records ring buffer
// - Updating FirstSeq and LastSeq
// - Sending updates to all connected clients
func (c *Chat) AddRecord(record ChatRecord) {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.LastSeq++
	record.Seq = c.LastSeq

	// Add record to ring buffer
	switch {
	case len(c.Records) < c.MaxRecords:
		if c.FirstSeq == -1 {
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
}

func (c *Chat) GetRecords(from, to Seq) ([]ChatRecord, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if c.FirstSeq == -1 {
		return []ChatRecord{}, nil
	}

	// Clamp range
	if from < c.FirstSeq {
		from = c.FirstSeq
	}
	if to > c.LastSeq+1 {
		to = c.LastSeq + 1
	}
	if from >= to {
		return []ChatRecord{}, nil
	}

	count := int(to - from)
	result := make([]ChatRecord, count)

	// Calculate start index in ring buffer
	// Head index (oldest record)
	head := 0
	if len(c.Records) == c.MaxRecords {
		head = (c.LastIndex + 1) % c.MaxRecords
	}

	// Offset of 'from' relative to 'FirstSeq'
	offset := int(from - c.FirstSeq)

	// Actual index in buffer
	startIdx := (head + offset) % len(c.Records)

	// Copy
	if startIdx+count <= len(c.Records) {
		copy(result, c.Records[startIdx:startIdx+count])
	} else {
		n1 := len(c.Records) - startIdx
		copy(result, c.Records[startIdx:])
		copy(result[n1:], c.Records[:count-n1])
	}

	return result, nil
}

func (c *Chat) GetLastRecords(count int) ([]ChatRecord, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if c.LastSeq == -1 {
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
