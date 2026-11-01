//go:build js && wasm

// Command workflow is an e2e fixture Worker that hosts a Go Workflow
// (exp/cloudflare/workflows) and exposes the client-side binding over HTTP.
// Handlers only return the SDK's raw results as JSON; assertions live in
// the e2e tests, not here.
//
// The class name passed to workflows.Register, workers-assets-gen's
// -workflows flag, and wrangler.jsonc's class_name are all "E2EWorkflow".
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/workflows"
)

// workflowBindingName is the Workflow binding declared in wrangler.jsonc.
const workflowBindingName = "E2E_WORKFLOW"

// workflowClassName matches workers-assets-gen's -workflows flag and
// wrangler.jsonc's class_name.
const workflowClassName = "E2EWorkflow"

// workflowParams is the JSON params object passed to Workflow.Create /
// CreateBatch and read back in the Runner via Event.PayloadJSON.
type workflowParams struct {
	Mode string `json:"mode"`
}

const (
	// modeComplete runs one DoJSON step and returns its result. Used by
	// CreateBatch, where every instance should reach status "complete".
	modeComplete = "complete"
	// modeWait blocks in Step.WaitForEvent until an "approved" event arrives.
	modeWait = "wait"
	// modeTimeout blocks in Step.WaitForEvent with a short timeout and
	// returns that error as the instance output when the event never comes.
	modeTimeout = "timeout"
	// modeSleep blocks in Step.Sleep long enough for pause, resume, and
	// terminate to observe the instance before it finishes on its own.
	modeSleep = "sleep"

	waitEventType    = "approved"
	timeoutEventType = "never"
	// sleepFor is long enough that a lifecycle subtest can pause, resume,
	// and terminate before the step ends. Local workerd honors the
	// duration as a real wait.
	sleepFor = 60 * time.Second
	// waitTimeout is the Step.WaitForEvent timeout for modeTimeout.
	waitTimeout = 2 * time.Second
)

func main() {
	workflows.Register(workflowClassName, runE2EWorkflow)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/create", handleCreate)
	mux.HandleFunc("/batch", handleBatch)
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/event", handleEvent)
	mux.HandleFunc("/pause", handlePause)
	mux.HandleFunc("/resume", handleResume)
	mux.HandleFunc("/terminate", handleTerminate)

	workers.ServeNonBlock(mux)
	workers.Ready()
	<-workers.Done()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// runE2EWorkflow is the Runner for E2EWorkflow. mode selects which step API
// the instance exercises; the returned value becomes InstanceStatus.Output.
func runE2EWorkflow(_ context.Context, event *workflows.Event, step *workflows.Step) (js.Value, error) {
	var params workflowParams
	if err := event.PayloadJSON(&params); err != nil {
		return js.Value{}, err
	}
	switch params.Mode {
	case modeWait:
		ev, err := step.WaitForEvent("await-event", waitEventType, 0)
		if err != nil {
			return js.Value{}, err
		}
		payload, err := jsValueToRaw(ev.Payload)
		if err != nil {
			return js.Value{}, err
		}
		return workflows.ResultJSON(map[string]any{
			"type":    ev.Type,
			"payload": payload,
		})
	case modeTimeout:
		_, err := step.WaitForEvent("await-timeout", timeoutEventType, waitTimeout)
		if err != nil {
			// Surface the timeout as run()'s resolved value so the e2e
			// test can read it from InstanceStatus.Output. Re-raising
			// would let the runtime retry the step instead.
			return workflows.ResultJSON(map[string]any{
				"timedOut": true,
				"error":    err.Error(),
			})
		}
		return workflows.ResultJSON(map[string]any{"timedOut": false})
	case modeSleep:
		if err := step.Sleep("hold", sleepFor); err != nil {
			return js.Value{}, err
		}
		return workflows.ResultJSON(map[string]bool{"slept": true})
	default:
		got, err := workflows.DoJSON(step, "finish", func(context.Context) (map[string]bool, error) {
			return map[string]bool{"ok": true}, nil
		})
		if err != nil {
			return js.Value{}, err
		}
		return workflows.ResultJSON(got)
	}
}

type createBody struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
}

type batchBody struct {
	Instances []createBody `json:"instances"`
}

