package fake

import (
	"encoding/json"
	"net/http"
)

// Collections of the security API.
const (
	Users       = "users"
	Groups      = "groups"
	Permissions = "permissions"
)

// secretFields are accepted from clients but never returned.
var secretFields = map[string][]string{Users: {"password"}}

// registerSecurity adds the users, groups and permission target endpoints.
// Users and groups are created with PUT and updated with POST, permission
// targets the other way around, as in Artifactory.
func (s *Server) registerSecurity(mux *http.ServeMux) {
	for _, c := range []struct {
		name, path, create, update string
	}{
		{Users, "/artifactory/api/security/users/{name}", http.MethodPut, http.MethodPost},
		{Groups, "/artifactory/api/security/groups/{name}", http.MethodPut, http.MethodPost},
		{Permissions, "/artifactory/api/v2/security/permissions/{name}", http.MethodPost, http.MethodPut},
	} {
		mux.HandleFunc("GET "+c.path, func(w http.ResponseWriter, r *http.Request) { s.getObject(w, r, c.name) })
		mux.HandleFunc(c.create+" "+c.path, func(w http.ResponseWriter, r *http.Request) { s.createObject(w, r, c.name) })
		mux.HandleFunc(c.update+" "+c.path, func(w http.ResponseWriter, r *http.Request) {
			// Users and groups are merged, permission targets replaced.
			s.updateObject(w, r, c.name, c.name != Permissions)
		})
		mux.HandleFunc("DELETE "+c.path, func(w http.ResponseWriter, r *http.Request) { s.deleteObject(w, r, c.name) })
	}
}

// Object returns a copy of a stored security object (including secrets), or nil.
func (s *Server) Object(collection, name string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.objects[collection][name])
}

// SetObject stores a security object, bypassing the API.
func (s *Server) SetObject(collection, name string, obj map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := clone(obj)
	c["name"] = name
	s.collection(collection)[name] = c
}

// DeleteObjectDirect removes a security object, bypassing the API.
func (s *Server) DeleteObjectDirect(collection, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collection(collection), name)
}

// collection must be called with s.mu held.
func (s *Server) collection(name string) map[string]map[string]any {
	if s.objects[name] == nil {
		s.objects[name] = map[string]map[string]any{}
	}
	return s.objects[name]
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request, collection string) {
	s.mu.Lock()
	obj, ok := s.collection(collection)[r.PathValue("name")]
	obj = clone(obj)
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, collection+" "+r.PathValue("name")+" does not exist")
		return
	}
	for _, f := range secretFields[collection] {
		delete(obj, f)
	}
	if collection == XrayWatches {
		decorateWatch(obj)
	}
	writeJSON(w, http.StatusOK, obj)
}

func (s *Server) createObject(w http.ResponseWriter, r *http.Request, collection string) {
	name := r.PathValue("name")
	var obj map[string]any
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := validate(collection, obj, true); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.collection(collection)[name]; exists {
		if collection == Permissions {
			writeError(w, http.StatusConflict, "permission target "+name+" already exists")
			return
		}
		// Artifactory's PUT on an existing user or group replaces it.
	}
	obj["name"] = name
	applySecurityDefaults(collection, obj)
	s.collection(collection)[name] = obj
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) updateObject(w http.ResponseWriter, r *http.Request, collection string, merge bool) {
	name := r.PathValue("name")
	var obj map[string]any
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := validate(collection, obj, false); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.collection(collection)[name]
	if !ok {
		writeError(w, http.StatusNotFound, collection+" "+name+" does not exist")
		return
	}
	if merge {
		for k, v := range obj {
			existing[k] = v
		}
	} else {
		if collection == Permissions || collection == XrayPolicies {
			obj["name"] = name
		}
		s.collection(collection)[name] = obj
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) deleteObject(w http.ResponseWriter, r *http.Request, collection string) {
	name := r.PathValue("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.collection(collection)[name]; !ok {
		writeError(w, http.StatusNotFound, collection+" "+name+" does not exist")
		return
	}
	delete(s.collection(collection), name)
	w.WriteHeader(http.StatusOK)
}

func validate(collection string, obj map[string]any, create bool) string {
	if collection != Users || !create {
		return ""
	}
	if e, _ := obj["email"].(string); e == "" {
		return "email is required"
	}
	if disabled, _ := obj["internalPasswordDisabled"].(bool); !disabled {
		if p, _ := obj["password"].(string); p == "" {
			return "password is required"
		}
	}
	return ""
}

func applySecurityDefaults(collection string, obj map[string]any) {
	var defaults map[string]any
	switch collection {
	case Users:
		defaults = map[string]any{
			"admin": false, "profileUpdatable": true, "disableUIAccess": false,
			"internalPasswordDisabled": false, "realm": "internal", "groups": []any{"readers"},
		}
	case Groups:
		defaults = map[string]any{"description": "", "autoJoin": false, "adminPrivileges": false, "realm": "internal"}
	}
	for k, v := range defaults {
		if _, ok := obj[k]; !ok {
			obj[k] = v
		}
	}
}
