package corebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func encodedRequest(t *testing.T, method bin.Object) Request {
	t.Helper()
	b := &bin.Buffer{}
	if err := method.Encode(b); err != nil {
		t.Fatal(err)
	}
	return Request{ID: 1, Method: method.(interface{ TypeName() string }).TypeName(), Payload: b.Buf}
}

func TestReadOnlyRequestProtocolAndBoundedHistory(t *testing.T) {
	for _, method := range []bin.Object{
		&tg.HelpGetConfigRequest{}, &tg.ContactsResolveUsernameRequest{Username: "testgroup"},
		&tg.MessagesCheckChatInviteRequest{Hash: "abcdefghijk"}, &tg.UpdatesGetStateRequest{},
		&tg.MessagesGetHistoryRequest{Peer: &tg.InputPeerChat{ChatID: 7}, Limit: 100},
		&tg.MessagesGetSearchCountersRequest{Peer: &tg.InputPeerChat{ChatID: 7}, Filters: []tg.MessagesFilterClass{&tg.InputMessagesFilterEmpty{}}},
	} {
		r := encodedRequest(t, method)
		if _, err := decodeRequest(r); err != nil {
			t.Fatalf("%s rejected: %v", r.Method, err)
		}
		r.Payload = append(r.Payload, 1)
		if _, err := decodeRequest(r); err == nil {
			t.Fatal("trailing payload accepted")
		}
	}
	for _, method := range []bin.Object{
		&tg.MessagesSendMessageRequest{Peer: &tg.InputPeerChat{ChatID: 7}, Message: "blocked", RandomID: 1},
		&tg.ChannelsJoinChannelRequest{Channel: &tg.InputChannel{ChannelID: 1, AccessHash: 2}},
		&tg.MessagesGetHistoryRequest{Peer: &tg.InputPeerChat{ChatID: 7}, Limit: 0},
		&tg.MessagesGetHistoryRequest{Peer: &tg.InputPeerChat{ChatID: 7}, Limit: 101},
	} {
		if _, err := decodeRequest(encodedRequest(t, method)); err == nil {
			t.Fatal("mutation or unbounded history accepted")
		}
	}
	if _, err := decodeRequest(Request{Method: "messages.getHistory", Payload: []byte{1, 2, 3}}); err == nil {
		t.Fatal("malformed TL accepted")
	}
}

func TestRPCClassificationPreservesFloodWaitWithoutLeakingValues(t *testing.T) {
	rpc := classify(tgerr.New(420, "FLOOD_WAIT_120"))
	if rpc.Code != 420 || rpc.Type != "FLOOD_WAIT" || rpc.Argument != 120 {
		t.Fatal("server wait was lost", rpc)
	}
	rpc = classify(errors.New("private credential request"))
	if rpc.Code != 500 || rpc.Type != "CORE_CONNECTION_ERROR" {
		t.Fatal("internal error exposed")
	}
	if wait, ok := tgerr.AsFloodWait(tgerr.New(rpc.Code, rpc.Type)); ok || wait != 0*time.Second {
		t.Fatal("non-RPC error became server wait")
	}
}

func TestServeRejectsInvalidConfigurationBeforeNetwork(t *testing.T) {
	for _, input := range []string{"", "not-json\n", "{}\n", `{"protocol":2,"api_id":1,"api_hash":"0123456789abcdef0123456789abcdef","session_file":"/missing"}` + "\n"} {
		var output bytes.Buffer
		if err := Serve(context.Background(), bytes.NewBufferString(input), &output); err == nil {
			t.Fatal("invalid configuration accepted")
		}
		if output.Len() != 0 {
			var frame Frame
			_ = json.Unmarshal(output.Bytes(), &frame)
			t.Fatal("invalid setup wrote protocol data", frame.Kind)
		}
	}
}
