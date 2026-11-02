// Package corebridge implements the versioned stdio transport used by
// telegram-auto. Stdout is protocol data only; no credentials are logged.
package corebridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const Protocol = 1
const MaxFrame = 32 << 20

type Config struct {
	Protocol    int    `json:"protocol"`
	APIID       int    `json:"api_id"`
	APIHash     string `json:"api_hash"`
	SessionFile string `json:"session_file"`
	Updates     bool   `json:"updates"`
}

type Request struct {
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Payload []byte `json:"payload"`
}

type RPCError struct {
	Code     int    `json:"code"`
	Type     string `json:"type"`
	Argument int    `json:"argument,omitempty"`
}

type Frame struct {
	Kind     string    `json:"kind"`
	ID       uint64    `json:"id,omitempty"`
	Protocol int       `json:"protocol,omitempty"`
	Layer    int       `json:"layer,omitempty"`
	Payload  []byte    `json:"payload,omitempty"`
	Error    *RPCError `json:"error,omitempty"`
}

type emitter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func (e *emitter) send(f Frame) error { e.mu.Lock(); defer e.mu.Unlock(); return e.encoder.Encode(f) }

type rawResult struct{ data []byte }

func (r *rawResult) Decode(b *bin.Buffer) error { r.data = b.Copy(); b.Skip(b.Len()); return nil }

// Explicit decoding prevents a general RPC proxy. The caller also restricts
// each read to its configured target group.
func decodeRequest(r Request) (bin.Object, error) {
	var method bin.Object
	switch r.Method {
	case "help.getConfig":
		method = &tg.HelpGetConfigRequest{}
	case "contacts.resolveUsername":
		method = &tg.ContactsResolveUsernameRequest{}
	case "messages.checkChatInvite":
		method = &tg.MessagesCheckChatInviteRequest{}
	case "messages.getSearchCounters":
		method = &tg.MessagesGetSearchCountersRequest{}
	case "messages.getHistory":
		method = &tg.MessagesGetHistoryRequest{}
	case "updates.getState":
		method = &tg.UpdatesGetStateRequest{}
	default:
		return nil, errors.New("method not supported")
	}
	buffer := &bin.Buffer{Buf: r.Payload}
	if method.Decode(buffer) != nil || buffer.Len() != 0 {
		return nil, errors.New("invalid request")
	}
	if h, ok := method.(*tg.MessagesGetHistoryRequest); ok && (h.Limit < 1 || h.Limit > 100) {
		return nil, errors.New("history limit is out of range")
	}
	return method, nil
}

func classify(err error) *RPCError {
	var rpc *tgerr.Error
	if errors.As(err, &rpc) {
		return &RPCError{Code: rpc.Code, Type: rpc.Type, Argument: rpc.Argument}
	}
	return &RPCError{Code: 500, Type: "CORE_CONNECTION_ERROR"}
}

// Serve establishes an already authorized session. No login or automatic
// application-level retries take place in this process.
func Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), MaxFrame)
	if !scanner.Scan() {
		return errors.New("configuration required")
	}
	var cfg Config
	if json.Unmarshal(scanner.Bytes(), &cfg) != nil || cfg.Protocol != Protocol || cfg.APIID <= 0 || len(cfg.APIHash) != 32 || !filepath.IsAbs(cfg.SessionFile) {
		return errors.New("invalid core configuration")
	}
	if _, err := readSession(cfg.SessionFile); err != nil {
		return errors.New("authorized session required")
	}
	e := &emitter{encoder: json.NewEncoder(output)}
	options := telegram.Options{NoUpdates: !cfg.Updates, SessionStorage: privateSession{cfg.SessionFile}, MaxRetries: 1,
		ReconnectionBackoff: func() backoff.BackOff { return &backoff.StopBackOff{} }, DialTimeout: 15 * time.Second, ExchangeTimeout: 15 * time.Second}
	if cfg.Updates {
		options.UpdateHandler = telegram.UpdateHandlerFunc(func(_ context.Context, u tg.UpdatesClass) error {
			b := &bin.Buffer{}
			if err := u.Encode(b); err != nil {
				return err
			}
			return e.send(Frame{Kind: "update", Payload: b.Buf})
		})
	}
	client := telegram.NewClient(cfg.APIID, cfg.APIHash, options)
	err := client.Run(ctx, func(ctx context.Context) error {
		if err := e.send(Frame{Kind: "ready", Protocol: Protocol, Layer: tg.Layer}); err != nil {
			return err
		}
		requests := make(chan Request)
		readErr := make(chan error, 1)
		go func() {
			for scanner.Scan() {
				var request Request
				if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == 0 {
					readErr <- errors.New("invalid frame")
					return
				}
				select {
				case requests <- request:
				case <-ctx.Done():
					return
				}
			}
			readErr <- scanner.Err()
		}()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-readErr:
				return err
			case request := <-requests:
				method, err := decodeRequest(request)
				if err != nil {
					if err := e.send(Frame{Kind: "result", ID: request.ID, Error: &RPCError{Code: 400, Type: "CORE_METHOD_INVALID"}}); err != nil {
						return err
					}
					continue
				}
				requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				result := &rawResult{}
				err = client.Invoke(requestCtx, method, result)
				cancel()
				frame := Frame{Kind: "result", ID: request.ID, Payload: result.data}
				if err != nil {
					frame.Error = classify(err)
				}
				if err := e.send(frame); err != nil {
					return err
				}
			}
		}
	})
	if err != nil && ctx.Err() == nil {
		_ = e.send(Frame{Kind: "fatal", Error: classify(err)})
	}
	return err
}
