package repl

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// clients returns a snapshot of the registered clients.
func (r *Repl) clients() []*replClient {
	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()

	return slices.Clone(r.rpcClients)
}

// addrs returns a snapshot of the registered named addresses.
func (r *Repl) addrs() []NamedAddr {
	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()

	return slices.Clone(r.Addrs)
}

// clientName returns the name of a client by its local machine ID.
func (r *Repl) clientName(machId string) string {
	for _, c := range r.clients() {
		if c.Mach.Id() == machId {
			return c.Name
		}
	}

	return machId
}

// countReady returns the number of ready clients.
func countReady(clients []*replClient) int {
	ready := 0
	for _, c := range clients {
		if c.Mach.Is1(ssrpc.ClientStates.Ready) {
			ready++
		}
	}

	return ready
}

// connStatus returns the aggregate connection status of a pool.
func connStatus(ready, total int) (connected, fully bool) {
	return ready > 0, total > 0 && ready == total
}

// isShuttingDown reports whether the current transition tears the REPL down.
func isShuttingDown(e *am.Event) bool {
	if e == nil {
		return false
	}

	t := e.Transition().TargetStates()
	mut := e.Mutation()
	iStart := e.Machine().Index1(ss.Start)

	return slices.Contains(t, ss.Disconnected) ||
		slices.Contains(t, ss.Disconnecting) ||
		// dont block a restart
		(mut.Type == am.MutationRemove && mut.IsCalled(iStart))
}

// updateConnStates recomputes the aggregate ConnPool states from the clients.
func (r *Repl) updateConnStates() {
	clients := r.clients()
	connected, fully := connStatus(countReady(clients), len(clients))

	if connected {
		r.Mach.Add1(ss.Connected, nil)
	} else {
		r.Mach.Remove1(ss.Connected, nil)
	}

	if fully {
		r.Mach.Add1(ss.ConnectedFully, nil)
	} else {
		r.Mach.Remove1(ss.ConnectedFully, nil)
	}
}

// planReconcile matches existing clients to new addresses, keeping clients
// which keep their name+addr, or only their addr (eg after a file rename).
// The returned slice is indexed like addrs, with nil for new addresses.
func planReconcile(
	old []*replClient, addrs []NamedAddr,
) (keep []*replClient, removed []*replClient) {
	byName := make(map[string]*replClient, len(old))
	byAddr := make(map[string]*replClient, len(old))
	for _, c := range old {
		byName[c.Name] = c
		byAddr[c.Addr] = c
	}

	used := make(map[*replClient]bool, len(old))
	keep = make([]*replClient, len(addrs))
	for i, a := range addrs {

		// keep by name
		if c := byName[a.Name]; c != nil && !used[c] && c.Addr == a.Addr {
			keep[i] = c
			used[c] = true
			continue
		}

		// keep by addr, eg a renamed address file
		if c := byAddr[a.Addr]; c != nil && !used[c] {
			keep[i] = c
			used[c] = true
			continue
		}
	}

	for _, c := range old {
		if !used[c] {
			removed = append(removed, c)
		}
	}

	return keep, removed
}

// renameClient returns a client with a new name, keeping the same connection.
// It never mutates the passed client, so concurrent readers stay race-free.
func renameClient(c *replClient, name string) *replClient {
	if c.Name == name {
		return c
	}

	return &replClient{Name: name, Client: c.Client}
}

// manualAddrs returns manually added addresses.
func manualAddrs(addrs []NamedAddr) []NamedAddr {
	ret := make([]NamedAddr, 0, len(addrs))
	for _, a := range addrs {
		if a.Manual {
			ret = append(ret, a)
		}
	}

	return ret
}

// mergeManualAddrs appends manual addresses missing from the watched list.
func mergeManualAddrs(watched, manual []NamedAddr) []NamedAddr {
	ret := slices.Clone(watched)
	seen := make(map[string]bool, len(ret)+len(manual))
	for _, a := range ret {
		seen[a.Addr] = true
	}
	for _, a := range manual {
		if seen[a.Addr] {
			continue
		}
		seen[a.Addr] = true
		ret = append(ret, a)
	}

	return ret
}

