package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

// receipt is how far one person has read a room.
type receipt struct {
	event string
	at    int64
}

// noteReceipts keeps each person's newest public receipt. Private ones are only ever this account's own.
func (r *room) noteReceipts(events []event) {
	for _, item := range events {
		if item.Type != "m.receipt" {
			continue
		}
		var content map[string]map[string]map[string]struct {
			At     int64  `json:"ts"`
			Thread string `json:"thread_id"`
		}
		if json.Unmarshal(item.Content, &content) != nil {
			continue
		}
		for id, kinds := range content {
			for user, value := range kinds["m.read"] {
				if value.Thread != "" && value.Thread != "main" {
					continue
				}
				if saved, ok := r.receipts[user]; !ok || value.At >= saved.at {
					if r.receipts == nil {
						r.receipts = map[string]receipt{}
					}
					r.receipts[user] = receipt{event: id, at: value.At}
				}
			}
		}
	}
}

// person names someone in a chat. This account always reads as You.
func (m *matrix) person(chat, id string) contract.HubMember {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := contract.HubMember{ID: id, Name: remoteName(id)}
	if room := m.rooms[chat]; room != nil {
		if item, ok := room.state["m.room.member\x00"+id]; ok {
			value.Avatar = decode[member](item.Content).Avatar
		}
	}
	if name := m.nameIn(chat, id); name != "" {
		value.Name = name
	}
	return value
}

// addPerson keeps You first, as the tooltip reads, and each person once.
func addPerson(people []contract.HubMember, person contract.HubMember) []contract.HubMember {
	if slices.ContainsFunc(people, func(item contract.HubMember) bool { return item.ID == person.ID }) {
		return people
	}
	if person.Name == "You" {
		return slices.Insert(people, 0, person)
	}
	return append(people, person)
}

// placeReaders puts each person on the newest message they've reached, so a chat shows where
// everyone is. A position outside this page belongs to another page, so it's left out here.
func placeReaders(events []event, messages []contract.HubMessage, receipts map[string]receipt, person func(string) contract.HubMember) {
	positions := make(map[string]int64, len(events))
	for _, item := range events {
		positions[item.ID] = item.At
	}
	users := make([]string, 0, len(receipts))
	for user := range receipts {
		users = append(users, user)
	}
	slices.Sort(users)
	for _, user := range users {
		at, found := positions[receipts[user].event]
		if !found {
			continue
		}
		for index := len(messages) - 1; index >= 0; index-- {
			message := &messages[index]
			if message.At > at || message.Sender == user || message.Kind == contract.HubMessageKindNotice {
				continue
			}
			message.Readers = append(message.Readers, person(user))
			break
		}
	}
	for index := range messages {
		slices.SortFunc(messages[index].Readers, func(a, b contract.HubMember) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
		})
	}
}

// markReaders fills in who has read a page, leaving out this account and bridge bots.
func (m *matrix) markReaders(ctx context.Context, chat string, events []event, messages []contract.HubMessage) {
	m.mu.Lock()
	receipts := map[string]receipt{}
	unknown := false
	if room := m.rooms[chat]; room != nil {
		for user, value := range room.receipts {
			localpart, _, _ := strings.Cut(strings.TrimPrefix(user, "@"), ":")
			if user == m.user || strings.HasSuffix(localpart, "bot") || isSelf(user, m.selves) {
				continue
			}
			receipts[user] = value
			if _, known := room.state["m.room.member\x00"+user]; !known {
				unknown = true
			}
		}
	}
	m.mu.Unlock()
	// The stream only carries members who've spoken, and a quiet reader still has a name.
	names := map[string]contract.HubMember{}
	if unknown {
		if members, err := m.members(ctx, chat); err == nil {
			for _, member := range members {
				names[member.ID] = member
			}
		}
	}
	placeReaders(events, messages, receipts, func(id string) contract.HubMember {
		if value, ok := names[id]; ok {
			return value
		}
		return m.person(chat, id)
	})
}
