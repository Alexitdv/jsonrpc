package jsonrpc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/goccy/go-json"
)

const (
	// Version is JSON-RPC 2.0.
	Version = "2.0"

	batchRequestKey  = '['
	contentTypeKey   = "Content-Type"
	contentTypeValue = "application/json"
)

type (
	// A Request represents a JSON-RPC request received by the server.
	Request struct {
		Version string           `json:"jsonrpc"`
		Method  string           `json:"method"`
		Params  *json.RawMessage `json:"params"`
		ID      *json.RawMessage `json:"id"`
	}

	// A Response represents a JSON-RPC response returned by the server.
	Response struct {
		Version   string             `json:"jsonrpc"`
		ID        *json.RawMessage   `json:"id,omitempty"`
		Result    any                `json:"result,omitempty"`
		Error     *Error             `json:"error,omitempty"`
		CancelReq context.CancelFunc `json:"-"`
	}
)

// ParseRequest parses a HTTP request to JSON-RPC request.
func ParseRequest(r *http.Request) ([]*Request, bool, *Error) {
	var rerr *Error

	if !strings.HasPrefix(r.Header.Get(contentTypeKey), contentTypeValue) {
		return nil, false, ErrInvalidRequest()
	}

	buf := bytes.NewBuffer(make([]byte, 0, r.ContentLength))
	if _, err := buf.ReadFrom(r.Body); err != nil {
		return nil, false, ErrInvalidRequest()
	}
	defer func(r *http.Request) {
		err := r.Body.Close()
		if err != nil {
			rerr = ErrInternal()
		}
	}(r)

	if buf.Len() == 0 {
		return nil, false, ErrInvalidRequest()
	}

	f, _, err := buf.ReadRune()
	if err != nil {
		return nil, false, ErrInvalidRequest()
	}
	if err := buf.UnreadRune(); err != nil {
		return nil, false, ErrInvalidRequest()
	}

	var rs []*Request
	if f != batchRequestKey {
		var req *Request
		if err := json.NewDecoder(buf).Decode(&req); err != nil {
			return nil, false, ErrParse()
		}

		return append(rs, req), false, nil
	}

	if err := json.NewDecoder(buf).Decode(&rs); err != nil {
		return nil, false, ErrParse()
	}

	return rs, true, rerr
}

// NewResponse generates a JSON-RPC response.
func NewResponse(r *Request) *Response {
	return &Response{
		Version: r.Version,
		ID:      r.ID,
	}
}

func WriteNoStream(w http.ResponseWriter, resp []*Response, batch bool) error {
	if batch || len(resp) > 1 {
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			return fmt.Errorf("jsonrpc: failed to encode: %w", err)
		}
	} else if len(resp) == 1 {
		if err := json.NewEncoder(w).Encode(resp[0]); err != nil {
			return fmt.Errorf("jsonrpc: failed to encode: %w", err)
		}
	}

	return nil
}

func CheckStream(resp []*Response) bool {
	for _, r := range resp {
		_, ok := r.Result.(chan []byte)
		if ok {
			return true
		}
	}
	return false
}

func WriteWithStream(w http.ResponseWriter, resp []*Response, batch bool) error {
	if batch || len(resp) > 1 {
		w.Write([]byte("["))
	}
	for i, r := range resp {
		if i > 0 {
			w.Write([]byte(","))
		}
		ch, ok := r.Result.(chan []byte)
		if ok {
			w.Write([]byte("{"))
			w.Write([]byte(fmt.Sprintf(`"jsonrpc": "%s",`, r.Version)))
			w.Write([]byte(fmt.Sprintf(`"id": %s,`, *r.ID)))
			w.Write([]byte(`"result": `))
			for data := range ch {
				_, err := w.Write(data)
				if err != nil {
					r.CancelReq()
				}
			}
			w.Write([]byte("}"))
		} else {
			data, _ := json.Marshal(r)
			w.Write(data)
		}
	}
	if batch || len(resp) > 1 {
		w.Write([]byte("]"))
	}
	return nil
}

// WriteResponse writes JSON-RPC response.
func WriteResponse(w http.ResponseWriter, resp []*Response, batch bool) error {
	w.Header().Set(contentTypeKey, contentTypeValue)
	if CheckStream(resp) {
		return WriteWithStream(w, resp, batch)
	}
	return WriteNoStream(w, resp, batch)
}