// validateConnectAddr validates a host:port address with an optional WebSocket
// path suffix, eg localhost:6452/ws.
func validateConnectAddr(addr string) error {
	hostport := addr
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		hostport = addr[:i]
	}

	host, port, err := net.SplitHostPort(hostport)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("invalid address: %s", addr)
	}

	return nil
}

// nextCLIName returns the next free CLI<N> name.
func nextCLIName(addrs []NamedAddr) string {
	used := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		used[a.Name] = true
	}
	for i := 0; ; i++ {
		name := "CLI" + strconv.Itoa(i)
		if !used[name] {
			return name
		}
	}
}

// connectAddr adds and starts a new client, or returns an existing one for the
// same address. Added clients are marked manual and survive watcher updates.
func (r *Repl) connectAddr(e *am.Event, addr string) (
	*replClient, bool, error,
) {
	r.clientsMu.Lock()
	for _, c := range r.rpcClients {
		if c.Addr == addr {
			r.clientsMu.Unlock()
			return c, false, nil
		}
	}

	named := NamedAddr{
		Name:   nextCLIName(r.Addrs),
		Addr:   addr,
		Manual: true,
	}
	r.clientSeq++
	c, err := r.newRpcClient(named, strconv.Itoa(r.clientSeq))
	if err != nil {
		r.clientsMu.Unlock()
		return nil, false, err
	}
	r.rpcClients = append(r.rpcClients, c)
	r.Addrs = append(r.Addrs, named)
	r.clientsMu.Unlock()

	c.Start(e)
	r.updateConnStates()

	return c, true, nil
}

// reconcileClients applies a new address list, keeping existing clients for
// unchanged addresses and stopping only removed ones.
func (r *Repl) reconcileClients(e *am.Event, addrs []NamedAddr) {
	r.clientsMu.Lock()
	// watched updates must not drop manually connected machines
	addrs = mergeManualAddrs(addrs, manualAddrs(r.Addrs))
	keep, removed := planReconcile(r.rpcClients, addrs)

	next := make([]*replClient, 0, len(addrs))
	added := make([]*replClient, 0)
	for i, a := range addrs {
		if keep[i] != nil {
			next = append(next, renameClient(keep[i], a.Name))
			continue
		}

		r.clientSeq++
		c, err := r.newRpcClient(a, strconv.Itoa(r.clientSeq))
		if err != nil {
			r.Mach.EvAddErr(e, err, nil)
			continue
		}
		next = append(next, c)
		added = append(added, c)
	}

	r.rpcClients = next
	r.Addrs = addrs
	r.clientsMu.Unlock()

	// start only the new clients
	for _, c := range added {
		c.Start(e)
	}
	// stop removed clients outside the lock
	for _, c := range removed {
		r.stopClient(c)
	}

	r.updateConnStates()
}

// stopClient gracefully disconnects and disposes a removed client.
func (r *Repl) stopClient(c *replClient) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if c.NetMach == nil {
			c.Mach.Dispose()
			return
		}
		c.Stop(ctx, nil, true)
	}()
}

// clientStatus is a named client connection status.
type clientStatus struct {
	Name  string
	Addr  string
	State string
}

// clientStatuses returns the status of all registered clients.
func (r *Repl) clientStatuses() []clientStatus {
	clients := r.clients()
	ret := make([]clientStatus, 0, len(clients))
	for _, c := range clients {
		state := "disconnected"
		switch {
		case c.Mach.Is1(ssrpc.ClientStates.Ready):
			state = "connected"
		case c.Mach.Is1(ssrpc.ClientStates.RetryingConn):
			state = "retrying"
		case c.Mach.Is1(ssrpc.ClientStates.Connecting):
			state = "connecting"
		}
		ret = append(ret, clientStatus{
			Name:  c.Name,
			Addr:  c.Addr,
			State: state,
		})
	}

	return ret
}
