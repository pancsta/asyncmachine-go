// Package rest exposes asyncmachines over a JSON REST API.
//
// Routes:
//   - GET  /mach          list machines as am.Serialized, with filters
//   - GET  /mach/{id}     machine as am.Serialized
//   - POST /mach          create a machine from a schema (and optional state)
//   - POST /mach/{id}     mutate states (integrations.MutationReq)
//   - POST /mach/{id}/wait wait for states (integrations.WaitingReq)
//   - POST /dispose/{id}  dispose a machine
//
// GET /mach accepts query params matching [amhelp.MachGroupFilter]: id,
// id_substr, id_regexp, id_prefix, id_suffix, parent, mtime_min, mtime_max,
// mtime_states, active, inactive, limit, start_idx. List params accept both
// CSV and repeated values.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/pkg/integrations"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Server exposes a group of state machines over HTTP.
type Server struct {
	ctx     context.Context
	machs   amhelp.MachGroup
	mu      sync.RWMutex
	handler http.Handler
	http    *http.Server
	ln      net.Listener
}

// NewMachReq is a request to create a state machine. It's compatible with
// [am.Serialized], so an exported machine can be re-created by adding a
// schema.
type NewMachReq struct {
	am.Serialized
	// Schema defines the new state machine.
	Schema am.Schema `json:"schema"`
}

// ErrResp is a JSON error response.
type ErrResp struct {
	Error string `json:"error"`
}

// New creates a REST server for the passed machines.
func New(ctx context.Context, machs amhelp.MachGroup) *Server {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &Server{
		ctx:   ctx,
		machs: slices.Clone(machs),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /mach", s.machsRpcJson)
	mux.HandleFunc("POST /mach", s.createMachRpcJson)
	mux.HandleFunc("GET /mach/{id}", s.machRpcJson)
	mux.HandleFunc("POST /mach/{id}", s.mutationRpcJson)
	mux.HandleFunc("POST /mach/{id}/wait", s.waitingRpcJson)
	mux.HandleFunc("POST /dispose/{id}", s.disposeRpcJson)
	s.handler = mux

	return s
}

// Handler returns the HTTP handler of this server.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ListenAndServe listens on addr and serves the REST API, blocking until the
// server is closed.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: s.handler}
	s.mu.Lock()
	s.ln = ln
	s.http = srv
	s.mu.Unlock()

	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close closes the server.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Server) machsRpcJson(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.RLock()
	group := slices.Clone(s.machs)
	s.mu.RUnlock()

	filtered := group.Filter(filter)
	machs := make([]*am.Serialized, len(filtered))
	for i, mach := range filtered {
		machs[i] = integrations.Serialize(mach)
	}

	writeJson(w, http.StatusOK, machs)
}

func (s *Server) machRpcJson(w http.ResponseWriter, r *http.Request) {
	mach := s.mach(r.PathValue("id"))
	if mach == nil {
		writeErr(w, http.StatusNotFound, "machine not found")
		return
	}

	writeJson(w, http.StatusOK, integrations.Serialize(mach))
}

func (s *Server) createMachRpcJson(w http.ResponseWriter, r *http.Request) {
	req := &NewMachReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Schema) == 0 {
		writeErr(w, http.StatusBadRequest, "schema required")
		return
	}
	if _, err := req.Schema.Parse(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID != "" && s.mach(req.ID) != nil {
		writeErr(w, http.StatusConflict, "machine already exists")
		return
	}

	mach, err := integrations.NewMach(s.ctx, req.Schema, &req.Serialized)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	s.machs = append(s.machs, mach)
	s.mu.Unlock()

	writeJson(w, http.StatusCreated, integrations.Serialize(mach))
}

func (s *Server) mutationRpcJson(w http.ResponseWriter, r *http.Request) {
	mach := s.mach(r.PathValue("id"))
	if mach == nil {
		writeErr(w, http.StatusNotFound, "machine not found")
		return
	}

	req := &integrations.MutationReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Kind.Value == "" {
		req.Kind = integrations.KindReqMutation
	}

	resp, err := integrations.HandlerMutation(r.Context(), mach, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJson(w, http.StatusOK, resp)
}

func (s *Server) waitingRpcJson(w http.ResponseWriter, r *http.Request) {
	mach := s.mach(r.PathValue("id"))
	if mach == nil {
		writeErr(w, http.StatusNotFound, "machine not found")
		return
	}

	req := &integrations.WaitingReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Kind.Value == "" {
		req.Kind = integrations.KindReqWaiting
	}

	resp, err := integrations.HandlerWaiting(r.Context(), mach, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJson(w, http.StatusOK, resp)
}

func (s *Server) disposeRpcJson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.Lock()
	idx := slices.IndexFunc(s.machs, func(mach am.Api) bool {
		return mach.Id() == id
	})
	var mach am.Api
	if idx >= 0 {
		mach = s.machs[idx]
		s.machs = slices.Delete(s.machs, idx, idx+1)
	}
	s.mu.Unlock()

	if mach == nil {
		writeErr(w, http.StatusNotFound, "machine not found")
		return
	}

	mach.Dispose()
	writeJson(w, http.StatusOK, map[string]any{
		"id":       id,
		"disposed": true,
	})
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func (s *Server) mach(id string) am.Api {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, mach := range s.machs {
		if mach.Id() == id {
			return mach
		}
	}
	return nil
}

func parseFilter(r *http.Request) (*amhelp.MachGroupFilter, error) {
	q := r.URL.Query()
	f := &amhelp.MachGroupFilter{
		IdExact:        q.Get("id"),
		IdSubstr:       q.Get("id_substr"),
		IdPrefix:       q.Get("id_prefix"),
		IdSuffix:       q.Get("id_suffix"),
		Parent:         q.Get("parent"),
		MtimeStates:    csv(q["mtime_states"]),
		StatesActive:   csv(q["active"]),
		StatesInactive: csv(q["inactive"]),
	}

	if re := q.Get("id_regexp"); re != "" {
		var err error
		f.IdRegexp, err = regexp.Compile(re)
		if err != nil {
			return nil, fmt.Errorf("id_regexp: %w", err)
		}
	}

	var err error
	if f.MtimeMin, err = parseUint(q.Get("mtime_min")); err != nil {
		return nil, fmt.Errorf("mtime_min: %w", err)
	}
	if f.MtimeMax, err = parseUint(q.Get("mtime_max")); err != nil {
		return nil, fmt.Errorf("mtime_max: %w", err)
	}
	if f.Limit, err = parseInt(q.Get("limit")); err != nil {
		return nil, fmt.Errorf("limit: %w", err)
	}
	if f.StartIdx, err = parseInt(q.Get("start_idx")); err != nil {
		return nil, fmt.Errorf("start_idx: %w", err)
	}

	return f, nil
}

func csv(values []string) am.S {
	var ret am.S
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				ret = append(ret, part)
			}
		}
	}
	return ret
}

func parseUint(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseUint(s, 10, 64)
}

func parseInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func writeJson(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJson(w, code, &ErrResp{Error: msg})
}
