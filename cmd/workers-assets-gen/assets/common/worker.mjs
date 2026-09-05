import "./wasm_exec.js";
import { createRuntimeContext, loadModule } from "./runtime.mjs";

let mod;

globalThis.tryCatch = (fn) => {
  try {
    return {
      result: fn(),
    };
  } catch (e) {
    return {
      error: e,
    };
  }
};

async function run(ctx) {
  if (mod === undefined) {
    mod = await loadModule();
  }
  const go = new Go();

  let ready;
  const readyPromise = new Promise((resolve) => {
    ready = resolve;
  });
  const instance = new WebAssembly.Instance(mod, {
    ...go.importObject,
    workers: {
      ready: () => {
        ready();
      },
    },
  });
  go.run(instance, ctx);
  await readyPromise;
}

async function fetch(req, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(req);
}

async function scheduled(event, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.runScheduler(event);
}


async function queue(batch, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleQueueMessageBatch(batch);
}

// onRequest handles request to Cloudflare Pages
async function onRequest(ctx) {
  const binding = {};
  const { request, env } = ctx;
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(request);
}

async function email(message, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleEmail(message);
}

export default {
  fetch,
  scheduled,
  queue,
  onRequest,
  email,
};
