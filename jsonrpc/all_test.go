// Copyright 2010 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	rpc "github.com/bitmark-inc/go-rpc" // "net/rpc"
	"strings"
	"testing"
	"time"
)

type Args struct {
	A, B int
}

type Reply struct {
	C int
}

type Arith int

type ArithAddResp struct {
	Id     interface{} `json:"id"`
	Result Reply       `json:"result"`
	Error  interface{} `json:"error"`
}

func (t *Arith) Add(args *Args, reply *Reply) error {
	reply.C = args.A + args.B
	return nil
}

func (t *Arith) AddAgain(args *Args, reply *Reply) error {
	reply.C = args.A + args.B
	return nil
}

type IntArray []int
// test the normal array type of JSON RPC
func (t *Arith) SumP(args *IntArray, reply *Reply) error {
	s := 0
	for _, n := range *args {
		s += n
	}
	reply.C = s
	return nil
}

func (t *Arith) SumA(args IntArray, reply *Reply) error {
	s := 0
	for _, n := range args {
		s += n
	}
	reply.C = s
	return nil
}

func (t *Arith) MixedP(args *[]interface{}, reply *Reply) error {
	s := 0
	for _, n := range *args {
		switch n.(type) {
		case float64:  // JSON default number type
			s += int(n.(float64))
		case string:
			s += len(n.(string))
		default:  // ignore
		}
	}
	reply.C = s
	return nil
}

type GenericArguments []interface{}
func (t *Arith) MixedA(args GenericArguments, reply *Reply) error {
	s := 0
	for _, n := range args {
		switch n.(type) {
		case float64:  // JSON default number type
			s += int(n.(float64))
		case string:
			s += len(n.(string))
		default:  // ignore
		}
	}
	reply.C = s
	return nil
}

func (t *Arith) Mul(args *Args, reply *Reply) error {
	reply.C = args.A * args.B
	return nil
}

func (t *Arith) Div(args *Args, reply *Reply) error {
	if args.B == 0 {
		return errors.New("divide by zero")
	}
	reply.C = args.A / args.B
	return nil
}

func (t *Arith) Error(args *Args, reply *Reply) error {
	panic("ERROR")
}

func init() {
	rpc.Register(new(Arith))
}
// send 10 notifications at 5ms intervals
func backgroundNotifier(server *rpc.Server, count *int, stop <-chan bool) {
	*count = 0
	const maxCount = 10
	const interval = 5 * time.Millisecond
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-time.After(interval):
			if *count >= maxCount {
				break loop
			}
			*count += 1
			message := fmt.Sprintf("message: %d", *count)
			server.SendNotification("Hello.World", []string{message})
		}
	}
}

func TestServerNoParams(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go ServeConn(srv)
	dec := json.NewDecoder(cli)

	fmt.Fprintf(cli, `{"method": "Arith.Add", "id": "123"}`)
	var resp ArithAddResp
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("Decode after no params: %s", err)
	}
	if resp.Error == nil {
		t.Fatalf("Expected error, got nil")
	}
}

func TestServerEmptyMessage(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go ServeConn(srv)
	dec := json.NewDecoder(cli)

	fmt.Fprintf(cli, "{}")
	var resp ArithAddResp
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("Decode after empty: %s", err)
	}
	if resp.Error == nil {
		t.Fatalf("Expected error, got nil")
	}
}

func TestServer(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go ServeConn(srv)
	dec := json.NewDecoder(cli)

	// Send hand-coded requests to server, parse responses.
	for i := 0; i < 10; i++ {
		fmt.Fprintf(cli, `{"method": "Arith.Add", "id": "\u%04d", "params": [{"A": %d, "B": %d}]}`, i, i, i+1)
		var resp ArithAddResp
		err := dec.Decode(&resp)
		if err != nil {
			t.Fatalf("Decode: %s", err)
		}
		if resp.Error != nil {
			t.Fatalf("resp.Error: %s", resp.Error)
		}
		if resp.Id.(string) != string(i) {
			t.Fatalf("resp: bad id %q want %q", resp.Id.(string), string(i))
		}
		if resp.Result.C != 2*i+1 {
			t.Fatalf("resp: bad result: %d+%d=%d", i, i+1, resp.Result.C)
		}
	}
}

func TestSum(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go ServeConn(srv)

	client := NewClient(cli)
	defer client.Close()

	// array
	m := []string{"Arith.SumP", "Arith.SumA"}
	for _, method := range m {
		args := &[]int{1, 3, 5, 7, 9, 11}
		reply := new(Reply)
		err := client.Call(method, args, reply)
		if err != nil {
			t.Errorf("%s: expected no error but got string %q", method, err.Error())
		}
		s := 0
		for _, n := range *args {
			s += n
		}
		if reply.C != s {
			t.Errorf("%s: got %d expected %d", method, reply.C, s)
		}
	}
}

