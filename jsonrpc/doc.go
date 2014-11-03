// Copyright 2010 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// copied from golang pkg/net/rpc/jsonrpc
//
// modified to accept lowercase type.method
// modified to allow for a notification channel - server->client messaging
package jsonrpc
