//go:build js && wasm

package ai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"

	"github.com/syumai/workers/cloudflare/internal/cfruntimecontext"
)

// AI enables access to Workers AI functionality via the env binding.
type AI struct {
	instance js.Value
}

// New creates a new AI binding instance from the Workers environment.
// NewAI returns AI binding for varName
//   - variable name must be defined in wrangler.toml.
//   - see example: https://github.com/hderms/gemma-4-cloudflare-workers-test/blob/main/wrangler.jsonc#L10
//   - if the given variable name doesn't exist on runtime context, returns error.
//   - This function panics when a runtime context is not found.
func NewAI(varName string) (*AI, error) {
	inst := cfruntimecontext.MustGetRuntimeContextEnv().Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}

	ai := &AI{
		instance: inst,
	}

	return ai, nil
}

// Run executes a Workers AI operation using the specified model.
// `input` is serialized into a JS object, and the result is deserialized into `output`.
// output is expected to be something that can be unmarshalled via json.Unmarshal or you will experience issues.
func (a *AI) Run(model string, input any, output any) error {
	inBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("Failure to marshal input as JSON: %s", err.Error())
	}
	modelOutput, err := a.runModel(model, inBytes)

	if err != nil {
		return fmt.Errorf("Failure to run model: %s", err.Error())
	}

	if modelOutput.IsUndefined() || modelOutput.IsNull() {
		return errors.New("response is null")
	}
	resString := js.Global().Get("JSON").Call("stringify", modelOutput).String()
	if err := json.Unmarshal([]byte(resString), output); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

//actually runs the model and returns a js.Value or error representing the end result
func (a *AI) runModel(model string, inBytes []byte) (js.Value, error) {
	jsInput := js.Global().Get("JSON").Call("parse", string(inBytes))

	resultChan := make(chan js.Value, 1)
	errChan := make(chan error, 1)

	go func() {
		//execute and wait
		promise := a.instance.Call("run", model, jsInput)

		jsResult, err := awaitPromise(promise)
		if err != nil {
			errChan <- fmt.Errorf("AI run error: %w", err)
		}

		resultChan <- jsResult
	}()

	select {
	case jsResult := <-resultChan:

		return jsResult, nil
	case err := <-errChan:
		return js.Undefined(), fmt.Errorf("Error awaiting JS promise in AI: %w", err)
	}

}

const DEFAULT_BUFFER_SIZE = 2 ^ 12     //4KB
const MAX_BUFFER_SIZE = 10 * (2 << 20) //10MB

func copyJSBuffer(value js.Value, sizeFactor int) ([]byte, int, error) {
	bufSize := DEFAULT_BUFFER_SIZE * (2 << sizeFactor)
	if bufSize > MAX_BUFFER_SIZE {
		return nil, -1, fmt.Errorf("Failed to copy JS buffer to Go because size %d output exceeded %d", bufSize, MAX_BUFFER_SIZE)
	}

	buf := make([]byte, bufSize)
	n := js.CopyBytesToGo(buf, value)

	//if n == bufSize then there's the possibility that the buffer was not big enough to read the output and the only thing we can do is try again with a bigger buffer
	if n == bufSize {
		return copyJSBuffer(value, sizeFactor+1)
	} else {
		return buf, n, nil
	}
}

// RunBytes executes a Workers AI operation and expects a binary ReadableStream response.
// This is useful for models returning audio or images (e.g., text-to-image).
// WARNING this function is predicated on the assumption that a readable stream chunk will never exceed MAX_BUFFER_SIZE (10MB)
//responseKey is the key in the response object under which the base64 binary is located like {"image" : "base64data"} would be "image"
func (a *AI) RunBytes(model string, input any, responseKey string) ([]byte, error) {

	inBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("Failure to marshal input as JSON: %s", err.Error())
	}

	modelOutput, err := a.runModel(model, inBytes)

	if modelOutput.IsUndefined() || modelOutput.IsNull() {
		return nil, errors.New("response is null")
	}

	if err != nil {
		return nil, fmt.Errorf("Failure to run model: %s", err.Error())
	}
	
	data := modelOutput.Get(responseKey).String()
	decodedBytes, err := base64.StdEncoding.DecodeString(data)

	if err != nil {
		return nil, err
	}

	return decodedBytes, nil
}

// RunStream executes a Workers AI operation and expects a binary ReadableStream response.
// This is useful for models which can be set to streaming=True
// in Cloudflare AI
// the error channel is for errors during the operation of the ReadableStream
// as contrasted with the error return value which concerns
// the preconditions and execution of the cloudflare API request
func (a *AI) RunStream(model string, input any) (chan string, chan error, error) {
	inBytes, err := json.Marshal(input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal input: %w", err)
	}
	
	jsResult, err := a.runModel(model, inBytes)

	if err != nil {
		return nil, nil, fmt.Errorf("Failure to run model: %s", err.Error())
	}

	readableStreamClass := js.Global().Get("ReadableStream")
	if !jsResult.InstanceOf(readableStreamClass) {
		return nil, nil, errors.New("AI model did not return a ReadableStream")
	}
	ch, errch := ReadStream(jsResult)
	//returns a channel which takes input from ReadableStream, closing once it's complete
	//as well as an error channel which signals errors that result from reading the stream
	return ch, errch, nil
}

// ReadStream reads from a ReadableString object in JS
// by sending chunks down the channel which is returned as they are ready
// WARNING this function is predicated on the assumption that a readable stream chunk will never exceed MAX_BUFFER_SIZE (10MB)
func ReadStream(stream js.Value) (chan string, chan error) {

	streamChannel := make(chan string)
	errorChan := make(chan error)

	reader := stream.Call("getReader")

	go func() {

		completionChannel := make(chan bool, 1)
		var cb js.Func
		cb = js.FuncOf(func(this js.Value, args []js.Value) any {

			//first argument to ReadableStream callback is object
			//with done boolean
			result := args[0]
			done := result.Get("done").Bool()

			// If there is no more data to read
			if done {
				completionChannel <- true
				return nil
			}

			value := result.Get("value")

			buf, n, err := copyJSBuffer(value, 1)

			if err != nil {
				errorChan <- err
				completionChannel <- true
				defer cb.Release()
			}

			if n > 0 {
				streamChannel <- string(buf[:n])
			}

			return nil
		})

	ReadLoop:
		for {
			select {
			case <-completionChannel:
				close(completionChannel)
				close(streamChannel)

				break ReadLoop
			default:

				readCB := reader.Call("read").Call("then", cb)
				_, err := awaitPromise(readCB)

				if err != nil {
					completionChannel <- true
					errorChan <- err
					break ReadLoop
				}
			}

		}
	}()

	return streamChannel, errorChan

}

// awaitPromise is a helper function that translates a JavaScript Promise into a Go blocking call.
// This mirrors the behavior of `JsFuture::from()` in your Rust code.
func awaitPromise(promise js.Value) (js.Value, error) {
	errChan := make(chan error, 1)
	resChan := make(chan js.Value, 1)

	var resolve, reject js.Func

	resolve = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer resolve.Release()

		resChan <- args[0]
		return js.Undefined()
	})

	reject = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer reject.Release()
		for _, arg := range args {
			js.Global().Get("console").Call("error", arg)
		}
		errChan <- fmt.Errorf("JS promise rejected: %s", args[0].String())
		return js.Undefined()
	})

	// Attach the Go callbacks to the JS promise
	promise.Call("then", resolve, reject)

	// Block the current goroutine until the JS Promise resolves or rejects
	select {
	case res := <-resChan:
		return res, nil
	case err := <-errChan:
		return js.Undefined(), err
	}
}
