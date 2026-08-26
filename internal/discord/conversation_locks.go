package discord

import "sync"

// ConversationLocks serializes turns and history-mutating commands per
// conversation (guild, thread), so history stays in order.
type ConversationLocks struct {
	mu sync.Map // map[string]*sync.Mutex, keyed by guildID + "|" + threadID
}

func NewConversationLocks() *ConversationLocks { return &ConversationLocks{} }

// Lock acquires the lock for the (guildID, threadID) conversation and
// returns a function that releases it.
func (c *ConversationLocks) Lock(guildID, threadID string) func() {
	v, _ := c.mu.LoadOrStore(guildID+"|"+threadID, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}
