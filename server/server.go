package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pborman/uuid"
)

type Server struct {
	sync.Mutex
	upgrader websocket.Upgrader
	webServer http.Server
	config Config
	destinations map[string]*websocket.Conn
  clientHosts map[string]*websocket.Conn
}

func (s Server) Authorize(hello ClientHello) (challenge string, err error) {
	if (hello.AuthenticationMethod != WEBSOCKET) {
		return "", errors.New("UNSUPPORTED")
	}
	addrHost, _, err := net.SplitHostPort(hello.DestinationAddress)
	if err != nil {
		return "", errors.New("UNSUPPORTED")
	}
	if val, ok := s.clientHosts[addrHost]; ok {
		challenge := uuid.New()
		if err = val.WriteMessage(websocket.TextMessage, []byte(challenge)); err != nil {
			return "", err
		}
		return challenge, nil;
	}

	return "", nil
}

func (s Server) Cleanup(remoteAddr string) {
	if conn, ok := s.destinations[remoteAddr]; ok {
		conn.Close()
		delete(s.destinations, remoteAddr)
		if addrHost, _, err := net.SplitHostPort(remoteAddr); err == nil {
			if cc, ok := s.clientHosts[addrHost]; ok && cc == conn {
				delete(s.clientHosts, addrHost)

				// See if there's another connection from the same address to promote.
				for addr, otherConn := range s.destinations {
					if destAddr, _, err := net.SplitHostPort(addr); err == nil && destAddr == addrHost {
						s.clientHosts[destAddr] = otherConn
						break
					}
				}
			}
		}
	}
}

func SocketHandler(server *Server) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := server.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		addrHost, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return
		}
		if _, ok := server.clientHosts[addrHost]; !ok {
			server.clientHosts[addrHost] = c
		}
		senderState := CLIENTHELLO
		var sendStream chan<- []byte
		challenge := ""

		defer server.Cleanup(r.RemoteAddr)
		for {
			msgType, msg, err := c.ReadMessage()
			if err != nil {
				log.Println("read err:", err)
				break
			}
			if (senderState == CLIENTHELLO && msgType == websocket.TextMessage) {
				hello := ClientHello{}
				err := json.Unmarshal(msg, &hello)
				if err != nil {
					log.Println("Hello err:", err)
					break
				}

				chal, err := server.Authorize(hello);
				if err != nil {
					log.Println("Authorize err:", err)
					break
				}
				challenge = chal
				senderState = HELLORECEIVED
				continue
			} else if (senderState == HELLORECEIVED && msgType == websocket.TextMessage) {
				auth := ClientAuthorization{}
				err := json.Unmarshal(msg, &auth)
				if err != nil {
					log.Println("Auth err:", err)
					break
				}
				if challenge != "" && challenge == auth.Challenge {
					senderState = AUTHORIZED
					// Further messages should now be considered a gopacket packet source.
					sendStream := CreateStream(auth.DestinationAddress)
					defer close(sendStream)
				} else {
					break
				}
				continue
			} else if (senderState == AUTHORIZED && msgType == websocket.BinaryMessage) {
				// Main forwarding loop.
				sendStream <- msg
				continue
			}
			// Else - unexpected message
			log.Println("Unexpected message", msg)
			break
		}
	})
}

func NewServer(conf Config) *Server {
	server := &Server{
		config:     conf,
		destinations: make(map[string]*websocket.Conn),
	}

	addr := fmt.Sprintf("0.0.0.0:%d", conf.port)
	mux := http.NewServeMux()
	mux.Handle("/", SocketHandler(server))
	webServer := &http.Server{Addr: addr, Handler: mux}
	webServer.ListenAndServe()

	server.webServer = *webServer
	return server
}
