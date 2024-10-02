# FAQ of asyncmachine

## How does asyncmachine work?

It calls certain methods on a struct in a certain order (eg FooState, FooEnd, FooBar).

## Does aRPC auto sync data?

aRPC auto syncs only states (clock values). Mutations carry data in arguments, from Client to Server, while
Provide-Deliver states pass payloads back to the Client.

## Does asyncmachine return data?

No, just yes/no/later (Executed, Canceled, Queued).

## Does asyncmachine return errors?

No, but there's an error state (Exception). There's also sentinel err states, eg ErrNetwork.

## How do I do X/Y/Z in asyncmachine?

Usually the answer is "make it a state".
