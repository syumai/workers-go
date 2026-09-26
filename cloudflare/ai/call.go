//go:build !js

package ai

import (
	"fmt"
	"io"
)

type AI struct {
	instance string
}

func New(varName string) (*AI, error) {
	if varName == "" {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &AI{instance: varName}, nil
}

func (ns *AI) Run(key string, opts map[string]any) (string, error) {
	return "mocked response", nil
}

func (ns *AI) RunWithOutJson(key string, opts map[string]any) (string, error) {
	return "mocked response", nil
}

func (ns *AI) RunReader(key string, opts map[string]any) (io.Reader, error) {
	return io.NopCloser(nil), nil
}
