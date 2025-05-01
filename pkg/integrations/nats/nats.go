package nats

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/pancsta/asyncmachine-go/pkg/integrations"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ExposeMachine exposes a state machine to NATS for requests of type
// - GetterReq
// - MutationReq
// - WaitingReq
// with responses of type
// - GetterResp
// - MutationResp
// - WaitingResp
//
// Each state machine subscribed to a dedicated subtopic "[topic].[machineID]".
// Optional [queue] allows to load-balance requests across multiple subscribers.
// ExposeMachine allocates a goroutine for GC blocked by ctx.
func ExposeMachine(
	ctx context.Context, mach am.Api, nc *nats.Conn, topic, queue string,
) error {
	var (
		sub1 *nats.Subscription
		err  error
	)

	bind := func(msg *nats.Msg) {
		dispatcher(ctx, nc, mach, msg)
	}

	if queue != "" {
		sub1, err = nc.QueueSubscribe(topic, queue, bind)
	} else {
		sub1, err = nc.Subscribe(topic, bind)
	}

	if err != nil {
		return err
	}

	// dedicated subtopic for this machine
	sub2, err := nc.Subscribe(topic+"."+mach.Id(), bind)
	if err != nil {
		_ = sub1.Unsubscribe()
		return err
	}

	// dispose with ctx
	go func() {
		<-ctx.Done()
		_ = sub1.Unsubscribe()
		_ = sub2.Unsubscribe()
	}()

	return err
}

// Add is a helper to add a list of states to machine machID, exposed
// under [topic]. It will block until the response or the context expires.
func Add(
	ctx context.Context, nc *nats.Conn, topic, machID string, states am.S,
	args am.A) (am.Result, error) {

	// create the request
	req := integrations.NewMutationReq()
	req.Add = states
	req.Args = args
	reqJs, err := json.Marshal(req)
	if err != nil {
		return am.Canceled, err
	}

	msg, err := nc.RequestWithContext(ctx, topic+"."+machID, reqJs)
	if err != nil {
		return am.Canceled, err
	}

	var resp integrations.MutationResp
	err = json.Unmarshal(msg.Data, &resp)

	return resp.Result, err
}

// Remove is a helper to remove a list of states from machine machID,
// exposed under [topic]. It will block until the response or the context
// expires.
func Remove(
	ctx context.Context, nc *nats.Conn, machID, topic string, states am.S,
	args am.A,
) (am.Result, error) {

	// create the request
	req := integrations.NewMutationReq()
	req.Remove = states
	req.Args = args
	reqJs, err := json.Marshal(req)
	if err != nil {
		return am.Canceled, err
	}

	msg, err := nc.RequestWithContext(ctx, topic+"."+machID, reqJs)
	if err != nil {
		return am.Canceled, err
	}

	var resp integrations.MutationResp
	err = json.Unmarshal(msg.Data, &resp)

	return resp.Result, err
}

// UTILS

func dispatcher(
	ctx context.Context, nc *nats.Conn, mach am.Api, msg *nats.Msg) {
	var (
		j    []byte
		err0 error
	)

	// check if this is something for us
	msgKind := integrations.MsgKindReq{}
	if err := json.Unmarshal(msg.Data, &msgKind); err != nil ||
		!integrations.KindEnum.Contains(msgKind.Kind) {

		// no match, exit
		return
	}

	get := &integrations.GetterReq{}
	mut := &integrations.MutationReq{}
	wait := &integrations.WaitingReq{}

	switch msgKind.Kind {
	case integrations.KindReqGetter:
		if err0 = json.Unmarshal(msg.Data, get); err0 == nil {
			resp, err := integrations.HandlerGetter(ctx, mach, get)
			if err != nil {
				err0 = err
			} else {
				j, err0 = json.Marshal(resp)
			}
		}

	case integrations.KindReqMutation:
		if err0 = json.Unmarshal(msg.Data, mut); err0 == nil {
			resp, err := integrations.HandlerMutation(ctx, mach, mut)
			if err != nil {
				err0 = err
			} else {
				j, err0 = json.Marshal(resp)
			}
		}

	case integrations.KindReqWaiting:
		if err0 = json.Unmarshal(msg.Data, wait); err0 == nil {
			resp, err := integrations.HandlerWaiting(ctx, mach, wait)
			if err != nil {
				err0 = err
			} else {
				j, err0 = json.Marshal(resp)
			}
		}
	}

	// internal err
	if err0 != nil {
		mach.AddErr(err0, nil)
		return
	}

	// opt response
	if j != nil {
		// publish for async replies
		if msgKind.Kind == integrations.KindReqWaiting {
			err0 = nc.Publish(msg.Subject, j)

			// response to sync request
		} else {
			err0 = msg.Respond(j)
		}
		if err0 != nil {
			mach.AddErr(err0, nil)
		}
	}
}
