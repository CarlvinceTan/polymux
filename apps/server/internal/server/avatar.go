package server

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var shapes = map[string]bool{
	"cercle": true, "galet": true, "squircle": true, "capsule": true,
	"triangle": true, "hexagone": true, "nuage": true, "goutte": true,
}
var colors = map[string]bool{
	"encre": true, "brun": true, "rouge": true, "orange": true,
	"ambre": true, "vert": true, "turquoise": true, "bleu": true,
	"violet": true, "rose": true, "gris": true, "creme": true,
}
var expressions = map[string]bool{
	"neutre": true, "attentif": true, "surpris": true, "excite": true,
	"heureux": true, "hilare": true, "colere": true, "triste": true,
	"effraye": true, "mefiant": true, "confus": true, "curieux": true,
	"fier": true, "timide": true, "blase": true, "somnolent": true,
}
var durations = map[string]float64{
	"idle": 0.6, "thinking": 0.6, "wink": 0.6, "wide": 0.6,
	"alert": 2, "notify": 0.6, "exclaim": 0.6, "sleep": 0.6,
	"egg": 0.6, "hexagon": 0.6, "play": 0.6, "orbit": 2.5,
	"burst": 2.4, "comet": 2.4, "swirl": 1.3,
}

func defaultAvatar() contract.AvatarDesign {
	v := contract.AvatarDesign{Shape: "cercle", Color: "encre", Expression: "neutre", FollowCursor: true, Animates: true, Blocks: []contract.AnimationBlock{}}
	for i, block := range []struct {
		state    string
		duration float64
	}{
		{"idle", 2.4}, {"thinking", 2.6}, {"wink", 1.6}, {"wide", 1.8},
		{"alert", 2.4}, {"notify", 2.2}, {"exclaim", 2}, {"sleep", 2.4},
		{"egg", 1.8}, {"hexagon", 1.6}, {"play", 2}, {"orbit", 3.4},
		{"burst", 2.6}, {"comet", 2.4},
	} {
		v.Blocks = append(v.Blocks, contract.AnimationBlock{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1), State: block.state, Duration: block.duration})
	}
	return v
}

func validAvatar(v contract.AvatarDesign) bool {
	if !shapes[v.Shape] || !colors[v.Color] || !expressions[v.Expression] || len(v.Blocks) > 200 {
		return false
	}
	ids := make(map[string]bool, len(v.Blocks))
	for _, b := range v.Blocks {
		minimum, known := durations[b.State]
		if strings.TrimSpace(b.ID) == "" || len(b.ID) > 128 || ids[b.ID] || !known || math.IsNaN(b.Duration) || math.IsInf(b.Duration, 0) || b.Duration < minimum || b.Duration > 10 {
			return false
		}
		ids[b.ID] = true
	}
	return true
}

func (s *Server) updateAvatar(w http.ResponseWriter, r *http.Request) {
	var avatar contract.AvatarDesign
	if !decode(w, r, &avatar) {
		return
	}
	if !validAvatar(avatar) {
		fail(w, 400, "invalid_avatar", "Choose a valid avatar and animation sequence.")
		return
	}
	if avatar.Blocks == nil {
		avatar.Blocks = []contract.AnimationBlock{}
	}
	s.actorsMu.RLock()
	defer s.actorsMu.RUnlock()
	if s.closing || s.ctx.Err() != nil {
		fail(w, 503, "closing", "The server is restarting. Try again shortly.")
		return
	}
	a := s.actors[r.PathValue("id")]
	if a == nil {
		fail(w, 404, "not_found", "Agent not found.")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	agent, err := s.Store.UpdateAvatar(r.Context(), a.id, avatar)
	if err != nil {
		s.respond(w, nil, err)
		return
	}
	a.definition.Agent.Avatar = &avatar
	write(w, 200, agent)
}
