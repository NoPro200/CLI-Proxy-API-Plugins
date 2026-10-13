package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int ModelMergerPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void ModelMergerPluginFree(void*, size_t);
extern void ModelMergerPluginShutdown(void);

static int ModelMerger_call_host(cliproxy_host_api* api, const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	return api->call(api->host_ctx, method, request, request_len, response);
}

static void ModelMerger_free_host_buffer(cliproxy_host_api* api, void* ptr, size_t len) {
	api->free_buffer(ptr, len);
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
	mergerplugin "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/plugin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

var abiState = struct {
	sync.RWMutex
	host   *C.cliproxy_host_api
	plugin *mergerplugin.ModelMergerPlugin
}{}

type abiLifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type abiRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  abiCapabilities    `json:"capabilities"`
}

type abiCapabilities struct {
	AuthProvider          bool                         `json:"auth_provider"`
	ModelProvider         bool                         `json:"model_provider"`
	Executor              bool                         `json:"executor"`
	ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
	ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
	ManagementAPI         bool                         `json:"management_api"`
}

type abiIdentifierResponse struct {
	Identifier string `json:"identifier"`
}

type abiManagementRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiHostAuthListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type abiHostAuthGetRequest struct {
	AuthIndex      string `json:"auth_index"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiHostAuthGetResponse struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"`
	JSON      json.RawMessage `json:"json"`
}

type abiHostAuthSaveRequest struct {
	pluginapi.HostAuthSaveRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type abiEmptyResponse struct{}

type abiHostLogRequest struct {
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil {
		return 1
	}
	abiState.Lock()
	abiState.host = host
	abiState.Unlock()
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.ModelMergerPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.ModelMergerPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.ModelMergerPluginShutdown)
	return 0
}

//export ModelMergerPluginCall
func ModelMergerPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeABIResponse(response, abiErrorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleABIMethod(context.Background(), C.GoString(method), requestBytes)
	if errHandle != nil {
		writeABIResponse(response, abiErrorEnvelopeFromError("plugin_error", errHandle))
		return 1
	}
	writeABIResponse(response, raw)
	return 0
}

//export ModelMergerPluginFree
func ModelMergerPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export ModelMergerPluginShutdown
func ModelMergerPluginShutdown() {
	abiState.Lock()
	abiState.plugin = nil
	abiState.host = nil
	abiState.Unlock()
}