func TestMixed(t *testing.T) {
	cli, srv := net.Pipe()
	defer cli.Close()
	go ServeConn(srv)

	client := NewClient(cli)
	defer client.Close()

	// array
	m := []string{"Arith.MixedP", "Arith.MixedA"}
	for _, method := range m {
		args := &[]interface{}{1, 3, 5, "hello", 7, "world", true, 9, 11, nil, 13}
		reply := new(Reply)
		err := client.Call(method, args, reply)
		if err != nil {
			t.Errorf("%s: expected no error but got string %q", method, err.Error())
		}
		s := 1 + 3 + 5 + 7 + 9 + 11 + 13 + len("hello") + len("world")
		if reply.C != s {
			t.Errorf("%s: got %d expected %d", method, reply.C, s)
		}
	}
}

func TestClient(t *testing.T) {
	// Assume server is okay (TestServer is above).
	// Test client against server.
	cli, srv := net.Pipe()
	go ServeConn(srv)

	client := NewClient(cli)
	defer client.Close()

	// check counts
	receivedCount := 0
	sentCount := 0
	defer func() {
		if 0 == receivedCount {
			t.Error("notification count was zero")
		}
		if sentCount != receivedCount {
			t.Errorf("new notification count error sent: %d  received: %d", sentCount, receivedCount)
		}
	}()

	// notification callback
	client.SetCallback(func(method string, params interface{}) {
		receivedCount += 1
		t.Logf("received notification: method=%s  params=%v\n", method, params)
	})

	// async notifier
	stop := make(chan bool)
	defer close(stop)

	go backgroundNotifier(rpc.DefaultServer, &sentCount, stop)

	// Synchronous calls
	args := &Args{7, 8}
	reply := new(Reply)
	err := client.Call("Arith.Add", args, reply)
	if err != nil {
		t.Errorf("Add: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("Add: got %d expected %d", reply.C, args.A+args.B)
	}

	// time for a notification to get through
	time.Sleep(5 * time.Millisecond)

	args = &Args{7, 8}
	reply = new(Reply)
	err = client.Call("Arith.Mul", args, reply)
	if err != nil {
		t.Errorf("Mul: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A*args.B {
		t.Errorf("Mul: got %d expected %d", reply.C, args.A*args.B)
	}

	// Out of order.
	args = &Args{7, 8}
	mulReply := new(Reply)
	mulCall := client.Go("Arith.Mul", args, mulReply, nil)
	addReply := new(Reply)
	addCall := client.Go("Arith.Add", args, addReply, nil)

	addCall = <-addCall.Done
	if addCall.Error != nil {
		t.Errorf("Add: expected no error but got string %q", addCall.Error.Error())
	}
	if addReply.C != args.A+args.B {
		t.Errorf("Add: got %d expected %d", addReply.C, args.A+args.B)
	}

	mulCall = <-mulCall.Done
	if mulCall.Error != nil {
		t.Errorf("Mul: expected no error but got string %q", mulCall.Error.Error())
	}
	if mulReply.C != args.A*args.B {
		t.Errorf("Mul: got %d expected %d", mulReply.C, args.A*args.B)
	}

	// time for a notification to get through
	time.Sleep(5 * time.Millisecond)

	// Error test
	args = &Args{7, 0}
	reply = new(Reply)
	err = client.Call("Arith.Div", args, reply)
	// expect an error: zero divide
	if err == nil {
		t.Error("Div: expected error")
	} else if err.Error() != "divide by zero" {
		t.Error("Div: expected divide by zero error; got", err)
	}

	// wait for all remaining notifications
	time.Sleep(50 * time.Millisecond)
}

// try lower case method names, also include '_'
func TestClientLower(t *testing.T) {
	// Assume server is okay (TestServer is above).
	// Test client against server.
	cli, srv := net.Pipe()
	go ServeConn(srv)

	client := NewClient(cli)
	defer client.Close()

	// Synchronous calls
	args := &Args{7, 8}
	reply := new(Reply)
	err := client.Call("arith.add", args, reply)
	if err != nil {
		t.Errorf("Add: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("Add: got %d expected %d", reply.C, args.A+args.B)
	}

	args = &Args{7, 8}
	reply = new(Reply)
	err = client.Call("arith.add_again", args, reply)
	if err != nil {
		t.Errorf("AddAgain: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A+args.B {
		t.Errorf("AddAgain: got %d expected %d", reply.C, args.A+args.B)
	}

	args = &Args{7, 8}
	reply = new(Reply)
	err = client.Call("arith.mul", args, reply)
	if err != nil {
		t.Errorf("Mul: expected no error but got string %q", err.Error())
	}
	if reply.C != args.A*args.B {
		t.Errorf("Mul: got %d expected %d", reply.C, args.A*args.B)
	}

	// Out of order.
	args = &Args{7, 8}
	mulReply := new(Reply)
	mulCall := client.Go("arith.mul", args, mulReply, nil)
	addReply := new(Reply)
	addCall := client.Go("arith.add", args, addReply, nil)

	addCall = <-addCall.Done
	if addCall.Error != nil {
		t.Errorf("Add: expected no error but got string %q", addCall.Error.Error())
	}
	if addReply.C != args.A+args.B {
		t.Errorf("Add: got %d expected %d", addReply.C, args.A+args.B)
	}

	mulCall = <-mulCall.Done
	if mulCall.Error != nil {
		t.Errorf("Mul: expected no error but got string %q", mulCall.Error.Error())
	}
	if mulReply.C != args.A*args.B {
		t.Errorf("Mul: got %d expected %d", mulReply.C, args.A*args.B)
	}

	// Error test
	args = &Args{7, 0}
	reply = new(Reply)
	err = client.Call("arith.div", args, reply)
	// expect an error: zero divide
	if err == nil {
		t.Error("Div: expected error")
	} else if err.Error() != "divide by zero" {
		t.Error("Div: expected divide by zero error; got", err)
	}
}

func TestMalformedInput(t *testing.T) {
	cli, srv := net.Pipe()
	go cli.Write([]byte(`{id:1}`)) // invalid json
	ServeConn(srv)                 // must return, not loop
}

func TestMalformedOutput(t *testing.T) {
	cli, srv := net.Pipe()
	go srv.Write([]byte(`{"id":0,"result":null,"error":null}`))
	go ioutil.ReadAll(srv)

	client := NewClient(cli)
	defer client.Close()

	args := &Args{7, 8}
	reply := new(Reply)
	err := client.Call("Arith.Add", args, reply)
	if err == nil {
		t.Error("expected error")
	}
}

func TestServerErrorHasNullResult(t *testing.T) {
	var out bytes.Buffer
	sc := NewServerCodec(struct {
		io.Reader
		io.Writer
		io.Closer
	}{
		Reader: strings.NewReader(`{"method": "Arith.Add", "id": "123", "params": []}`),
		Writer: &out,
		Closer: ioutil.NopCloser(nil),
	})
	r := new(rpc.Request)
	if err := sc.ReadRequestHeader(r); err != nil {
		t.Fatal(err)
	}
	const valueText = "the value we don't want to see"
	const errorText = "some error"
	err := sc.WriteResponse(&rpc.Response{
		ServiceMethod: "Method",
		Seq:           1,
		Error:         errorText,
	}, valueText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), errorText) {
		t.Fatalf("Response didn't contain expected error %q: %s", errorText, &out)
	}
	if strings.Contains(out.String(), valueText) {
		t.Errorf("Response contains both an error and value: %s", &out)
	}
}

func TestUnexpectedError(t *testing.T) {
	cli, srv := myPipe()
	go cli.PipeWriter.CloseWithError(errors.New("unexpected error!")) // reader will get this error
	ServeConn(srv)                                                    // must return, not loop
}

// Copied from package net.
func myPipe() (*pipe, *pipe) {
	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()

	return &pipe{r1, w2}, &pipe{r2, w1}
}

type pipe struct {
	*io.PipeReader
	*io.PipeWriter
}

type pipeAddr int

func (pipeAddr) Network() string {
	return "pipe"
}

func (pipeAddr) String() string {
	return "pipe"
}

func (p *pipe) Close() error {
	err := p.PipeReader.Close()
	err1 := p.PipeWriter.Close()
	if err == nil {
		err = err1
	}
	return err
}

func (p *pipe) LocalAddr() net.Addr {
	return pipeAddr(0)
}

func (p *pipe) RemoteAddr() net.Addr {
	return pipeAddr(0)
}

func (p *pipe) SetTimeout(nsec int64) error {
	return errors.New("net.Pipe does not support timeouts")
}

func (p *pipe) SetReadTimeout(nsec int64) error {
	return errors.New("net.Pipe does not support timeouts")
}

func (p *pipe) SetWriteTimeout(nsec int64) error {
	return errors.New("net.Pipe does not support timeouts")
}
