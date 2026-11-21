package hub

import (
	"slices"
	"strings"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

type spaceLink struct {
	Via []string `json:"via"`
}

// spaceParents lists the Spaces a room says it's in. A removed link stays as empty state.
func (r *room) spaceParents() []string {
	out := []string{}
	for key, value := range r.state {
		if value.Type == "m.space.parent" && len(decode[spaceLink](value.Content).Via) > 0 {
			_, parent, _ := strings.Cut(key, "\x00")
			out = append(out, parent)
		}
	}
	slices.Sort(out)
	return out
}

// linkSpaces completes what each side of a Space leaves out: some bridges only write
// m.space.child, and a Space a bridge made may not say which platform it's on.
func linkSpaces(chats []contract.HubChat, rooms map[string]*room) {
	places := make(map[string]int, len(chats))
	for index, chat := range chats {
		places[chat.ID] = index
	}
	for id, value := range rooms {
		for key, link := range value.state {
			if link.Type != "m.space.child" || len(decode[spaceLink](link.Content).Via) == 0 {
				continue
			}
			_, child, _ := strings.Cut(key, "\x00")
			if index, ok := places[child]; ok && !slices.Contains(chats[index].Parents, id) {
				chats[index].Parents = append(chats[index].Parents, id)
				slices.Sort(chats[index].Parents)
			}
		}
	}
	for index := range chats {
		space := &chats[index]
		if !space.Space || space.Platform != "matrix" {
			continue
		}
		platforms := map[string]bool{}
		for _, chat := range chats {
			if chat.Platform != "matrix" && slices.Contains(chat.Parents, space.ID) {
				platforms[chat.Platform] = true
				space.Platform = chat.Platform
			}
		}
		if len(platforms) > 1 {
			space.Platform = "matrix"
		}
	}
}

// linkAccounts names the logins each chat belongs to. A login's filtering Space holds its
// chats, a split portal names its receiver, and a shared group can have several.
func linkAccounts(chats []contract.HubChat, rooms map[string]*room, spaces map[string]string) {
	for index := range chats {
		chat := &chats[index]
		chat.Accounts = []string{}
		for _, parent := range chat.Parents {
			if login, ok := spaces[parent]; ok && !slices.Contains(chat.Accounts, login) {
				chat.Accounts = append(chat.Accounts, login)
			}
		}
		if value := rooms[chat.ID]; value != nil {
			if login := value.receiver(); login != "" && !slices.Contains(chat.Accounts, login) {
				chat.Accounts = append(chat.Accounts, login)
			}
		}
	}
	for index := range chats {
		space := &chats[index]
		if !space.Space {
			continue
		}
		for _, chat := range chats {
			if !slices.Contains(chat.Parents, space.ID) {
				continue
			}
			for _, login := range chat.Accounts {
				if !slices.Contains(space.Accounts, login) {
					space.Accounts = append(space.Accounts, login)
				}
			}
		}
	}
}
