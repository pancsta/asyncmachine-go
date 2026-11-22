# WASM livepage Example

This example ships a **TinyGo** client with full-duplex aRPC over [livepage], using a single WebSocket only. It's
useful for small and fast Go deployments to the browser (~2MB) when compared to GopherJS (~10MB) or BigGo (~40MB). In
theory, it should also work for WebWorkers, like in `/examples/wasm_workflow`.
