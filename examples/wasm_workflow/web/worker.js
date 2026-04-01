// Import the Go JS Glue (ensure wasm_exec.js is served at this path)
importScripts("wasm_exec.js");

const go = new Go();
go.argv = ["main.wasm", "worker"];
WebAssembly.instantiateStreaming(fetch("main.wasm?"+Date.now()), go.importObject).then((result) => {
    go.run(result.instance);
});