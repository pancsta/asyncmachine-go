// Package grpc exposes asyncmachines over gRPC. It's an equivalent of
// /pkg/integrations/rest, using the same machine group and filters.
package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"slices"
	"sync"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/pkg/integrations"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Server exposes a group of state machines over gRPC.
type Server struct {
	UnimplementedAsyncmachineServiceServer

	ctx   context.Context
	machs amhelp.MachGroup
	mu    sync.RWMutex
	grpc  *ggrpc.Server
	ln    net.Listener
}

// New creates a gRPC server for the passed machines.
func New(ctx context.Context, machs amhelp.MachGroup) *Server {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &Server{
		ctx:   ctx,
		machs: slices.Clone(machs),
	}
	s.grpc = ggrpc.NewServer()
	RegisterAsyncmachineServiceServer(s.grpc, s)

	return s
}

// ListenAndServe listens on addr and serves the gRPC API, blocking until the
// server is closed.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve serves the gRPC API on ln, blocking until the server is closed.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	err := s.grpc.Serve(ln)
	if errors.Is(err, ggrpc.ErrServerStopped) {
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
	s.grpc.Stop()
	return nil
}

// ///// ///// /////

// ///// API

// ///// ///// /////

func (s *Server) ListMachines(
	ctx context.Context, req *ListMachinesReq,
) (*ListMachinesResp, error) {
	filter, err := req.filter()
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	group := slices.Clone(s.machs)
	s.mu.RUnlock()

	filtered := group.Filter(filter)
	machs := make([]*Serialized, len(filtered))
	for i, mach := range filtered {
		machs[i] = toProto(integrations.Serialize(mach))
	}

	return &ListMachinesResp{Machines: machs}, nil
}

func (s *Server) GetMachine(
	ctx context.Context, req *GetMachineReq,
) (*Serialized, error) {
	mach := s.mach(req.Id)
	if mach == nil {
		return nil, status.Errorf(codes.NotFound, "machine not found: %s", req.Id)
	}

	return toProto(integrations.Serialize(mach)), nil
}

func (s *Server) CreateMachine(
	ctx context.Context, req *CreateMachineReq,
) (*Serialized, error) {
	schema, err := schemaFromStruct(req.Schema)
	if err != nil {
		return nil, err
	}
	ser := fromProto(req.Serialized)
	if ser != nil && ser.ID != "" && s.mach(ser.ID) != nil {
		return nil, status.Errorf(codes.AlreadyExists,
			"machine already exists: %s", ser.ID)
	}
	if s.ctx.Err() != nil {
		return nil, status.Error(codes.Unavailable, s.ctx.Err().Error())
	}

	mach, err := integrations.NewMach(s.ctx, schema, ser)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	s.mu.Lock()
	s.machs = append(s.machs, mach)
	s.mu.Unlock()

	return toProto(integrations.Serialize(mach)), nil
}

func (s *Server) Mutate(
	ctx context.Context, req *MutateReq,
) (*MutateResp, error) {
	mach := s.mach(req.Id)
	if mach == nil {
		return nil, status.Errorf(codes.NotFound, "machine not found: %s", req.Id)
	}

	mreq := integrations.NewMutationReq()
	mreq.Add = req.Add
	mreq.Remove = req.Remove
	if req.Args != nil {
		mreq.Args = req.Args.AsMap()
	}

	resp, err := integrations.HandlerMutation(ctx, mach, mreq)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &MutateResp{Result: int64(resp.Result)}, nil
}

func (s *Server) Wait(
	ctx context.Context, req *WaitReq,
) (*WaitResp, error) {
	mach := s.mach(req.Id)
	if mach == nil {
		return nil, status.Errorf(codes.NotFound, "machine not found: %s", req.Id)
	}

	wreq := integrations.NewWaitingReq()
	wreq.States = req.States
	wreq.StatesNot = req.StatesNot
	wreq.Time = req.Time

	resp, err := integrations.HandlerWaiting(ctx, mach, wreq)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {

			return nil, status.FromContextError(err).Err()
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &WaitResp{
		Id:        resp.MachId,
		States:    resp.States,
		StatesNot: resp.StatesNot,
		Time:      resp.Time,
	}, nil
}

func (s *Server) Dispose(
	ctx context.Context, req *DisposeReq,
) (*DisposeResp, error) {
	mach := s.removeMach(req.Id)
	if mach == nil {
		return nil, status.Errorf(codes.NotFound, "machine not found: %s", req.Id)
	}

	mach.Dispose()
	return &DisposeResp{Disposed: true}, nil
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func (req *ListMachinesReq) filter() (*amhelp.MachGroupFilter, error) {
	f := &amhelp.MachGroupFilter{
		IdExact:        req.Id,
		IdSubstr:       req.IdSubstr,
		IdPrefix:       req.IdPrefix,
		IdSuffix:       req.IdSuffix,
		Parent:         req.Parent,
		MtimeMin:       req.MtimeMin,
		MtimeMax:       req.MtimeMax,
		MtimeStates:    req.MtimeStates,
		StatesActive:   req.Active,
		StatesInactive: req.Inactive,
		Limit:          int(req.Limit),
		StartIdx:       int(req.StartIdx),
	}

	if req.IdRegexp != "" {
		re, err := regexp.Compile(req.IdRegexp)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument,
				"id_regexp: %s", err)
		}
		f.IdRegexp = re
	}

	return f, nil
}

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

func (s *Server) removeMach(id string) am.Api {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := slices.IndexFunc(s.machs, func(mach am.Api) bool {
		return mach.Id() == id
	})
	if idx < 0 {
		return nil
	}

	mach := s.machs[idx]
	s.machs = slices.Delete(s.machs, idx, idx+1)
	return mach
}

func schemaFromStruct(st *structpb.Struct) (am.Schema, error) {
	if st == nil || len(st.Fields) == 0 {
		return nil, status.Error(codes.InvalidArgument, "schema required")
	}

	data, err := json.Marshal(st.AsMap())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	var schema am.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return schema, nil
}

func toProto(ser *am.Serialized) *Serialized {
	return &Serialized{
		Id:          ser.ID,
		StateNames:  ser.StateNames,
		Time:        ser.Time,
		QueueTick:   ser.QueueTick,
		MachineTick: ser.MachineTick,
		Tags:        ser.Tags,
	}
}

func fromProto(ser *Serialized) *am.Serialized {
	if ser == nil {
		return nil
	}

	return &am.Serialized{
		ID:          ser.Id,
		StateNames:  ser.StateNames,
		Time:        ser.Time,
		QueueTick:   ser.QueueTick,
		MachineTick: ser.MachineTick,
		Tags:        ser.Tags,
	}
}
