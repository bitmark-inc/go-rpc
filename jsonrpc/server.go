// Copyright 2010 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"encoding/json"
	"errors"
	"io"
	rpc "github.com/bitmark-inc/go-rpc" // "net/rpc"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

var errMissingParams = errors.New("jsonrpc: request body missing params")
var errInvalidParams = errors.New("jsonrpc: params must be struct or slice")

type serverCodec struct {
	dec *json.Decoder // for reading JSON values
	enc *json.Encoder // for writing JSON values
	c   io.Closer

	// temporary work space
	req serverRequest

	// JSON-RPC clients can use arbitrary json values as request IDs.
	// Package rpc expects uint64 request IDs.
	// We assign uint64 sequence numbers to incoming requests
	// but save the original request ID in the pending map.
	// When rpc responds, we use the sequence number in
	// the response to find the original request ID.
	mutex   sync.Mutex // protects seq, pending
	seq     uint64
	pending map[uint64]*json.RawMessage
}

// NewServerCodec returns a new rpc.ServerCodec using JSON-RPC on conn.
func NewServerCodec(conn io.ReadWriteCloser) rpc.ServerCodec {
	return &serverCodec{
		dec:     json.NewDecoder(conn),
		enc:     json.NewEncoder(conn),
		c:       conn,
		pending: make(map[uint64]*json.RawMessage),
	}
}

type serverRequest struct {
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params"`
	Id     *json.RawMessage `json:"id"`
}

type serverNotify struct {
	Method string           `json:"method"`
	Params interface{}      `json:"params"`
	Id     *json.RawMessage `json:"id"`
}

func (r *serverRequest) reset() {
	r.Method = ""
	r.Params = nil
	r.Id = nil
}

type serverResponse struct {
	Id     *json.RawMessage `json:"id"`
	Result interface{}      `json:"result"`
	Error  interface{}      `json:"error"`
}

// capitalise after "." and "_" and remove "_"
// map "type.method"           -> "Type.Method"
// map "type.method_name"      -> "Type.MethodName"
// map "some_type.method"      -> "SomeType.Method"
// map "some_type.method_name" -> "SomeType.MethodName"
func camel(s string) string {
	toUpper := true
	firstDot := true
	return strings.Map(func(r rune) rune {
		if firstDot && '.' == r {
			firstDot = false
			toUpper = true
			return r
		}
		if '_' == r {
			toUpper = true
			return -1
		}
		if toUpper {
			toUpper = false
			return unicode.ToUpper(r)
		}
		return r
	}, s)
}

func (c *serverCodec) ReadRequestHeader(r *rpc.Request) error {
	c.req.reset()
	if err := c.dec.Decode(&c.req); err != nil {
		return err
	}

	r.ServiceMethod = camel(c.req.Method)

	// JSON request id can be any JSON value;
	// RPC package expects uint64.  Translate to
	// internal uint64 and save JSON on the side.
	c.mutex.Lock()
	c.seq++
	c.pending[c.seq] = c.req.Id
	c.req.Id = nil
	r.Seq = c.seq
	c.mutex.Unlock()
	return nil
}

func (c *serverCodec) ReadRequestBody(x interface{}) error {
	if x == nil {
		return nil
	}
	if c.req.Params == nil {
		return errMissingParams
	}
	// JSON params is array value.
	// RPC params is struct.
        //     or a slice of specific type
	//     or []interface{} for a generic receive
	v := reflect.ValueOf(x)
	if reflect.Ptr == v.Kind() {
		e := v.Elem()
		switch e.Kind() {
		case reflect.Struct:
			var params [1]interface{}
			params[0] = x
			return json.Unmarshal(*c.req.Params, &params)

		case reflect.Slice:
			return json.Unmarshal(*c.req.Params, x)

		default:
		}
	}
	return errInvalidParams
}

var null = json.RawMessage([]byte("null"))

func (c *serverCodec) WriteResponse(r *rpc.Response, x interface{}) error {
	n, ok := x.(rpc.Notification)
	if ok {
		notify := serverNotify{
			Id: &null,
			Method: n.ServiceMethod,
			Params: n.Params,
		}
		return c.enc.Encode(notify)
	}

	c.mutex.Lock()
	b, ok := c.pending[r.Seq]
	if !ok {
		c.mutex.Unlock()
		return errors.New("invalid sequence number in response")
	}
	delete(c.pending, r.Seq)
	c.mutex.Unlock()

	if b == nil {
		// Invalid request so no id.  Use JSON null.
		b = &null
	}
	resp := serverResponse{Id: b}
	if r.Error == "" {
		resp.Result = x
	} else {
		resp.Error = r.Error
	}
	return c.enc.Encode(resp)
}

func (c *serverCodec) Close() error {
	return c.c.Close()
}

// ServeConn runs the JSON-RPC server on a single connection.
// ServeConn blocks, serving the connection until the client hangs up.
// The caller typically invokes ServeConn in a go statement.
func ServeConn(conn io.ReadWriteCloser) {
	rpc.ServeCodec(NewServerCodec(conn))
}
