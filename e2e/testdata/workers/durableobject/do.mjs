// Counter is a minimal Durable Object class used to check that the Go
// SDK's cloudflare.DurableObjectNamespace/Stub can drive a real,
// JS-defined Durable Object (obtain a stub, call fetch(), and call an RPC
// method). Extending DurableObject from cloudflare:workers is what enables
// Workers RPC on the stub; a class that only implements fetch() can be
// called with stub.fetch but rejects stub.add().
import { DurableObject } from "cloudflare:workers";

export class Counter extends DurableObject {
  async fetch(_request) {
    let value = (await this.ctx.storage.get("value")) || 0;
    value++;
    await this.ctx.storage.put("value", value);
    return new Response(String(value));
  }

  // add is an RPC method. The Go Worker calls it through
  // cloudflare.DurableObjectStub.RPC, not fetch(). amount is added to the
  // same persisted counter fetch() increments.
  async add(amount) {
    const n = Number(amount);
    if (!Number.isFinite(n)) {
      throw new Error("add: amount must be a finite number");
    }
    let value = (await this.ctx.storage.get("value")) || 0;
    value += n;
    await this.ctx.storage.put("value", value);
    return value;
  }
}