func handleABIMethod(ctx context.Context, method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return handleRegister(request)
	}
	p, errPlugin := currentPlugin()
	if errPlugin != nil {
		return nil, errPlugin
	}
	switch method {
	case pluginabi.MethodAuthIdentifier:
		return abiOKEnvelope(abiIdentifierResponse{Identifier: authpkg.ProviderKey})
	case pluginabi.MethodExecutorIdentifier:
		return abiOKEnvelope(abiIdentifierResponse{Identifier: p.Identifier()})
	case pluginabi.MethodAuthParse:
		var req pluginapi.AuthParseRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.ParseAuth(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodAuthLoginStart:
		var req pluginapi.AuthLoginStartRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.StartLogin(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodAuthLoginPoll:
		var req pluginapi.AuthLoginPollRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.PollLogin(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodAuthRefresh:
		var req pluginapi.AuthRefreshRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.RefreshAuth(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodModelStatic:
		var req pluginapi.StaticModelRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.StaticModels(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodModelForAuth:
		var req pluginapi.AuthModelRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.ModelsForAuth(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodExecutorExecute:
		var req pluginapi.ExecutorRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.Execute(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodExecutorExecuteStream:
		var req pluginapi.ExecutorRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		// Virtual image models do not stream; the executor says so.
		_, errCall := p.ExecuteStream(ctx, req)
		return abiOKEnvelopeWithError(abiEmptyResponse{}, errCall)
	case pluginabi.MethodExecutorCountTokens:
		var req pluginapi.ExecutorRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.CountTokens(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodExecutorHTTPRequest:
		var req pluginapi.ExecutorHTTPRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.HttpRequest(ctx, req)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodManagementRegister:
		var req pluginapi.ManagementRegistrationRequest
		if errDecode := json.Unmarshal(request, &req); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.RegisterManagement(ctx, req)
		resp = stripManagementHandlers(resp)
		return abiOKEnvelopeWithError(resp, errCall)
	case pluginabi.MethodManagementHandle:
		var rpcReq abiManagementRequest
		if errDecode := json.Unmarshal(request, &rpcReq); errDecode != nil {
			return nil, errDecode
		}
		resp, errCall := p.HandleManagementWithHost(ctx, rpcReq.ManagementRequest, abiManagementHost{callbackID: rpcReq.HostCallbackID})
		return abiOKEnvelopeWithError(resp, errCall)
	default:
		return abiErrorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func handleRegister(request []byte) ([]byte, error) {
	var req abiLifecycleRequest
	if errDecode := json.Unmarshal(request, &req); errDecode != nil {
		return nil, errDecode
	}
	plugin := mergerplugin.Build(req.ConfigYAML, hostLog)
	plugin.Metadata.Version = pluginVersion
	p, ok := plugin.Capabilities.AuthProvider.(*mergerplugin.ModelMergerPlugin)
	if !ok || p == nil {
		return nil, fmt.Errorf("model-merger plugin registration returned invalid auth provider")
	}
	abiState.Lock()
	abiState.plugin = p
	abiState.Unlock()
	return abiOKEnvelope(abiRegistration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata:      plugin.Metadata,
		Capabilities: abiCapabilities{
			AuthProvider:          plugin.Capabilities.AuthProvider != nil,
			ModelProvider:         plugin.Capabilities.ModelProvider != nil,
			Executor:              plugin.Capabilities.Executor != nil,
			ExecutorModelScope:    plugin.Capabilities.ExecutorModelScope,
			ExecutorInputFormats:  append([]string(nil), plugin.Capabilities.ExecutorInputFormats...),
			ExecutorOutputFormats: append([]string(nil), plugin.Capabilities.ExecutorOutputFormats...),
			ManagementAPI:         plugin.Capabilities.ManagementAPI != nil,
		},
	})
}

// hostLog writes to CLIProxyAPI's log; a failed log call is not worth an error.
func hostLog(level, message string, fields map[string]any) {
	_, _ = callHost[abiEmptyResponse](pluginabi.MethodHostLog, abiHostLogRequest{Level: level, Message: message, Fields: fields})
}

func currentPlugin() (*mergerplugin.ModelMergerPlugin, error) {
	abiState.RLock()
	defer abiState.RUnlock()
	if abiState.plugin == nil {
		return nil, fmt.Errorf("model-merger plugin is not registered")
	}
	return abiState.plugin, nil
}

func stripManagementHandlers(resp pluginapi.ManagementRegistrationResponse) pluginapi.ManagementRegistrationResponse {
	for i := range resp.Routes {
		resp.Routes[i].Handler = nil
	}
	for i := range resp.Resources {
		resp.Resources[i].Handler = nil
	}
	return resp
}

type abiManagementHost struct {
	callbackID string
}

func (h abiManagementHost) ListAuths(ctx context.Context) ([]pluginapi.HostAuthFileEntry, error) {
	_ = ctx
	resp, err := callHost[abiHostAuthListResponse](pluginabi.MethodHostAuthList, map[string]any{
		"host_callback_id": h.callbackID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Files, nil
}

func (h abiManagementHost) GetAuth(ctx context.Context, authIndex string) (pluginapi.HostAuthGetResponse, error) {
	_ = ctx
	resp, err := callHost[abiHostAuthGetResponse](pluginabi.MethodHostAuthGet, abiHostAuthGetRequest{
		AuthIndex:      authIndex,
		HostCallbackID: h.callbackID,
	})
	if err != nil {
		return pluginapi.HostAuthGetResponse{}, err
	}
	return pluginapi.HostAuthGetResponse{
		AuthIndex: resp.AuthIndex,
		Name:      resp.Name,
		Path:      resp.Path,
		JSON:      resp.JSON,
	}, nil
}

func (h abiManagementHost) SaveAuth(ctx context.Context, name string, raw json.RawMessage) (pluginapi.HostAuthSaveResponse, error) {
	_ = ctx
	return callHost[pluginapi.HostAuthSaveResponse](pluginabi.MethodHostAuthSave, abiHostAuthSaveRequest{
		HostAuthSaveRequest: pluginapi.HostAuthSaveRequest{
			Name: name,
			JSON: raw,
		},
		HostCallbackID: h.callbackID,
	})
}

func callHost[T any](method string, request any) (T, error) {
	var zero T
	abiState.RLock()
	host := abiState.host
	abiState.RUnlock()
	if host == nil || host.call == nil {
		return zero, fmt.Errorf("host callback is unavailable")
	}
	rawRequest, errMarshal := json.Marshal(request)
	if errMarshal != nil {
		return zero, errMarshal
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var requestPtr *C.uint8_t
	if len(rawRequest) > 0 {
		requestPtr = (*C.uint8_t)(unsafe.Pointer(&rawRequest[0]))
	}
	var resp C.cliproxy_buffer
	code := C.ModelMerger_call_host(host, cMethod, requestPtr, C.size_t(len(rawRequest)), &resp)
	if resp.ptr != nil {
		defer C.ModelMerger_free_host_buffer(host, resp.ptr, resp.len)
	}
	if code != 0 {
		return zero, fmt.Errorf("host callback %s failed with code %d", method, int(code))
	}
	rawResp := C.GoBytes(resp.ptr, C.int(resp.len))
	var envelope pluginabi.Envelope
	if errDecode := json.Unmarshal(rawResp, &envelope); errDecode != nil {
		return zero, errDecode
	}
	if !envelope.OK {
		if envelope.Error != nil {
			return zero, fmt.Errorf("%s", envelope.Error.Message)
		}
		return zero, fmt.Errorf("host callback %s failed", method)
	}
	var out T
	if len(envelope.Result) == 0 {
		return out, nil
	}
	if errDecode := json.Unmarshal(envelope.Result, &out); errDecode != nil {
		return zero, errDecode
	}
	return out, nil
}

func abiOKEnvelope(v any) ([]byte, error) {
	result, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(pluginabi.Envelope{OK: true, Result: result})
}

func abiOKEnvelopeWithError(v any, err error) ([]byte, error) {
	if err != nil {
		return abiErrorEnvelopeFromError("plugin_error", err), nil
	}
	return abiOKEnvelope(v)
}

func abiErrorEnvelopeFromError(code string, err error) []byte {
	if err == nil {
		return abiErrorEnvelope(code, "")
	}
	httpStatus := 0
	var statusProvider interface{ StatusCode() int }
	if errors.As(err, &statusProvider) {
		httpStatus = statusProvider.StatusCode()
	}
	return abiErrorEnvelopeWithStatus(code, err.Error(), httpStatus)
}

func abiErrorEnvelope(code string, message string) []byte {
	return abiErrorEnvelopeWithStatus(code, message, 0)
}

func abiErrorEnvelopeWithStatus(code string, message string, httpStatus int) []byte {
	raw, _ := json.Marshal(pluginabi.Envelope{
		OK: false,
		Error: &pluginabi.Error{
			Code:       code,
			Message:    message,
			HTTPStatus: httpStatus,
		},
	})
	return raw
}

func writeABIResponse(response *C.cliproxy_buffer, data []byte) {
	if response == nil {
		return
	}
	if len(data) == 0 {
		response.ptr = nil
		response.len = 0
		return
	}
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		response.ptr = nil
		response.len = 0
		return
	}
	C.memcpy(ptr, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	response.ptr = ptr
	response.len = C.size_t(len(data))
}
