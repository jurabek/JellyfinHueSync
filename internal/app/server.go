package app

import (
	"encoding/json"
	"net/http"

	"github.com/jurabek/JellyfinHueSync/internal/web"
)

// Handler returns the HTTP routes for the web UI and JSON API.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.State())
	})
	mux.HandleFunc("POST /api/discover", func(w http.ResponseWriter, r *http.Request) {
		a.Discover()
		writeJSON(w, http.StatusOK, map[string]bool{"started": true})
	})
	mux.HandleFunc("POST /api/identify", func(w http.ResponseWriter, r *http.Request) {
		var req identifyRequest
		if !decode(w, r, &req) {
			return
		}
		b, err := a.Identify(r.Context(), req.IP)
		if err != nil {
			writeError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toBridgeDTO(b))
	})
	mux.HandleFunc("POST /api/pair", func(w http.ResponseWriter, r *http.Request) {
		var req identifyRequest
		if !decode(w, r, &req) {
			return
		}
		if _, err := a.Pair(r.Context(), req.IP); err != nil {
			writeError(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"paired": true})
	})
	mux.HandleFunc("GET /api/areas", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, toAreaDTOs(a.RefreshAreas(r.Context())))
	})
	mux.HandleFunc("POST /api/area", func(w http.ResponseWriter, r *http.Request) {
		var req areaRequest
		if !decode(w, r, &req) {
			return
		}
		a.SelectArea(req.ID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/jellyfin", func(w http.ResponseWriter, r *http.Request) {
		var req jellyfinRequest
		if !decode(w, r, &req) {
			return
		}
		a.SaveJellyfin(req)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/jellyfin/test", func(w http.ResponseWriter, r *http.Request) {
		var req jellyfinRequest
		if !decode(w, r, &req) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": a.TestJellyfin(r.Context(), req.URL, req.APIKey)})
	})
	mux.HandleFunc("POST /api/options", func(w http.ResponseWriter, r *http.Request) {
		var req syncOptionsDTO
		if !decode(w, r, &req) {
			return
		}
		a.UpdateOptions(req)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/sync/start", func(w http.ResponseWriter, r *http.Request) {
		if msg := a.StartSync(); msg != "" {
			writeError(w, msg)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/sync/stop", func(w http.ResponseWriter, r *http.Request) {
		a.StopSync()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.Handle("/", http.FileServer(http.FS(web.Files)))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, apiError{Error: msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, "Invalid request body")
		return false
	}
	return true
}
