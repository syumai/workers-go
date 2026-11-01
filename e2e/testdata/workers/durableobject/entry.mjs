// entry.mjs is wrangler.jsonc's `main`. build/worker.mjs is generated
// (workers-assets-gen + go build), so it can't itself export the
// JS-defined Counter class; this file re-exports the generated worker's
// handlers (fetch/scheduled/queue/onRequest) as the default export while
// also exporting Counter and the generated Go Durable Object class
// Notebook, matching the pattern in
// _examples/durable-object-counter/worker.mjs.
export { default, Notebook } from "./build/worker.mjs";
export { Counter } from "./do.mjs";
