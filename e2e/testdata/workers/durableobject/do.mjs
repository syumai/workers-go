// Counter is a minimal Durable Object class used to check that the Go
// SDK's cloudflare.DurableObjectNamespace/Stub can drive a real,
// JS-defined Durable Object (obtain a stub, call fetch(), read the
// response). See _examples/durable-object-counter/worker.mjs, which this
// is based on.
export class Counter {
  constructor(state) {
    this.state = state;
  }

  async fetch(_request) {
    let value = (await this.state.storage.get("value")) || 0;
    value++;
    await this.state.storage.put("value", value);
    return new Response(String(value));
  }
}
