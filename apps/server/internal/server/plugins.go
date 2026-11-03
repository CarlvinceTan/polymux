package server

import (
	"errors"
	"net/http"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
)

func (s *Server) pluginRoutes() {
	s.mux.HandleFunc("GET /plugins", s.listPlugins)
	s.mux.HandleFunc("GET /plugins/market", s.pluginMarket)
	s.mux.HandleFunc("POST /plugins", s.installPlugin)
	s.mux.HandleFunc("PUT /plugins/layout", s.savePluginLayout)
	s.mux.HandleFunc("PATCH /plugins/{name}", s.updatePlugin)
	s.mux.HandleFunc("DELETE /plugins/{name}", s.removePlugin)
	s.mux.HandleFunc("GET /plugins/{name}/settings", s.pluginSettings)
	s.mux.HandleFunc("PUT /plugins/{name}/settings", s.savePluginSettings)
	s.mux.HandleFunc("GET /transports", s.transports)
	s.mux.HandleFunc("/plugins/{name}/api/{path...}", s.forwardPlugin)
	s.mux.HandleFunc("GET /plugins/{name}/ui/{path...}", s.pluginPage)
}

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	list, err := s.plugins.List(r.Context())
	if err != nil {
		fail(w, 500, "plugins", "Plugins could not be loaded.")
		return
	}
	write(w, 200, list)
}

func (s *Server) pluginMarket(w http.ResponseWriter, r *http.Request) {
	market, err := s.plugins.Market(r.Context())
	if err != nil {
		s.config.Log.Warn("read plugin market", "error", err)
		fail(w, 502, "market", "The marketplace can't be reached.")
		return
	}
	write(w, 200, market)
}

func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var input contract.InstallPlugin
	if !decode(w, r, &input) {
		return
	}
	plugin, err := s.plugins.Install(r.Context(), input.Name, input.Version)
	if err != nil {
		s.config.Log.Warn("install plugin", "plugin", input.Name, "error", err)
		fail(w, 409, "plugin", "This plugin can't be installed.")
		return
	}
	write(w, 202, plugin)
}

func (s *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var input contract.UpdatePlugin
	if !decode(w, r, &input) {
		return
	}
	plugin, err := s.plugins.SetChats(r.Context(), r.PathValue("name"), input.Chats)
	if errors.Is(err, plugins.ErrUnknown) {
		fail(w, 404, "not_found", err.Error())
		return
	}
	if err != nil {
		fail(w, 400, "invalid", "Those chats can't use this plugin.")
		return
	}
	write(w, 200, plugin)
}

func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	err := s.plugins.Remove(r.Context(), r.PathValue("name"))
	if errors.Is(err, plugins.ErrUnknown) {
		fail(w, 404, "not_found", err.Error())
		return
	}
	if err != nil {
		s.config.Log.Error("remove plugin", "plugin", r.PathValue("name"), "error", err)
		fail(w, 500, "plugin", "The plugin could not be removed.")
		return
	}
	w.WriteHeader(204)
}

func (s *Server) savePluginLayout(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var input contract.PluginLayout
	if !decode(w, r, &input) {
		return
	}
	layout, err := s.plugins.SetLayout(r.Context(), input)
	if err != nil {
		fail(w, 500, "plugins", "The layout could not be saved.")
		return
	}
	write(w, 200, layout)
}

func (s *Server) forwardPlugin(w http.ResponseWriter, r *http.Request) {
	if !s.plugins.Forward(w, r, r.PathValue("name"), r.PathValue("path")) {
		fail(w, 503, "unavailable", "This plugin isn't running.")
	}
}

func (s *Server) pluginPage(w http.ResponseWriter, r *http.Request) {
	if !s.plugins.ServePage(w, r, r.PathValue("name"), r.PathValue("path")) {
		fail(w, 404, "not_found", "This page doesn't exist.")
	}
}

func (s *Server) pluginSettings(w http.ResponseWriter, r *http.Request) {
	values, err := s.plugins.Settings(r.Context(), r.PathValue("name"))
	if errors.Is(err, plugins.ErrUnknown) {
		fail(w, 404, "not_found", err.Error())
		return
	}
	if err != nil {
		fail(w, 500, "plugins", "The settings could not be loaded.")
		return
	}
	write(w, 200, values)
}

func (s *Server) savePluginSettings(w http.ResponseWriter, r *http.Request) {
	if !nativeRequest(w, r) {
		return
	}
	var input map[string]any
	if !decode(w, r, &input) {
		return
	}
	values, err := s.plugins.SetSettings(r.Context(), r.PathValue("name"), input)
	if errors.Is(err, plugins.ErrUnknown) {
		fail(w, 404, "not_found", err.Error())
		return
	}
	if err != nil {
		fail(w, 400, "invalid", "Those settings can't be saved.")
		return
	}
	write(w, 200, values)
}

// transports lists where each transport plugin says this server can be reached.
func (s *Server) transports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, s.plugins.Transports(r.Context()))
}
