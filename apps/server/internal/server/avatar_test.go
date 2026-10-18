package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func avatarDesign() contract.AvatarDesign {
	return contract.AvatarDesign{Shape: "hexagone", Color: "creme", Expression: "curieux", FollowCursor: true, Animates: true, PlaySequence: true, Blocks: []contract.AnimationBlock{{ID: "first", State: "orbit", Duration: 2.5}, {ID: "second", State: "swirl", Duration: 1.3}}}
}

func avatarRequest(t *testing.T, api *Server, path string, avatar contract.AvatarDesign, status int) contract.Agent {
	t.Helper()
	data, err := json.Marshal(avatar)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, path, strings.NewReader(string(data))))
	if recorder.Code != status {
		t.Fatalf("avatar update status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var a contract.Agent
	if status == 200 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &a); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func TestMissingAvatarGetsPersistentServerDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(dir, "polymux.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := agentruntime.Definition{Agent: contract.Agent{ID: "existing", Name: "Existing"}, Prompt: "Keep my prompt."}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDefinition(t.Context(), d.Agent, data); err != nil {
		t.Fatal(err)
	}
	api, err := New(t.Context(), db, Config{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	profile := profileRequest(t, api, http.MethodGet, "/agents/existing", "", 200)
	avatar := defaultAvatar()
	if profile.Prompt != d.Prompt || !reflect.DeepEqual(profile.Agent.Avatar, &avatar) || !validAvatar(avatar) {
		t.Fatal("default assignment changed profile or omitted a valid avatar")
	}
	data, err = db.Definition(t.Context(), d.Agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Agent.Avatar, &avatar) {
		t.Fatal("default avatar was not stored with the definition")
	}
}

func TestAvatarPersistsWithoutReplacingProfileOrSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "polymux.db")
	config := Config{DataDir: dir, Model: "provider/default", Definitions: []agentruntime.Definition{{Agent: contract.Agent{ID: "assistant", Name: "Assistant"}, Prompt: "Default", Tools: []string{"read"}}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	open := func() (*Server, *definitionFactory) {
		t.Helper()
		s, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		f := &definitionFactory{opened: make(chan openedAgent, 4)}
		config.Factory = f
		api, err := New(t.Context(), s, config)
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		return api, f
	}
	api, factory := open()
	avatar := avatarDesign()
	body, _ := json.Marshal(contract.CreateAgentRequest{Name: "Original", Role: "Guide", Prompt: "Keep these instructions.", Avatar: &avatar})
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(string(body))))
	var created contract.Agent
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil || recorder.Code != 201 || !reflect.DeepEqual(created.Avatar, &avatar) {
		t.Fatalf("initial shared avatar %+v status=%d err=%v", created, recorder.Code, err)
	}
	send := func(user string) {
		t.Helper()
		body := `{"id":"` + user + `","text":"hello","attachmentIDs":[]}`
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/threads/"+created.ID+"/messages", strings.NewReader(body)))
		if recorder.Code != 202 {
			t.Fatalf("send status=%d", recorder.Code)
		}
		awaitReply(t, api, created.ID, user)
	}
	send("first")
	first := <-factory.opened
	if !first.definition.Intro {
		t.Fatal("creation intro missing")
	}
	first = <-factory.opened
	avatar.Shape = "goutte"
	avatar.Color = "bleu"
	avatar.Expression = "triste"
	avatar.FollowCursor = false
	avatar.PlaySequence = false
	avatar.Blocks = []contract.AnimationBlock{{ID: "custom", State: "comet", Duration: 2.4}}
	updated := avatarRequest(t, api, "/agents/"+created.ID+"/avatar", avatar, 200)
	if !reflect.DeepEqual(updated.Avatar, &avatar) || updated.Name != created.Name || updated.Role != created.Role || updated.Model != created.Model {
		t.Fatalf("avatar patch changed identity/profile %+v", updated)
	}
	profile := profileRequest(t, api, http.MethodGet, "/agents/"+created.ID, "", 200)
	if profile.Prompt != first.definition.Prompt || !reflect.DeepEqual(profile.Agent.Avatar, &avatar) {
		t.Fatalf("avatar patch overwrote instructions %+v", profile)
	}
	profileRequest(t, api, http.MethodPatch, "/agents/"+created.ID, `{"name":"Renamed","role":"Guide","prompt":"Keep these instructions."}`, 200)
	send("second")
	renamed := <-factory.opened
	if renamed.definition.Agent.Name != "Renamed" || renamed.path != "test.jsonl" {
		t.Fatal("identity edit did not safely refresh the prompt")
	}
	api.Close()
	api.Store.Close()
	api, factory = open()
	defer api.Store.Close()
	defer api.Close()
	profile = profileRequest(t, api, http.MethodGet, "/agents/"+created.ID, "", 200)
	if profile.Agent.Name != "Renamed" || profile.Prompt != first.definition.Prompt || !reflect.DeepEqual(profile.Agent.Avatar, &avatar) {
		t.Fatalf("restart lost shared identity %+v", profile)
	}
	defaultProfile := profileRequest(t, api, http.MethodGet, "/agents/assistant", "", 200)
	defaultDesign := defaultAvatar()
	if !reflect.DeepEqual(defaultProfile.Agent.Avatar, &defaultDesign) {
		t.Fatal("server default avatar did not persist")
	}
	send("third")
	restored := <-factory.opened
	if restored.path != "test.jsonl" || !reflect.DeepEqual(restored.definition.Agent.Avatar, &avatar) || len(restored.definition.Tools) != 6 || restored.definition.Prompt != first.definition.Prompt {
		t.Fatalf("avatar edit changed runtime continuity %+v", restored)
	}
	threads, err := api.Store.Threads(t.Context())
	messages, messageErr := api.Store.Messages(t.Context(), created.ID, 0, 100)
	if err != nil || messageErr != nil || len(threads) != 2 || len(messages) != 7 {
		t.Fatalf("identity update changed history threads=%d messages=%d err=%v/%v", len(threads), len(messages), err, messageErr)
	}
}

