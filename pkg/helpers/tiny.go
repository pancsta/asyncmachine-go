//go:build tinygo

package helpers

import (
	"context"
	"fmt"
	"slices"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// WaitForAny waits for any of the channels to close, or until the context is
// done, or until the timeout is reached. Returns nil if any channel is
// closed, or ErrTimeout, or ctx.Err().
//
// It's advised to check the state ctx after this call, as it usually means
// expiration and not a timeout.
//
// This function uses reflection to wait for multiple channels at once.
func WaitForAny(
	ctx context.Context, timeout time.Duration, chans ...<-chan struct{},
) error {
	// TODO handler better
	maxChans := 100
	if len(chans) > maxChans {
		return fmt.Errorf("channel overflow, max 100 in tinygo")
	}

	// TODO test
	// TODO reflection-less selectes for 1/2/3 chans
	// exit early
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)

	// generate fake chans
	selChans := slices.Concat(chans, make([]<-chan struct{}, maxChans-len(chans)))

	retCtx := false
	retTimeout := false
	select {
	case <-ctx.Done():
		retCtx = true
		break
	case <-t:
		retTimeout = true
		break

	// TODO lol
	case <-selChans[0]:
		break
	case <-selChans[1]:
		break
	case <-selChans[2]:
		break
	case <-selChans[3]:
		break
	case <-selChans[4]:
		break
	case <-selChans[5]:
		break
	case <-selChans[6]:
		break
	case <-selChans[7]:
		break
	case <-selChans[8]:
		break
	case <-selChans[9]:
		break
	case <-selChans[10]:
		break
	case <-selChans[11]:
		break
	case <-selChans[12]:
		break
	case <-selChans[13]:
		break
	case <-selChans[14]:
		break
	case <-selChans[15]:
		break
	case <-selChans[16]:
		break
	case <-selChans[17]:
		break
	case <-selChans[18]:
		break
	case <-selChans[19]:
		break
	case <-selChans[20]:
		break
	case <-selChans[21]:
		break
	case <-selChans[22]:
		break
	case <-selChans[23]:
		break
	case <-selChans[24]:
		break
	case <-selChans[25]:
		break
	case <-selChans[26]:
		break
	case <-selChans[27]:
		break
	case <-selChans[28]:
		break
	case <-selChans[29]:
		break
	case <-selChans[30]:
		break
	case <-selChans[31]:
		break
	case <-selChans[32]:
		break
	case <-selChans[33]:
		break
	case <-selChans[34]:
		break
	case <-selChans[35]:
		break
	case <-selChans[36]:
		break
	case <-selChans[37]:
		break
	case <-selChans[38]:
		break
	case <-selChans[39]:
		break
	case <-selChans[40]:
		break
	case <-selChans[41]:
		break
	case <-selChans[42]:
		break
	case <-selChans[43]:
		break
	case <-selChans[44]:
		break
	case <-selChans[45]:
		break
	case <-selChans[46]:
		break
	case <-selChans[47]:
		break
	case <-selChans[48]:
		break
	case <-selChans[49]:
		break
	case <-selChans[50]:
		break
	case <-selChans[51]:
		break
	case <-selChans[52]:
		break
	case <-selChans[53]:
		break
	case <-selChans[54]:
		break
	case <-selChans[55]:
		break
	case <-selChans[56]:
		break
	case <-selChans[57]:
		break
	case <-selChans[58]:
		break
	case <-selChans[59]:
		break
	case <-selChans[60]:
		break
	case <-selChans[61]:
		break
	case <-selChans[62]:
		break
	case <-selChans[63]:
		break
	case <-selChans[64]:
		break
	case <-selChans[65]:
		break
	case <-selChans[66]:
		break
	case <-selChans[67]:
		break
	case <-selChans[68]:
		break
	case <-selChans[69]:
		break
	case <-selChans[70]:
		break
	case <-selChans[71]:
		break
	case <-selChans[72]:
		break
	case <-selChans[73]:
		break
	case <-selChans[74]:
		break
	case <-selChans[75]:
		break
	case <-selChans[76]:
		break
	case <-selChans[77]:
		break
	case <-selChans[78]:
		break
	case <-selChans[79]:
		break
	case <-selChans[80]:
		break
	case <-selChans[81]:
		break
	case <-selChans[82]:
		break
	case <-selChans[83]:
		break
	case <-selChans[84]:
		break
	case <-selChans[85]:
		break
	case <-selChans[86]:
		break
	case <-selChans[87]:
		break
	case <-selChans[88]:
		break
	case <-selChans[89]:
		break
	case <-selChans[90]:
		break
	case <-selChans[91]:
		break
	case <-selChans[92]:
		break
	case <-selChans[93]:
		break
	case <-selChans[94]:
		break
	case <-selChans[95]:
		break
	case <-selChans[96]:
		break
	case <-selChans[97]:
		break
	case <-selChans[98]:
		break
	case <-selChans[99]:
		break
	}

	switch {
	case retCtx:
		return ctx.Err()
	case retTimeout:
		return am.ErrTimeout
	default:

		return nil
	}
}