type eventBody struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body createBody
	if err := readJSON(r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wf, err := workflows.NewWorkflow(workflowBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	opts, err := createOptions(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	instance, err := wf.Create(opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"id": instance.ID()})
}

func handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body batchBody
	if err := readJSON(r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wf, err := workflows.NewWorkflow(workflowBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	batch := make([]workflows.WorkflowInstanceCreateOptions, len(body.Instances))
	for i, inst := range body.Instances {
		opts, err := createOptions(inst)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		batch[i] = opts
	}
	instances, err := wf.CreateBatch(batch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ids := make([]string, len(instances))
	for i, inst := range instances {
		ids[i] = inst.ID()
	}
	writeJSON(w, map[string]any{"ids": ids})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	instance, ok := instanceByQuery(w, r)
	if !ok {
		return
	}
	status, err := instance.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, statusResponse(status))
}

func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instance, ok := instanceByQuery(w, r)
	if !ok {
		return
	}
	var body eventBody
	if err := readJSON(r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, err := rawToJS(body.Payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := instance.SendEvent(workflows.WorkflowInstanceSendEventEvent{
		Type:    body.Type,
		Payload: payload,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instance, ok := instanceByQuery(w, r)
	if !ok {
		return
	}
	if err := instance.Pause(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instance, ok := instanceByQuery(w, r)
	if !ok {
		return
	}
	if err := instance.Resume(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleTerminate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	instance, ok := instanceByQuery(w, r)
	if !ok {
		return
	}
	if err := instance.Terminate(workflows.WorkflowInstanceTerminateOptions{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func instanceByQuery(w http.ResponseWriter, r *http.Request) (*workflows.WorkflowInstance, bool) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing \"id\" query parameter", http.StatusBadRequest)
		return nil, false
	}
	wf, err := workflows.NewWorkflow(workflowBindingName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	instance, err := wf.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return instance, true
}

func createOptions(body createBody) (workflows.WorkflowInstanceCreateOptions, error) {
	mode := body.Mode
	if mode == "" {
		mode = modeComplete
	}
	params, err := jsonToJS(workflowParams{Mode: mode})
	if err != nil {
		return workflows.WorkflowInstanceCreateOptions{}, err
	}
	return workflows.WorkflowInstanceCreateOptions{
		ID:     body.ID,
		Params: params,
	}, nil
}

// statusResponse turns an InstanceStatus into a plain JSON-encodable value.
// InstanceStatus.Output is a raw js.Value, so it is re-encoded through
// JSON.stringify first, matching _examples/workflow-go.
func statusResponse(status workflows.InstanceStatus) map[string]any {
	resp := map[string]any{"status": status.Status}
	if status.Error != nil {
		resp["error"] = map[string]string{
			"name":    status.Error.Name,
			"message": status.Error.Message,
		}
	}
	if !status.Output.IsUndefined() && !status.Output.IsNull() {
		raw, err := jsValueToRaw(status.Output)
		if err != nil {
			resp["outputError"] = err.Error()
		} else {
			resp["output"] = raw
		}
	}
	return resp
}

func readJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("missing body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func jsonToJS(v any) (js.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return js.Value{}, err
	}
	return js.Global().Get("JSON").Call("parse", string(b)), nil
}

func rawToJS(raw json.RawMessage) (js.Value, error) {
	if len(raw) == 0 {
		return js.Value{}, errors.New("missing payload")
	}
	// JSON.parse throws on invalid JSON, and syscall/js turns that into a
	// panic. Validate first so the handler can return 400.
	if !json.Valid(raw) {
		return js.Value{}, errors.New("payload is not valid JSON")
	}
	return js.Global().Get("JSON").Call("parse", string(raw)), nil
}

func jsValueToRaw(v js.Value) (json.RawMessage, error) {
	if v.IsUndefined() || v.IsNull() {
		return json.RawMessage("null"), nil
	}
	s := js.Global().Get("JSON").Call("stringify", v)
	if s.IsUndefined() || s.IsNull() {
		return nil, errors.New("JSON.stringify returned null")
	}
	raw := json.RawMessage(s.String())
	if !json.Valid(raw) {
		return nil, errors.New("JSON.stringify returned invalid JSON")
	}
	return raw, nil
}