func TestAvatarValidationRejectsInvalidDesignsWithoutMutation(t *testing.T) {
	api, _ := startTest(t, &testFactory{})
	initial := profileRequest(t, api, http.MethodGet, "/agents/assistant", "", 200)
	for _, invalid := range []func(*contract.AvatarDesign){
		func(v *contract.AvatarDesign) { v.Shape = "circle" },
		func(v *contract.AvatarDesign) { v.Color = "#ffffff" },
		func(v *contract.AvatarDesign) { v.Expression = "angry" },
		func(v *contract.AvatarDesign) { v.Blocks[0].State = "unknown" },
		func(v *contract.AvatarDesign) { v.Blocks[0].ID = "" },
		func(v *contract.AvatarDesign) { v.Blocks[1].ID = v.Blocks[0].ID },
		func(v *contract.AvatarDesign) { v.Blocks[0].Duration = 0 },
		func(v *contract.AvatarDesign) { v.Blocks[0].Duration = 2.4 },
		func(v *contract.AvatarDesign) { v.Blocks[0].Duration = 10.1 },
		func(v *contract.AvatarDesign) {
			v.Blocks = []contract.AnimationBlock{}
			for i := 0; i < 201; i++ {
				v.Blocks = append(v.Blocks, contract.AnimationBlock{ID: fmt.Sprint(i), State: "idle", Duration: 1})
			}
		},
	} {
		avatar := avatarDesign()
		invalid(&avatar)
		avatarRequest(t, api, "/agents/assistant/avatar", avatar, 400)
		body, _ := json.Marshal(contract.CreateAgentRequest{Name: "Invalid", Avatar: &avatar})
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(string(body))))
		if recorder.Code != 400 {
			t.Fatalf("invalid initial avatar accepted status=%d", recorder.Code)
		}
	}
	profile := profileRequest(t, api, http.MethodGet, "/agents/assistant", "", 200)
	agents, err := api.Store.Agents(t.Context())
	if err != nil || len(agents) != 1 || !reflect.DeepEqual(profile.Agent.Avatar, initial.Agent.Avatar) {
		t.Fatalf("invalid designs mutated identity %+v agents=%d err=%v", profile, len(agents), err)
	}
	avatarRequest(t, api, "/agents/missing/avatar", avatarDesign(), 404)
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		avatar := avatarDesign()
		avatar.Blocks[0].Duration = value
		if validAvatar(avatar) {
			t.Fatal("nonfinite duration accepted")
		}
	}
}

func TestAvatarMinimumsMatchPinnedAnimationMeasurements(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "AvatarKit", "Tests", "AvatarKitTests", "Fixtures", "upstream.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cycle struct {
			Clamps []struct {
				State    string
				Duration float64
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, clamp := range fixture.Cycle.Clamps {
		avatar := avatarDesign()
		avatar.Blocks = []contract.AnimationBlock{{ID: "measured", State: clamp.State, Duration: clamp.Duration}}
		if !validAvatar(avatar) {
			t.Fatalf("measured duration rejected state=%s duration=%f", clamp.State, clamp.Duration)
		}
		avatar.Blocks[0].Duration -= 0.1
		if validAvatar(avatar) {
			t.Fatalf("shorter than measured minimum accepted state=%s", clamp.State)
		}
	}
}
