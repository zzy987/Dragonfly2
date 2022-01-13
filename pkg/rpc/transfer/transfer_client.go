/*
 *     Copyright 2020 The Dragonfly Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package transfer

import (
	"context"
	"io"
	"sync"

	"google.golang.org/grpc/metadata"

	"golang.org/x/net/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/sets"

	"d7y.io/dragonfly/v2/pkg/rpc/pickreq"
)

// UnaryClientInterceptor returns a new unary client interceptors that performs migrate client.
func UnaryClientInterceptor(optFuncs ...CallOption) grpc.UnaryClientInterceptor {
	intOpts := reuseOrNewWithCallOptions(defaultOptions, optFuncs)
	return func(parentCtx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		grpcOpts, transferOpts := filterCallOptions(opts)
		callOpts := reuseOrNewWithCallOptions(intOpts, transferOpts)
		var lastErr error
		var firstErrSet bool
		callCtx := parentCtx
		for {
			var p peer.Peer
			currentErr := invoker(parentCtx, method, req, reply, cc, append(grpcOpts, grpc.Peer(&p))...)
			if currentErr == nil {
				return nil
			}
			if !firstErrSet {
				lastErr = currentErr
				firstErrSet = true
			}
			if isUnTransferableError(currentErr, callOpts) {
				logTrace(parentCtx, "grpc_transfer server addr: %s, got unable transfer err: %v", p.Addr, currentErr)
				return lastErr
			}
			callCtx = callContext(callCtx, p)
			lastErr = currentErr
		}
	}
}

// StreamClientInterceptor returns a new stream client interceptor that performs migrate client.
func StreamClientInterceptor(optFuncs ...CallOption) grpc.StreamClientInterceptor {
	intOpts := reuseOrNewWithCallOptions(defaultOptions, optFuncs)
	return func(parentCtx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		grpcOpts, transferOpts := filterCallOptions(opts)
		callOpts := reuseOrNewWithCallOptions(intOpts, transferOpts)
		var lastErr error
		var firstErrSet bool
		callCtx := parentCtx
		for {
			var p peer.Peer
			newStreamer, currentErr := streamer(callCtx, desc, cc, method, append(grpcOpts, grpc.Peer(&p))...)
			if currentErr == nil {
				transferStreamer := &serverStreamingTransferStream{
					ClientStream: newStreamer,
					callOpts:     callOpts,
					parentCtx:    parentCtx,
					serverPeer:   &p,
					streamerCall: func(ctx context.Context) (grpc.ClientStream, error) {
						return streamer(ctx, desc, cc, method, append(grpcOpts, grpc.Peer(&p))...)
					},
				}
				return transferStreamer, nil
			}
			if !firstErrSet {
				lastErr = currentErr
				firstErrSet = true
			}
			if isUnTransferableError(currentErr, callOpts) {
				logTrace(parentCtx, "grpc_transfer server addr: %s, got unable transfer err: %v", p.Addr, currentErr)
				return nil, lastErr
			}
			callCtx = callContext(callCtx, p)
			lastErr = currentErr
		}
	}
}

// type serverStreamingTransferStream is the implementation of grpc.ClientStream that acts as a
// proxy to the underlying call. If any of the RecvMsg() calls fail, it will try to reestablish
// a new ClientStream according to the transfer policy.
type serverStreamingTransferStream struct {
	grpc.ClientStream
	bufferedSends []interface{} // single message that the client can sen
	wasClosedSend bool          // indicates that CloseSend was closed
	parentCtx     context.Context
	callOpts      *options
	streamerCall  func(ctx context.Context) (grpc.ClientStream, error)
	serverPeer    *peer.Peer
	mu            sync.RWMutex
}

func (s *serverStreamingTransferStream) setStream(clientStream grpc.ClientStream) {
	s.mu.Lock()
	s.ClientStream = clientStream
	s.mu.Unlock()
}

func (s *serverStreamingTransferStream) getStream() grpc.ClientStream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ClientStream
}

func (s *serverStreamingTransferStream) SendMsg(m interface{}) error {
	s.mu.Lock()
	s.bufferedSends = append(s.bufferedSends, m)
	s.mu.Unlock()
	return s.getStream().SendMsg(m)
}

func (s *serverStreamingTransferStream) CloseSend() error {
	s.mu.Lock()
	s.wasClosedSend = true
	s.mu.Unlock()
	return s.getStream().CloseSend()
}

func (s *serverStreamingTransferStream) Header() (metadata.MD, error) {
	return s.getStream().Header()
}

func (s *serverStreamingTransferStream) Trailer() metadata.MD {
	return s.getStream().Trailer()
}

func (s *serverStreamingTransferStream) RecvMsg(m interface{}) error {
	attemptTransfer, lastErr := s.receiveMsgAndIndicateTransfer(m)
	if !attemptTransfer {
		return lastErr // success or hard failure
	}
	callCtx := s.parentCtx
	for {
		callCtx = callContext(callCtx, *s.serverPeer)
		newStream, err := s.reestablishStreamAndResendBuffer(callCtx)
		if err != nil {
			if isUnTransferableError(err, s.callOpts) {
				return err
			}
			continue
		}
		s.setStream(newStream)
		attemptTransfer, lastErr = s.receiveMsgAndIndicateTransfer(m)
		if !attemptTransfer {
			return lastErr
		}
	}
}

func (s *serverStreamingTransferStream) receiveMsgAndIndicateTransfer(m interface{}) (bool, error) {
	err := s.getStream().RecvMsg(m)
	if err == nil || err == io.EOF {
		return false, err
	}
	if isUnTransferableError(err, s.callOpts) {
		logTrace(s.parentCtx, "grpc_transfer parent context error: %v", s.parentCtx.Err())
		return false, err
	}
	logTrace(s.parentCtx, "grpc_transfer context error from transfer call")
	return true, err
}

func (s *serverStreamingTransferStream) reestablishStreamAndResendBuffer(callCtx context.Context) (grpc.ClientStream, error) {
	s.mu.RLock()
	bufferedSends := s.bufferedSends
	s.mu.RUnlock()
	newStream, err := s.streamerCall(callCtx)
	if err != nil {
		logTrace(callCtx, "grpc_transfer failed redialing new stream: %v", err)
		return nil, err
	}
	for _, msg := range bufferedSends {
		if err := newStream.SendMsg(msg); err != nil {
			logTrace(callCtx, "grpc_transfer failed resending message: %v", err)
			return nil, err
		}
	}
	if err := newStream.CloseSend(); err != nil {
		logTrace(callCtx, "grpc_transfer failed CloseSend on new stream %v", err)
		return nil, err
	}
	return newStream, nil
}

func logTrace(ctx context.Context, format string, a ...interface{}) {
	tr, ok := trace.FromContext(ctx)
	if !ok {
		return
	}
	tr.LazyPrintf(format, a...)
}

func isContextError(err error) bool {
	code := status.FromContextError(err).Code()
	return code == codes.DeadlineExceeded || code == codes.Canceled
}

func isUnTransferableError(err error, callOpts *options) bool {
	errCode := status.Code(err)
	if isContextError(err) {
		return true
	}
	for _, code := range callOpts.codes {
		if code == errCode {
			return true
		}
	}
	return false
}

func callContext(ctx context.Context, failedPeer peer.Peer) context.Context {
	pr, ok := pickreq.FromContext(ctx)
	if !ok {
		pr = new(pickreq.PickRequest)
	}
	if pr.FailedNodes == nil {
		pr.FailedNodes = sets.NewString()
	}
	pr.FailedNodes.Insert(failedPeer.Addr.String())
	return pickreq.NewContext(ctx, pr)
}