// WaitForErrAny is like WaitForAny, but also waits on WhenErr of the passed
// machine. For state machines with error handling (like retry) it's recommended
// to measure machine time of [am.StateException] instead.
func WaitForErrAny(
	ctx context.Context, timeout time.Duration, mach *am.Machine,
	chans ...<-chan struct{},
) error {
	// TODO handler better
	maxChans := 100
	if len(chans) > maxChans {
		return fmt.Errorf("channel overflow, max 100 in tinygo")
	}

	// TODO test
	// TODO reflection-less selectes for 1/2/3 chans
	// exit early
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)

	// generate fake chans
	selChans := slices.Concat(chans, make([]<-chan struct{}, maxChans-len(chans)))

	retCtx := false
	retTimeout := false
	retMach := false
	select {
	case <-ctx.Done():
		retCtx = true
		break
	case <-t:
		retTimeout = true
		break
	case <-mach.WhenErr(ctx):
		retMach = true
		break

	// TODO lol
	case <-selChans[0]:
		break
	case <-selChans[1]:
		break
	case <-selChans[2]:
		break
	case <-selChans[3]:
		break
	case <-selChans[4]:
		break
	case <-selChans[5]:
		break
	case <-selChans[6]:
		break
	case <-selChans[7]:
		break
	case <-selChans[8]:
		break
	case <-selChans[9]:
		break
	case <-selChans[10]:
		break
	case <-selChans[11]:
		break
	case <-selChans[12]:
		break
	case <-selChans[13]:
		break
	case <-selChans[14]:
		break
	case <-selChans[15]:
		break
	case <-selChans[16]:
		break
	case <-selChans[17]:
		break
	case <-selChans[18]:
		break
	case <-selChans[19]:
		break
	case <-selChans[20]:
		break
	case <-selChans[21]:
		break
	case <-selChans[22]:
		break
	case <-selChans[23]:
		break
	case <-selChans[24]:
		break
	case <-selChans[25]:
		break
	case <-selChans[26]:
		break
	case <-selChans[27]:
		break
	case <-selChans[28]:
		break
	case <-selChans[29]:
		break
	case <-selChans[30]:
		break
	case <-selChans[31]:
		break
	case <-selChans[32]:
		break
	case <-selChans[33]:
		break
	case <-selChans[34]:
		break
	case <-selChans[35]:
		break
	case <-selChans[36]:
		break
	case <-selChans[37]:
		break
	case <-selChans[38]:
		break
	case <-selChans[39]:
		break
	case <-selChans[40]:
		break
	case <-selChans[41]:
		break
	case <-selChans[42]:
		break
	case <-selChans[43]:
		break
	case <-selChans[44]:
		break
	case <-selChans[45]:
		break
	case <-selChans[46]:
		break
	case <-selChans[47]:
		break
	case <-selChans[48]:
		break
	case <-selChans[49]:
		break
	case <-selChans[50]:
		break
	case <-selChans[51]:
		break
	case <-selChans[52]:
		break
	case <-selChans[53]:
		break
	case <-selChans[54]:
		break
	case <-selChans[55]:
		break
	case <-selChans[56]:
		break
	case <-selChans[57]:
		break
	case <-selChans[58]:
		break
	case <-selChans[59]:
		break
	case <-selChans[60]:
		break
	case <-selChans[61]:
		break
	case <-selChans[62]:
		break
	case <-selChans[63]:
		break
	case <-selChans[64]:
		break
	case <-selChans[65]:
		break
	case <-selChans[66]:
		break
	case <-selChans[67]:
		break
	case <-selChans[68]:
		break
	case <-selChans[69]:
		break
	case <-selChans[70]:
		break
	case <-selChans[71]:
		break
	case <-selChans[72]:
		break
	case <-selChans[73]:
		break
	case <-selChans[74]:
		break
	case <-selChans[75]:
		break
	case <-selChans[76]:
		break
	case <-selChans[77]:
		break
	case <-selChans[78]:
		break
	case <-selChans[79]:
		break
	case <-selChans[80]:
		break
	case <-selChans[81]:
		break
	case <-selChans[82]:
		break
	case <-selChans[83]:
		break
	case <-selChans[84]:
		break
	case <-selChans[85]:
		break
	case <-selChans[86]:
		break
	case <-selChans[87]:
		break
	case <-selChans[88]:
		break
	case <-selChans[89]:
		break
	case <-selChans[90]:
		break
	case <-selChans[91]:
		break
	case <-selChans[92]:
		break
	case <-selChans[93]:
		break
	case <-selChans[94]:
		break
	case <-selChans[95]:
		break
	case <-selChans[96]:
		break
	case <-selChans[97]:
		break
	case <-selChans[98]:
		break
	case <-selChans[99]:
		break
	}

	switch {
	case retCtx:
		return ctx.Err()
	case retTimeout:
		return am.ErrTimeout
	case retMach:
		return mach.Err()
	default:

		return nil
	}
}

func ArgsToLogMap(args interface{}, maxLen int) map[string]string {
	// TODO
	return make(map[string]string)
}

func ArgsToArgs[T any](src interface{}, dest T) T {
	// TODO gen mappers
	return dest
}
