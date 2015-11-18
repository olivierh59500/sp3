package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	sync.Mutex

	config Config

	// the 10 minute timer to clean sending grants
	lastSweepTime     time.Time
}

var upgrader = websocket.Upgrader{}
func clientWebConnect(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			log.Println("read err:", err)
			break
		}
		break
	}
}

func NewServer(conf Config) *Server {
	var addr = fmt.Sprintf("0.0.0.0:%d", conf.port)

	http.HandleFunc("/", clientWebConnect)
	http.ListenAndServe(addr, nil)

	return &Server{
		config:     conf,
	}
}
