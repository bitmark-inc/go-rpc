// Copyright 2010 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package jsonrpc implements a JSON-RPC ClientCodec and ServerCodec
// for the rpc package.
package jsonrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	rpc "github.com/bitmark-inc/go-rpc" // "net/rpc"
	"sync"
)

type clientCodec struct {
	dec *json.Decoder // for reading JSON values
	enc *json.Encoder // for writing JSON values
	c   io.Closer

	// temporary work space
	req    clientRequest
	resp   clientResponse
	notify clientNotification
	isNotify bool

	// JSON-RPC responses include the request id but not the request method.
	// Package rpc expects both.
	// We save the request method in pending when sending a request
	// and then look it up by request ID when filling out the rpc Response.
	mutex   sync.Mutex        // protects pending
	pending map[uint64]string // map request id to method name
}

// NewClientCodec returns a new rpc.ClientCodec using JSON-RPC on conn.
func NewClientCodec(conn io.ReadWriteCloser) rpc.ClientCodec {
	return &clientCodec{
		dec:     json.NewDecoder(conn),
		enc:     json.NewEncoder(conn),
		c:       conn,
		pending: make(map[uint64]string),
	}
}

type clientRequest struct {
	Method string         `json:"method"`
	Params [1]interface{} `json:"params"`
	Id     uint64         `json:"id"`
}

func (c *clientCodec) WriteRequest(r *rpc.Request, param interface{}) error {
	c.mutex.Lock()
	c.pending[r.Seq] = r.ServiceMethod
	c.mutex.Unlock()
	c.req.Method = r.ServiceMethod
	c.req.Params[0] = param
	c.req.Id = r.Seq
	return c.enc.Encode(&c.req)
}

type clientResponse struct {
	Id     uint64           `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  interface{}      `json:"error"`
}

func (r *clientResponse) reset() {
	r.Id = 0
	r.Result = nil
	r.Error = nil
}

type clientNotification struct {
	Id     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params"`
}

func (n *clientNotification) reset() {
	n.Id = &null
	n.Method = ""
	n.Params = nil
}


func (c *clientCodec) ReadResponseHeader(r *rpc.Response) error {
	c.resp.reset()
	c.notify.reset()
	c.isNotify = false
	var raw json.RawMessage
	//if err := c.dec.Decode(&c.resp); err != nil {
	if err := c.dec.Decode(&raw); err != nil {
		return err
	}
	err1 := json.Unmarshal(raw, &c.resp)
	err2 := json.Unmarshal(raw, &c.notify)
	if nil != err1 && nil != err2 {
		return err1
	}

	// it is a notification
	if "" != c.notify.Method && nil != c.notify.Params {
		c.isNotify = true
		r.Seq = 0
		r.ServiceMethod = c.notify.Method
		r.Error = ""
		return nil
	}

	// normal result
	c.mutex.Lock()
	r.ServiceMethod = c.pending[c.resp.Id]
	delete(c.pending, c.resp.Id)
	c.mutex.Unlock()

	r.Error = ""
	r.Seq = c.resp.Id
	if c.resp.Error != nil || c.resp.Result == nil {
		x, ok := c.resp.Error.(string)
		if !ok {
			return fmt.Errorf("invalid error %v", c.resp.Error)
		}
		if x == "" {
			x = "unspecified error"
		}
		r.Error = x
	}
	return nil
}

// xx will be eithe rpc.Response or rpc.Notification
func (c *clientCodec) ReadResponseBody(x interface{}) error {
	if x == nil {
		return nil
	}
	switch x.(type) {
	case *rpc.Notification:
		n := x.(*rpc.Notification)
		n.ServiceMethod = c.notify.Method
		return json.Unmarshal(*c.notify.Params, &n.Params)
	default: // else is a result
		return json.Unmarshal(*c.resp.Result, x)
	}
}

func (c *clientCodec) Close() error {
	return c.c.Close()
}

// NewClient returns a new rpc.Client to handle requests to the
// set of services at the other end of the connection.
func NewClient(conn io.ReadWriteCloser) *rpc.Client {
	return rpc.NewClientWithCodec(NewClientCodec(conn))
}

// Dial connects to a JSON-RPC server at the specified network address.
func Dial(network, address string) (*rpc.Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), err
}
