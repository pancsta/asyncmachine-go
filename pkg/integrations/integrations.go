//nolint:lll
package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/orsinium-labs/enum"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Kind enum

type Kind enum.Member[string]

var (
	KindReqGetter    = Kind{"am_req_getter"}
	KindReqMutation  = Kind{"am_req_mutation"}
	KindReqWaiting   = Kind{"am_req_waiting"}
	KindRespGetter   = Kind{"am_resp_getter"}
	KindRespMutation = Kind{"am_resp_mutation"}
	KindRespWaiting  = Kind{"am_resp_waiting"}
	KindEnum         = enum.New(KindReqGetter, KindReqMutation, KindReqWaiting,
		KindRespGetter, KindRespMutation, KindRespWaiting)
)

// flatten to a string in JSON

func (k *Kind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.Value)
}

func (k *Kind) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}

	// success
	k.Value = s
	return nil
}

// TODO validate JSON also for Req and Resp structs

// MsgKindReq is a decoding helper.
type MsgKindReq struct {
	// The kind of the request.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_req_getter,enum=am_req_mutation,enum=am_req_waiting"`
}

// MsgKindResp is a decoding helper.
type MsgKindResp struct {
	// The kind of the response.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_resp_waiting,enum=am_resp_mutation,enum=am_resp_getter"`
}

// MUTATION

type MutationReq struct {
	// The kind of the request.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_req_mutation"`
	// The states to add to the state machine.
	Add am.S `json:"add,omitempty" jsonschema:"oneof_required=add"`
	// The states to remove from the state machine.
	Remove am.S `json:"remove,omitempty" jsonschema:"oneof_required=remove"`
	// Arguments passed to transition handlers.
	Args map[string]any `json:"args,omitempty"`

	// TODO machine filters to narrow down the request on group channels
}

type MutationResp struct {
	// The kind of the request.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_req_mutation"`
	// The result of the mutation request.
	Result am.Result `json:"result"`
}

// WAITING

type WaitingReq struct {
	// The kind of the request.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_req_waiting"`
	// The states to wait for, the default is to all states being active simultaneously (if no time passed).
	States am.S `json:"states,omitempty" jsonschema:"oneof_required=states"`
	// The states names to wait for to be inactive. Ignores the Time field.
	StatesNot am.S `json:"states_not,omitempty" jsonschema:"oneof_required=statesNot"`
	// The specific (minimal) time to wait for.
	Time am.Time `json:"time,omitempty"`
	// TODO WhenArgs
}

type WaitingRespUnsafe struct {
	// The kind of the response.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_resp_waiting"`
	// The ID of the state machine.
	MachId string `json:"mach_id"`
	// The active states waited for. If time is empty, all these states are active simultaneously.
	States am.S `json:"states,omitempty" jsonschema:"oneof_required=states"`
	// The inactive states waited for.
	StatesNot am.S `json:"states_not,omitempty" jsonschema:"oneof_required=states"`
	// The requested machine time (the current one may be higher).
	Time am.Time `json:"time,omitempty"`
}

type WaitingResp struct {
	WaitingRespUnsafe
}

func (w *WaitingResp) UnmarshalJSON(b []byte) error {
	resp := WaitingRespUnsafe{}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	if resp.Kind != KindRespWaiting {
		return errors.New("wrong response kind")
	}
	w.WaitingRespUnsafe = resp

	// TODO more validation

	return nil
}

// GETTER

// GetterReq is a generic request, which results in GetterResp with
// respective fields filled out.
type GetterReq struct {
	// The kind of the request.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_req_getter"`
	// Request ticks of the passed states
	Time am.S `json:"time,omitempty"`
	// Request the sum of ticks of the passed states
	TimeSum am.S `json:"time_sum,omitempty"`
	// Request named clocks of the passed states
	Clocks am.S `json:"clocks,omitempty"`
	// Request the tags of the state machine
	Tags bool `json:"tags,omitempty"`
	// Request an importable version of the state machine
	Export bool `json:"export,omitempty"`
	// Request the ID of the state machine
	Id bool `json:"id,omitempty"`
	// Request the ID of the parent state machine
	ParentId bool `json:"parent_id,omitempty"`
}

