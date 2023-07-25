package jsonrpc

import (
	"context"
	"fmt"
	"github.com/goccy/go-json"
	"net/http"
)

// Handler links a method of JSON-RPC request.
type Handler interface {
	ServeJSONRPC(c context.Context, params *json.RawMessage) (result any, err *Error)
}

type StreamHandler interface {
	ServeJSONRPCStream(c context.Context, params *json.RawMessage) (ch <-chan []byte, err *Error)
}

// HandlerFunc type is an adapter to allow the use of
// ordinary functions as JSONRPC handlers. If f is a function
// with the appropriate signature, HandlerFunc(f) is a
// jsonrpc.Handler that calls f.
type HandlerFunc func(c context.Context, params *json.RawMessage) (result any, err *Error)

// ServeJSONRPC calls f(w, r).
func (f HandlerFunc) ServeJSONRPC(c context.Context, params *json.RawMessage) (any, *Error) {
	return f(c, params)
}

// ServeHTTP provides basic JSON-RPC handling.
func (mr *MethodRepository) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rs, batch, err := ParseRequest(r)
	if err != nil {
		err := WriteResponse(w, []*Response{
			{
				Version: Version,
				Error:   err,
			},
		}, false)
		if err != nil {
			fmt.Fprint(w, "Failed to encode error objects")
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	resp := make([]*Response, len(rs))
	for i := range rs {
		resp[i] = mr.InvokeMethod(r.Context(), rs[i])
	}

	if err := WriteResponse(w, resp, batch); err != nil {
		fmt.Fprint(w, "Failed to encode result objects")
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// InvokeMethod invokes JSON-RPC method.
func (mr *MethodRepository) InvokeMethod(c context.Context, r *Request) *Response {
	var md Metadata
	res := NewResponse(r)
	md, res.Error = mr.TakeMethodMetadata(r)
	if res.Error != nil {
		return res
	}

	wrappedContext := WithRequestID(c, r.ID)
	wrappedContext = WithMethodName(wrappedContext, r.Method)
	wrappedContext = WithMetadata(wrappedContext, md)
	wrappedContext, res.CancelReq = context.WithCancel(wrappedContext)
	res.Result, res.Error = md.Handler.ServeJSONRPC(wrappedContext, r.Params)
	if res.Error != nil {
		res.Result = nil
	}

	return res
}
