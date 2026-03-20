//go:build tinygo

package pubsub

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindHandlersTopic(h *Client, mach *am.Machine) error {
	return mach.BindHandlerMaps("Topic",
		TopicNegotiations(h), TopicFinals(h))
}

// TODO args
