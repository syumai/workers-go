//go:build js && wasm

// Package ai provides the Workers AI binding (https://developers.cloudflare.com/workers-ai/).
//
// Ai.Run, Ai.RunStream, Ai.RunTextGeneration, Ai.RunTextGenerationStream,
// and Ai.RunTextEmbeddings are hand-written here: Ai.run itself is a
// TypeScript overload set keyed off a generic AiModelList[Name] type
// parameter (see exp/internal/gen/overrides/ai.yaml), which cfgen cannot
// generate Go from, so this file calls straight into the underlying JS
// method instead.
package ai

import (
	"fmt"
	"io"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// Run calls the Ai binding's underlying run(model, inputs, options) method
// directly: model names a Workers AI model ID, inputs is the raw JS object
// (or array, for a batch request) the chosen model expects, and the
// returned js.Value is that model's raw JS response, all left un-typed
// since AiModels' mapped-type model -> input/output pairing isn't
// representable in the generator's IR (see ai.yaml). Prefer
// RunTextGeneration or RunTextEmbeddings when the model family matches one
// of those two shapes.
func (a *Ai) Run(model string, inputs js.Value, opts *AiOptions) (js.Value, error) {
	options := js.Value{}
	if opts != nil {
		var err error
		if options, err = opts.toJS(); err != nil {
			return js.Value{}, fmt.Errorf("ai: encoding options: %w", err)
		}
	}
	p, err := jsrt.Call(a.v, "run", model, inputs, options)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Await(p)
}

// RunStream is Run, except it sets inputs.stream = true (mutating inputs
// in place) before calling run, and wraps the resulting JS ReadableStream
// (a server-sent-events byte stream) in an io.ReadCloser instead of
// decoding it.
func (a *Ai) RunStream(model string, inputs js.Value, opts *AiOptions) (io.ReadCloser, error) {
	inputs.Set("stream", true)
	v, err := a.Run(model, inputs, opts)
	if err != nil {
		return nil, err
	}
	return jsrt.ReadCloser(v), nil
}

// RunTextGeneration runs a text-generation model (the @cf/meta/llama-*
// family and similar), encoding in via AiTextGenerationInput.toJS and
// decoding the response via aiTextGenerationOutputFromJS.
func (a *Ai) RunTextGeneration(model string, in AiTextGenerationInput, opts *AiOptions) (AiTextGenerationOutput, error) {
	jsIn, err := in.toJS()
	if err != nil {
		return AiTextGenerationOutput{}, fmt.Errorf("ai: encoding AiTextGenerationInput: %w", err)
	}
	v, err := a.Run(model, jsIn, opts)
	if err != nil {
		return AiTextGenerationOutput{}, err
	}
	out, err := aiTextGenerationOutputFromJS(v)
	if err != nil {
		return AiTextGenerationOutput{}, fmt.Errorf("ai: decoding AiTextGenerationOutput: %w", err)
	}
	return out, nil
}

// RunTextGenerationStream is RunTextGeneration, except it sets
// in.Stream = true and returns the raw SSE byte stream instead of a
// decoded AiTextGenerationOutput; see RunStream.
func (a *Ai) RunTextGenerationStream(model string, in AiTextGenerationInput, opts *AiOptions) (io.ReadCloser, error) {
	in.Stream = true
	jsIn, err := in.toJS()
	if err != nil {
		return nil, fmt.Errorf("ai: encoding AiTextGenerationInput: %w", err)
	}
	return a.RunStream(model, jsIn, opts)
}

// RunTextEmbeddings runs a text-embeddings model (the @cf/baai/bge-* family
// and similar), encoding in via AiTextEmbeddingsInput.toJS and decoding the
// response via aiTextEmbeddingsOutputFromJS.
func (a *Ai) RunTextEmbeddings(model string, in AiTextEmbeddingsInput, opts *AiOptions) (AiTextEmbeddingsOutput, error) {
	jsIn, err := in.toJS()
	if err != nil {
		return AiTextEmbeddingsOutput{}, fmt.Errorf("ai: encoding AiTextEmbeddingsInput: %w", err)
	}
	v, err := a.Run(model, jsIn, opts)
	if err != nil {
		return AiTextEmbeddingsOutput{}, err
	}
	out, err := aiTextEmbeddingsOutputFromJS(v)
	if err != nil {
		return AiTextEmbeddingsOutput{}, fmt.Errorf("ai: decoding AiTextEmbeddingsOutput: %w", err)
	}
	return out, nil
}
