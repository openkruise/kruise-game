/*
Copyright 2023 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package errors

import "fmt"

// PluginErrorType describes a high-level category of a given error
type PluginErrorType string

const (
	// ApiCallError is an error related to communication with k8s API server
	ApiCallError PluginErrorType = "apiCallError"
	// InternalError is an error inside plugin
	InternalError PluginErrorType = "internalError"
	// ParameterError is an error related to bad parameters provided by a user
	ParameterError PluginErrorType = "parameterError"
	// NotImplementedError an error related to be not implemented by developers
	NotImplementedError PluginErrorType = "notImplementedError"
)

type PluginError interface {
	// Error implements golang error interface
	Error() string

	// Type returns the type of CloudProviderError
	Type() PluginErrorType
}

type pluginErrorImplErrorImpl struct {
	errorType PluginErrorType
	msg       string
}

func (c pluginErrorImplErrorImpl) Error() string {
	return c.msg
}

func (c pluginErrorImplErrorImpl) Type() PluginErrorType {
	return c.errorType
}

// NewPluginError returns new plugin error with a message constructed from format string
func NewPluginError(errorType PluginErrorType, msg string, args ...interface{}) PluginError {
	return pluginErrorImplErrorImpl{
		errorType: errorType,
		msg:       fmt.Sprintf(msg, args...),
	}
}

func ToPluginError(err error, errorType PluginErrorType) PluginError {
	if err == nil {
		return nil
	}
	return pluginErrorImplErrorImpl{
		errorType: errorType,
		msg:       err.Error(),
	}
}
