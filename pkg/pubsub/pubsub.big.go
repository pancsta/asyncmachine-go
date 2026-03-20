//go:build !tinygo

package pubsub

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindTopic(h *Topic, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

// TODO args
