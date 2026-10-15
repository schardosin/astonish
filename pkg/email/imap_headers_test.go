package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
)

func TestIMAPWirePreservesSecurityHeaders(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	wire := "From: first@example.com\r\nFrom: second@example.com\r\nAuthentication-Results: mx; dmarc=fail\r\nAuthentication-Results: mx; dmarc=pass\r\n\theader.from=example.com\r\nContent-Type: text/plain\r\n\r\nhello"
	go func() {
		defer serverConn.Close()
		fmt.Fprint(serverConn, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
		scanner := bufio.NewScanner(serverConn)
		for scanner.Scan() {
			parts := strings.Fields(scanner.Text())
			if len(parts) < 2 {
				return
			}
			tag := parts[0]
			switch strings.ToUpper(parts[1]) {
			case "SELECT":
				fmt.Fprintf(serverConn, "* 1 EXISTS\r\n* OK [UIDVALIDITY 1] valid\r\n%s OK [READ-WRITE] selected\r\n", tag)
			case "UID":
				fmt.Fprintf(serverConn, "* 1 FETCH (UID 1 FLAGS () BODY[] {%d}\r\n%s)\r\n%s OK fetched\r\n", len(wire), wire, tag)
			default:
				fmt.Fprintf(serverConn, "%s OK done\r\n", tag)
			}
		}
	}()
	conn := imapclient.New(clientConn, nil)
	defer conn.Close()
	client := &IMAPSMTPClient{cfg: &Config{}, imapConn: conn, connected: true}
	msg, err := client.ReadMessage(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.HeaderValues["From"]) != 2 || len(msg.HeaderValues["Authentication-Results"]) != 2 {
		t.Fatalf("lost wire duplicates: %#v", msg.HeaderValues)
	}
	if msg.Headers["From"] != "first@example.com" || msg.Headers["Authentication-Results"] != "mx; dmarc=fail" {
		t.Fatalf("wrong first copy: %#v", msg.Headers)
	}
	if msg.HeaderValues["From"][1] != "second@example.com" || msg.HeaderValues["Authentication-Results"][1] != "mx; dmarc=pass header.from=example.com" {
		t.Fatalf("wrong wire order/folding: %#v", msg.HeaderValues)
	}
}
