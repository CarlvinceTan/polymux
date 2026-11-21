package hub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

const (
	maxLinkMembers   = 32
	maxRecipients    = 128
	maxBroadcastSent = 250
)

// people holds what Hub knows that no remote platform does: which chats are the
// same person, and broadcast lists. Neither ever leaves this server.
type people struct {
	path string
	send func(context.Context, string, string) error

	mu    sync.Mutex
	state peopleState
}

type peopleState struct {
	Links      []contract.HubLink      `json:"links"`
	Broadcasts []contract.HubBroadcast `json:"broadcasts"`
}

func openPeople(dir string, send func(context.Context, string, string) error) (*people, error) {
	value := &people{path: filepath.Join(dir, "people.json"), send: send,
		state: peopleState{Links: []contract.HubLink{}, Broadcasts: []contract.HubBroadcast{}}}
	data, err := os.ReadFile(value.path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return nil, err
	}
	return value, json.Unmarshal(data, &value.state)
}

func (p *people) save() error {
	data, err := json.Marshal(p.state)
	if err != nil {
		return err
	}
	temporary := p.path + ".new"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, p.path)
}

func (p *people) links() []contract.HubLink {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.state.Links)
}

func unique(members []contract.HubLinkMember) []contract.HubLinkMember {
	out := []contract.HubLinkMember{}
	for _, member := range members {
		if !slices.ContainsFunc(out, func(item contract.HubLinkMember) bool { return item.Chat == member.Chat }) {
			out = append(out, member)
		}
	}
	return out
}

// merge links chats as one person. Overlapping links fold into one, so a chat never has two owners.
func (p *people) merge(input contract.HubLinkEdit) (contract.HubLink, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	requested := unique(input.Members)
	if len(requested) < 2 {
		return contract.HubLink{}, publicError("Choose at least two chats.")
	}
	kept := []contract.HubLink{}
	members := []contract.HubLinkMember{}
	name := strings.TrimSpace(input.Name)
	for _, link := range p.state.Links {
		overlaps := slices.ContainsFunc(link.Members, func(member contract.HubLinkMember) bool {
			return slices.ContainsFunc(requested, func(item contract.HubLinkMember) bool { return item.Chat == member.Chat })
		})
		if !overlaps {
			kept = append(kept, link)
			continue
		}
		members = append(members, link.Members...)
		if name == "" {
			name = link.Name
		}
	}
	members = unique(append(members, requested...))
	if len(members) > maxLinkMembers {
		return contract.HubLink{}, publicError("That's too many chats for one contact.")
	}
	if name == "" {
		name = members[0].Name
	}
	link := contract.HubLink{ID: "contact-" + randomHex(8), Name: name, Members: members}
	p.state.Links = append(kept, link)
	return link, p.save()
}

func (p *people) unlink(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.Links = slices.DeleteFunc(p.state.Links, func(link contract.HubLink) bool { return link.ID == id })
	return p.save()
}

func (p *people) broadcasts() []contract.HubBroadcast {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.state.Broadcasts)
}

func (p *people) saveBroadcast(id string, input contract.HubBroadcastEdit) (contract.HubBroadcast, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := strings.TrimSpace(input.Name)
	recipients := unique(input.Recipients)
	if name == "" || len(recipients) == 0 {
		return contract.HubBroadcast{}, publicError("Name the list and choose who gets it.")
	}
	if len(recipients) > maxRecipients {
		return contract.HubBroadcast{}, publicError("That's too many people for one list.")
	}
	if id == "" {
		value := contract.HubBroadcast{ID: "broadcast-" + randomHex(8), Name: name, Recipients: recipients, Messages: []contract.HubBroadcastMessage{}}
		p.state.Broadcasts = append(p.state.Broadcasts, value)
		return value, p.save()
	}
	index := slices.IndexFunc(p.state.Broadcasts, func(item contract.HubBroadcast) bool { return item.ID == id })
	if index < 0 {
		return contract.HubBroadcast{}, errMissing
	}
	p.state.Broadcasts[index].Name, p.state.Broadcasts[index].Recipients = name, recipients
	return p.state.Broadcasts[index], p.save()
}

func (p *people) removeBroadcast(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state.Broadcasts = slices.DeleteFunc(p.state.Broadcasts, func(item contract.HubBroadcast) bool { return item.ID == id })
	return p.save()
}

// sendBroadcast writes to each chat on its own, so no one sees the list and one failure stops nothing.
func (p *people) sendBroadcast(ctx context.Context, id, text string) (contract.HubBroadcastMessage, error) {
	p.mu.Lock()
	index := slices.IndexFunc(p.state.Broadcasts, func(item contract.HubBroadcast) bool { return item.ID == id })
	if index < 0 {
		p.mu.Unlock()
		return contract.HubBroadcastMessage{}, errMissing
	}
	recipients := slices.Clone(p.state.Broadcasts[index].Recipients)
	p.mu.Unlock()
	message := contract.HubBroadcastMessage{ID: randomHex(8), Text: text, At: time.Now().UnixMilli(), Deliveries: []contract.HubDelivery{}}
	for _, recipient := range recipients {
		err := p.send(ctx, recipient.Chat, text)
		message.Deliveries = append(message.Deliveries, contract.HubDelivery{Chat: recipient.Chat, Name: recipient.Name, Ok: err == nil})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	index = slices.IndexFunc(p.state.Broadcasts, func(item contract.HubBroadcast) bool { return item.ID == id })
	if index < 0 {
		return message, nil
	}
	sent := append(p.state.Broadcasts[index].Messages, message)
	if len(sent) > maxBroadcastSent {
		sent = sent[len(sent)-maxBroadcastSent:]
	}
	p.state.Broadcasts[index].Messages = sent
	return message, p.save()
}
