```go
var BasicSchema = am.Schema{
    // Errors

    ssB.Exception: {Multi: true},
    ssB.ErrNetwork: {
        Multi:   true,
        Add:     S{Exception},
        Require: S{Exception},
    },
    ssB.ErrHandlerTimeout: {
        Multi:   true,
        Add:     S{Exception},
        Require: S{Exception},
    },

    // Basics

    ssB.Start:       {},
    ssB.Ready:       {Require: S{ssB.Start}},
    ssB.Healthcheck: {Multi: true},
    ssB.Heartbeat:   {},
}

// ConnectedSchema represents all relations and properties of ConnectedStates.
var ConnectedSchema = am.Schema{
    ssC.ErrConnecting: {Require: S{Exception}},

    ssC.Connecting: {
        Require: S{Start},
        Remove:  sgC.Connected,
    },
    ssC.Connected: {
        Require: S{Start},
        Remove:  sgC.Connected,
    },
    ssC.Disconnecting: {Remove: sgC.Connected},
    ssC.Disconnected: {
        Auto:   true,
        Remove: sgC.Connected,
    },
}

// DisposedSchema represents all relations and properties of DisposedStates.
var DisposedSchema = am.Schema{
    ssD.RegisterDisposal: {Multi: true},
    ssD.Disposing:        {Remove: S{Start}},
    ssD.Disposed:         {Remove: S{ssD.Disposing, Start}},
}
```
