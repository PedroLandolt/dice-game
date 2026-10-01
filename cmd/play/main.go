package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const usage = "commands: wallet | play <cents> <even|odd> | end | quit"

type message struct {
	Type      string       `json:"type"`
	RequestID string       `json:"requestId"`
	Data      *playRequest `json:"data,omitempty"`
}

type playRequest struct {
	Amount int64  `json:"amount"`
	Type   string `json:"type"`
}

type reply struct {
	Type  string      `json:"type"`
	Data  replyData   `json:"data"`
	Error *replyError `json:"error"`
}

type replyData struct {
	Balance  int64      `json:"balance"`
	Currency string     `json:"currency"`
	OpenPlay *replyData `json:"openPlay"`
	Rolled   int        `json:"rolled"`
	Result   string     `json:"result"`
	Payout   int64      `json:"payout"`
	Credited int64      `json:"credited"`
}

type replyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	url := flag.String("url", "ws://localhost:8080/v1/ws", "websocket endpoint")
	token := flag.String("token", "dev-gandalf", "player token")
	flag.Parse()
	if err := run(*url, *token); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(url, token string) error {
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()

	go printReplies(ctx, conn)

	fmt.Println(usage)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "quit" {
			break
		}
		msg, err := parseCommand(line)
		if err != nil {
			fmt.Println(err)
			continue
		}
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			return fmt.Errorf("send: %w", err)
		}
	}
	return conn.Close(websocket.StatusNormalClosure, "")
}

func printReplies(ctx context.Context, conn *websocket.Conn) {
	for {
		var r reply
		if err := wsjson.Read(ctx, conn, &r); err != nil {
			return
		}
		fmt.Println(describe(r))
	}
}

func parseCommand(line string) (message, error) {
	fields := strings.Fields(line)
	requestID := rand.Text()
	switch {
	case len(fields) == 1 && fields[0] == "wallet":
		return message{Type: "wallet", RequestID: requestID}, nil
	case len(fields) == 1 && fields[0] == "end":
		return message{Type: "end_play", RequestID: requestID}, nil
	case len(fields) == 3 && fields[0] == "play":
		amount, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return message{}, errors.New("amount must be a whole number of cents, e.g. play 1250 odd")
		}
		return message{Type: "play", RequestID: requestID, Data: &playRequest{Amount: amount, Type: fields[2]}}, nil
	}
	return message{}, errors.New(usage)
}

func describe(r reply) string {
	switch r.Type {
	case "wallet_result":
		text := fmt.Sprintf("balance %s %s", euros(r.Data.Balance), r.Data.Currency)
		if open := r.Data.OpenPlay; open != nil {
			text += fmt.Sprintf(" | open play: rolled %d, %s, payout %s (send end)", open.Rolled, open.Result, euros(open.Payout))
		}
		return text
	case "play_result":
		return fmt.Sprintf("rolled %d: %s, payout %s | balance %s", r.Data.Rolled, r.Data.Result, euros(r.Data.Payout), euros(r.Data.Balance))
	case "end_play_result":
		return fmt.Sprintf("credited %s | balance %s", euros(r.Data.Credited), euros(r.Data.Balance))
	case "error":
		return fmt.Sprintf("error %s: %s", r.Error.Code, r.Error.Message)
	}
	return "unexpected reply " + r.Type
}

func euros(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
