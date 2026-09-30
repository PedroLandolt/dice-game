package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/PedroLandolt/dice-game/internal/game"
)

func TestWSPlaysARound(t *testing.T) {
	play := game.Play{ID: "p1", Rolled: 3, Payout: 0, BalanceAfter: 9500}
	g := &fakeGame{play: play, state: game.WalletState{Balance: 10000, Currency: "EUR"}, playRequests: make(chan string, 1)}
	url := newWSServer(t, newTestHandler(g, nil))
	conn := dialWS(t, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token-gandalf"}}})

	wallet := `"data":{"clientId":"gandalf","balance":10000,"currency":"EUR","openPlay":null}`
	expectReply(t, conn, `{"type":"wallet_result",`+wallet+`}`)

	sendWS(t, conn, `{"type":"wallet","requestId":"a1"}`)
	expectReply(t, conn, `{"type":"wallet_result","requestId":"a1",`+wallet+`}`)

	sendWS(t, conn, `{"type":"play","requestId":"a2","data":{"amount":500,"type":"odd"}}`)
	expectReply(t, conn, `{"type":"play_result","requestId":"a2","data":{"playId":"p1","rolled":3,"result":"lose","payout":0,"balance":9500}}`)
	if got := <-g.playRequests; got != "a2" {
		t.Errorf("idempotency key = %q; want the message requestId a2", got)
	}

	sendWS(t, conn, `{"type":"end_play","requestId":"a3"}`)
	expectReply(t, conn, `{"type":"end_play_result","requestId":"a3","data":{"playId":"p1","credited":0,"balance":10000}}`)
}

func TestWSSubprotocolAuth(t *testing.T) {
	url := newWSServer(t, newTestHandler(&fakeGame{}, nil))
	conn := dialWS(t, url, &websocket.DialOptions{Subprotocols: []string{"dice.v1", "bearer.token-gandalf"}})

	if got := conn.Subprotocol(); got != "dice.v1" {
		t.Errorf("subprotocol = %q; want dice.v1 (never the token)", got)
	}
	if reply := readWS(t, conn); reply.Type != "wallet_result" {
		t.Errorf("first message = %s; want wallet_result", reply.Type)
	}
}

func TestWSRejectsMissingToken(t *testing.T) {
	url := newWSServer(t, newTestHandler(&fakeGame{}, nil))

	_, resp, err := websocket.Dial(t.Context(), url, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial without token = %v; want 401 before the upgrade", err)
	}
}

func TestWSErrorsKeepConnectionOpen(t *testing.T) {
	url := newWSServer(t, newTestHandler(&fakeGame{err: game.ErrPlayAlreadyOpen}, nil))
	conn := dialWS(t, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token-gandalf"}}})
	readWS(t, conn)

	tests := []struct {
		message       string
		wantRequestID string
		wantCode      string
	}{
		{`not json`, "", "INVALID_REQUEST"},
		{`{"type":"dance","requestId":"x1"}`, "x1", "INVALID_REQUEST"},
		{`{"type":"play","data":{"amount":500,"type":"odd"}}`, "", "INVALID_REQUEST"},
		{`{"type":"play","requestId":"x2","data":{"amount":500,"type":"odd"}}`, "x2", "PLAY_ALREADY_OPEN"},
	}
	for _, tt := range tests {
		sendWS(t, conn, tt.message)
		reply := readWS(t, conn)
		if reply.Type != "error" || reply.RequestID != tt.wantRequestID || reply.Error.Code != tt.wantCode {
			t.Errorf("reply to %s = %+v; want error %s for %q", tt.message, reply, tt.wantCode, tt.wantRequestID)
		}
	}
}

func TestWSClosesOnMessageTooBig(t *testing.T) {
	url := newWSServer(t, newTestHandler(&fakeGame{}, nil))
	conn := dialWS(t, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token-gandalf"}}})
	readWS(t, conn)

	sendWS(t, conn, `{"type":"wallet","requestId":"`+strings.Repeat("x", 2048)+`"}`)
	_, _, err := conn.Read(t.Context())
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Errorf("close status = %v; want %v", got, websocket.StatusMessageTooBig)
	}
}

func TestWSOutlivesServerTimeouts(t *testing.T) {
	srv := httptest.NewUnstartedServer(newTestHandler(&fakeGame{}, nil))
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	conn := dialWS(t, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token-gandalf"}}})
	readWS(t, conn)

	time.Sleep(600 * time.Millisecond)
	sendWS(t, conn, `{"type":"wallet","requestId":"late"}`)
	if reply := readWS(t, conn); reply.RequestID != "late" {
		t.Errorf("reply after the server timeouts = %+v; want wallet_result for late", reply)
	}
}

func newWSServer(t *testing.T, handler http.Handler) string {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws"
}

func dialWS(t *testing.T, url string, opts *websocket.DialOptions) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(t.Context(), url, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func sendWS(t *testing.T, conn *websocket.Conn, message string) {
	t.Helper()
	if err := conn.Write(t.Context(), websocket.MessageText, []byte(message)); err != nil {
		t.Fatal(err)
	}
}

func readWS(t *testing.T, conn *websocket.Conn) wsReply {
	t.Helper()
	_, data, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var reply wsReply
	if err := json.Unmarshal(data, &reply); err != nil {
		t.Fatalf("decode reply %s: %v", data, err)
	}
	return reply
}

func expectReply(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	_, data, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Errorf("reply = %s; want %s", got, want)
	}
}