// GetterResp is a response to GetterReq.
type GetterResp struct {
	// The kind of the response.
	Kind Kind `json:"kind" jsonschema:"required,enum=am_resp_getter"`
	// The ID of the state machine.
	MachId string `json:"mach_id,omitempty"`
	// The ticks of the passed states
	Time am.Time `json:"time,omitempty"`
	// The sum of ticks of the passed states
	TimeSum int `json:"time_sum,omitempty"`
	// The named clocks of the passed states
	Clocks am.Clock `json:"clocks,omitempty"`
	// The tags of the state machine
	Tags []string `json:"tags,omitempty"`
	// The importable version of the state machine
	Export *am.Serialized `json:"export,omitempty"`
	// The ID of the state machine
	Id string `json:"id,omitempty"`
	// The ID of the parent state machine
	ParentId string `json:"parent_id,omitempty"`
}

// UTILS & HANDLERS

// NewGetterReq creates a new getter request.
func NewGetterReq() *GetterReq {
	return &GetterReq{
		Kind: KindReqGetter,
	}
}

// NewMutationReq creates a new mutation request.
// TODO sugar for NewAddReq and NewRemoveReq
func NewMutationReq() *MutationReq {
	return &MutationReq{
		Kind: KindReqMutation,
	}
}

// NewWaitingReq creates a new waiting request.
func NewWaitingReq() *WaitingReq {
	return &WaitingReq{
		Kind: KindReqWaiting,
	}
}

func HandlerWaiting(
	ctx context.Context, mach am.Api, req *WaitingReq,
) (*WaitingResp, error) {
	resp := &WaitingResp{WaitingRespUnsafe{Kind: KindRespWaiting}}

	// validate
	lenTime := len(req.Time)
	lenStates := max(len(req.States), len(req.StatesNot))
	if lenStates == 0 {
		return nil, errors.New("waiting states missing")
	} else if lenTime > 0 && lenTime != len(req.States) {
		return nil, errors.New("waiting states and time length mismatch")
	}

	if !mach.Has(req.States) {
		return nil, fmt.Errorf("%w: (%s) for %s", am.ErrStateMissing, req.States,
			mach.Id())
	}

	// subscribe
	var sub <-chan struct{}
	if len(req.StatesNot) > 0 {
		sub = mach.WhenNot(req.StatesNot, ctx)
		resp.StatesNot = req.StatesNot
	} else if lenTime > 0 {
		sub = mach.WhenTime(req.States, req.Time, ctx)
	} else {
		sub = mach.When(req.States, ctx)
		resp.States = req.States
	}

	// wait
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
		// pass
	case <-sub:
	}

	// update the response and return
	resp.MachId = mach.Id()
	if lenTime > 0 {
		resp.Time = mach.Time(req.States)
	}
	return resp, nil
}

func HandlerMutation(
	ctx context.Context, mach am.Api, req *MutationReq,
) (*MutationResp, error) {
	resp := &MutationResp{Kind: KindRespMutation}
	if len(req.Add) > 0 && len(req.Remove) == 0 {

		if !mach.Has(req.Add) {
			return nil, fmt.Errorf("%w: (%s) for %s", am.ErrStateMissing, req.Add,
				mach.Id())
		}
		resp.Result = mach.Add(req.Add, req.Args)
	} else if len(req.Remove) > 0 && len(req.Add) == 0 {

		if !mach.Has(req.Remove) {
			return nil, fmt.Errorf("%w: (%s) for %s", am.ErrStateMissing, req.Remove,
				mach.Id())
		}
		resp.Result = mach.Remove(req.Remove, req.Args)
	} else if len(req.Add) > 0 && len(req.Remove) > 0 {
		return nil, errors.New("mutation can be add or remove, not both")
	} else {
		return nil, errors.New("mutation state names missing")
	}

	return resp, nil
}

func HandlerGetter(
	ctx context.Context, mach am.Api, req *GetterReq,
) (*GetterResp, error) {
	resp := &GetterResp{Kind: KindRespGetter}
	if req.Id {
		resp.Id = mach.Id()
	}
	if req.ParentId {
		resp.ParentId = mach.ParentId()
	}
	if req.Tags {
		resp.Tags = mach.Tags()
	}
	if req.Export {
		resp.Export = mach.Export()
	}
	if len(req.Time) > 0 {
		resp.Time = mach.Time(req.Time)
	}
	if len(req.Clocks) > 0 {
		resp.Clocks = mach.Clock(req.Clocks)
	}
	if len(req.TimeSum) > 0 {
		resp.TimeSum = int(mach.Time(req.TimeSum).Sum(nil))
	}

	return resp, nil
}
