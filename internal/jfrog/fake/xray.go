package fake

import (
	"encoding/json"
	"net/http"
)

// Xray collections.
const (
	XrayPolicies = "xray-policies"
	XrayWatches  = "xray-watches"
)

// registerXray adds the Xray policy and watch endpoints. Both are created with
// POST on the collection, named in the body, and replaced with PUT.
func (s *Server) registerXray(mux *http.ServeMux) {
	for _, c := range []struct {
		name, path string
		nameOf     func(map[string]any) string
	}{
		{XrayPolicies, "/xray/api/v2/policies", func(o map[string]any) string { n, _ := o["name"].(string); return n }},
		{XrayWatches, "/xray/api/v2/watches", watchName},
	} {
		mux.HandleFunc("GET "+c.path+"/{name}", func(w http.ResponseWriter, r *http.Request) { s.getObject(w, r, c.name) })
		mux.HandleFunc("DELETE "+c.path+"/{name}", func(w http.ResponseWriter, r *http.Request) { s.deleteObject(w, r, c.name) })
		mux.HandleFunc("PUT "+c.path+"/{name}", func(w http.ResponseWriter, r *http.Request) { s.updateObject(w, r, c.name, false) })
		mux.HandleFunc("POST "+c.path, func(w http.ResponseWriter, r *http.Request) {
			var obj map[string]any
			if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			name := c.nameOf(obj)
			if name == "" {
				writeError(w, http.StatusBadRequest, "name is required")
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, exists := s.collection(c.name)[name]; exists {
				writeError(w, http.StatusConflict, name+" already exists")
				return
			}
			if c.name == XrayPolicies {
				obj["author"] = "admin"
			}
			s.collection(c.name)[name] = obj
			w.WriteHeader(http.StatusCreated)
		})
	}
}

func watchName(o map[string]any) string {
	general, _ := o["general_data"].(map[string]any)
	n, _ := general["name"].(string)
	return n
}

// decorateWatch adds the fields Xray returns on top of what was sent.
func decorateWatch(o map[string]any) {
	pr, _ := o["project_resources"].(map[string]any)
	resources, _ := pr["resources"].([]any)
	for _, r := range resources {
		if m, ok := r.(map[string]any); ok && m["type"] == "repository" {
			m["repo_type"] = "local"
		}
	}
}
