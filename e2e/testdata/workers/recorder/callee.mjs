// callee.mjs stands in for bindings that wrangler dev cannot simulate
// locally (Vectorize and Workers for Platforms dispatch namespaces have no
// local implementation, and Workers AI always proxies to Cloudflare). Each
// fake method does no real work: it returns a description of the JS values
// it received, as workerd's structured-clone/RPC layer delivered them.
//
// That description is the e2e ground truth for "what JS type does the
// generated Go code actually send?". The fakes cannot say whether the real
// service would accept the value; they only prove what crosses the binding
// boundary, so the Go side's conversion can be checked in a real workerd
// instead of a Go-side fake.

// describe returns a JSON-safe description of v that keeps the distinctions
// the tests care about (Uint8Array vs Array vs plain object vs string).
function describe(v) {
  const tag = Object.prototype.toString.call(v).slice(8, -1);
  switch (tag) {
    case "Null":
    case "Undefined":
    case "Boolean":
    case "Number":
    case "String":
      return { type: tag, value: v === undefined ? null : v };
    case "Uint8Array":
      return { type: tag, value: Array.from(v) };
    case "Date":
      return { type: tag, value: v.toISOString() };
    case "Array":
      return { type: tag, value: v.map(describe) };
    case "Object": {
      const out = {};
      for (const [k, x] of Object.entries(v)) out[k] = describe(x);
      return { type: tag, value: out };
    }
    default:
      return { type: tag };
  }
}

import { WorkerEntrypoint } from "cloudflare:workers";

// Vectorize's generated methods decode a typed reply (VectorizeAsyncMutation
// / VectorizeMatches), so the fake must answer in the real reply shape. What
// it received is kept in module state and read back through lastInsert /
// lastQuery, which the Go fixture calls through Vectorize.JSValue(). Both
// bindings are served by one isolate, so state set by insert is visible to
// the later lastInsert call.
let lastInsert = null;
let lastQuery = null;

export class FakeVectorize extends WorkerEntrypoint {
  async insert(vectors) {
    lastInsert = describe(vectors);
    return { mutationId: "m1" };
  }
  async upsert(vectors) {
    lastInsert = describe(vectors);
    return { mutationId: "m1" };
  }
  async query(vector, options) {
    lastQuery = describe({ vector, options });
    return { matches: [], count: 0 };
  }
  async lastInsert() { return lastInsert; }
  async lastQuery() { return lastQuery; }
}

// DispatchNamespace.get is synchronous in the real API and returns a Fetcher,
// so the Go side gets whatever the stub's get() returns without awaiting it.
// The fake therefore records its arguments in module state and the Go fixture
// reads them back through lastGet(), the same way Vectorize does.
let lastGet = null;

export class FakeDispatch extends WorkerEntrypoint {
  get(name, args, options) {
    lastGet = describe({ name, args, options });
    return {};
  }
  async lastGet() { return lastGet; }
}

export class FakeAi extends WorkerEntrypoint {
  async run(model, inputs, options) { return describe({ model, inputs, options }); }
}

export default { fetch() { return new Response("recorder-callee"); } };
