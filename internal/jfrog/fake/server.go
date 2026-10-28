// Package fake provides an in-memory JFrog Platform for tests. It implements
// the subset of the REST APIs the operator uses, with the same methods,
// status codes and create/update semantics as the real platform.
package fake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Token is the access token the fake server accepts.
const Token = "fake-token"

// Request records a request the server received.
type Request struct {
	Method string
	Path   string
}

// Server is an in-memory JFrog Platform.
type Server struct {
	*httptest.Server

	mu           sync.Mutex
	repositories map[string]map[string]any
	requests     []Request
	// failures maps "METHOD path" to a status code to return instead of handling the request.
	failures map[string]int
}

// NewServer starts a fake JFrog Platform. Close it when done.
func NewServer() *Server {
	s := &Server{
		repositories: map[string]map[string]any{},
		failures:     map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /artifactory/api/system/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"version": "7.104.5", "revision": "fake"})
	})
	mux.HandleFunc("GET /xray/api/v1/system/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"xray_version": "3.111.0", "xray_revision": "fake"})
	})
	mux.HandleFunc("GET /artifactory/api/repositories", s.listRepositories)
	mux.HandleFunc("GET /artifactory/api/repositories/{key}", s.getRepository)
	mux.HandleFunc("PUT /artifactory/api/repositories/{key}", s.createRepository)
	mux.HandleFunc("POST /artifactory/api/repositories/{key}", s.updateRepository)
	mux.HandleFunc("DELETE /artifactory/api/repositories/{key}", s.deleteRepository)
	s.Server = httptest.NewServer(s.middleware(mux))
	return s
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path})
		code, fail := s.failures[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+Token {
			writeError(w, http.StatusUnauthorized, "Bad credentials")
			return
		}
		if fail {
			writeError(w, code, "injected failure")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FailWith makes the server answer every request with the given method and
// path with code. A code of 0 removes the failure.
func (s *Server) FailWith(method, path string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if code == 0 {
		delete(s.failures, method+" "+path)
		return
	}
	s.failures[method+" "+path] = code
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// CountRequests returns how many requests with the given method and path were received.
func (s *Server) CountRequests(method, path string) int {
	n := 0
	for _, r := range s.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// Repository returns a copy of the stored repository configuration, or nil.
func (s *Server) Repository(key string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.repositories[key])
}

// SetRepository stores a repository configuration, bypassing the API. Use it
// to simulate changes made outside the operator.
func (s *Server) SetRepository(key string, cfg map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := clone(cfg)
	c["key"] = key
	s.repositories[key] = c
}

// DeleteRepositoryDirect removes a repository, bypassing the API.
func (s *Server) DeleteRepositoryDirect(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.repositories, key)
}

func (s *Server) listRepositories(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	for key, repo := range s.repositories {
		out = append(out, map[string]any{
			"key":         key,
			"type":        strings.ToUpper(repo["rclass"].(string)),
			"packageType": repo["packageType"],
			"url":         s.URL + "/artifactory/" + key,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRepository(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	repo, ok := s.repositories[r.PathValue("key")]
	repo = clone(repo)
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "Bad Request")
		return
	}
	delete(repo, "password")
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) createRepository(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var cfg map[string]any
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rclass, _ := cfg["rclass"].(string)
	switch rclass {
	case "local", "remote", "virtual", "federated":
	default:
		writeError(w, http.StatusBadRequest, "Invalid rclass")
		return
	}
	if pt, _ := cfg["packageType"].(string); pt == "" {
		writeError(w, http.StatusBadRequest, "packageType is required")
		return
	}
	if rclass == "remote" {
		if u, _ := cfg["url"].(string); u == "" {
			writeError(w, http.StatusBadRequest, "url is required")
			return
		}
	}
	if k, ok := cfg["key"].(string); ok && k != key {
		writeError(w, http.StatusBadRequest, "key in path and body differ")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.repositories[key]; exists {
		writeError(w, http.StatusBadRequest, "Case insensitive repository key already exists")
		return
	}
	cfg["key"] = key
	applyRepositoryDefaults(cfg)
	s.repositories[key] = cfg
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Successfully created repository '" + key + "'"))
}

func (s *Server) updateRepository(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var cfg map[string]any
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.repositories[key]
	if !ok {
		writeError(w, http.StatusBadRequest, "Repository "+key+" does not exist")
		return
	}
	for _, immutable := range []string{"rclass", "packageType"} {
		if v, ok := cfg[immutable]; ok && v != existing[immutable] {
			writeError(w, http.StatusBadRequest, immutable+" cannot be changed")
			return
		}
	}
	for k, v := range cfg {
		existing[k] = v
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Repository " + key + " update successfully."))
}

func (s *Server) deleteRepository(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.repositories[key]; !ok {
		writeError(w, http.StatusNotFound, "Repository "+key+" does not exist")
		return
	}
	delete(s.repositories, key)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Repository '" + key + "' and all its content have been removed successfully."))
}

// applyRepositoryDefaults mimics Artifactory returning defaults for unset fields.
func applyRepositoryDefaults(cfg map[string]any) {
	defaults := map[string]any{
		"description":     "",
		"notes":           "",
		"includesPattern": "**/*",
		"excludesPattern": "",
		"xrayIndex":       false,
		"repoLayoutRef":   "simple-default",
	}
	for k, v := range defaults {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	if cfg["rclass"] == "virtual" {
		if _, ok := cfg["repositories"]; !ok {
			cfg["repositories"] = []any{}
		}
	}
}

func clone(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error in the format Artifactory uses.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"errors": []map[string]any{{"status": code, "message": strings.TrimSpace(msg)}}})
}
