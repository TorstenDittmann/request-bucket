package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Event struct {
	// Events are pushed to this channel by the main events-gathering routine
	Message chan string

	// New client connections
	NewClients chan chan string

	// Closed client connections
	ClosedClients chan chan string

	// Total client connections
	TotalClients map[chan string]bool
}

type Request struct {
	Timestamp int64
	Method    string
	Url       string
	UrlPath   string
	Headers   map[string]string
	Query     map[string]string
	Body      string
}

type ClientChan chan string

var requests = make([]Request, 0)

func setupRouter() *gin.Engine {
	stream := setupStream()
	router := gin.Default()
	router.LoadHTMLGlob("templates/*")

	router.GET("/__dashboard__/sse", stream.serveHTTP(), func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept")

		v, ok := c.Get("clientChan")
		if !ok {
			return
		}
		clientChan, ok := v.(ClientChan)
		if !ok {
			return
		}
		c.Stream(func(writter io.Writer) bool {
			if msg, ok := <-clientChan; ok {
				c.SSEvent("message", msg)
				return true
			}
			return false
		})
	})

	router.GET("/__dashboard__", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Main website",
		})
	})

	router.NoRoute(func(c *gin.Context) {
		var request = Request{
			Timestamp: time.Now().UnixMilli(),
			Method:    c.Request.Method,
			Url:       c.Request.URL.String(),
			UrlPath:   c.Request.URL.Path,
			Headers:   make(map[string]string),
			Query:     make(map[string]string),
		}
		for key := range c.Request.Header {
			request.Headers[key] = c.Request.Header.Get(key)
		}
		for key := range c.Request.URL.Query() {
			request.Query[key] = c.Request.URL.Query().Get(key)
		}

		body, err := io.ReadAll(c.Request.Body)
		if err == nil {
			request.Body = string(body)
		}

		requests = append(requests, request)
		raw, err := json.Marshal(request)
		if err != nil {
			panic(err)
		}
		stream.Message <- string(raw)
		c.JSON(http.StatusOK, request)

	})

	return router
}

func (stream *Event) listen() {
	for {
		select {
		case client := <-stream.NewClients:
			stream.TotalClients[client] = true
			log.Printf("Client added. %d registered clients", len(stream.TotalClients))

		case client := <-stream.ClosedClients:
			delete(stream.TotalClients, client)
			close(client)
			log.Printf("Removed client. %d registered clients", len(stream.TotalClients))

		case eventMsg := <-stream.Message:
			for clientMessageChan := range stream.TotalClients {
				clientMessageChan <- eventMsg
			}
		}
	}
}

func setupStream() (event *Event) {
	event = &Event{
		Message:       make(chan string),
		NewClients:    make(chan chan string),
		ClosedClients: make(chan chan string),
		TotalClients:  make(map[chan string]bool),
	}

	go event.listen()

	return
}

func (stream *Event) serveHTTP() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Initialize client channel
		clientChan := make(ClientChan)

		// Send new connection to event server
		stream.NewClients <- clientChan

		defer func() {
			// Send closed connection to event server
			stream.ClosedClients <- clientChan
		}()

		c.Set("clientChan", clientChan)

		c.Next()
	}
}

func main() {
	router := setupRouter()

	router.Run(":8080")
}
