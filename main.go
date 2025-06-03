package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hashicorp/memberlist"
)

type CustomMessage struct {
	NodeName string
	Message  string
	SentAt   time.Time
}

type Delegate struct {
	nodeName         string
	Logger           *NodeLogger
	broadcasts       *memberlist.TransmitLimitedQueue // Queue for messages to broadcast
	receivedMessages chan CustomMessage               // Channel to pass received messages to main logic
}

// NewDelegate creates a new delegate
func NewDelegate(name string, logger *NodeLogger) *Delegate {
	return &Delegate{
		nodeName: name,
		broadcasts: &memberlist.TransmitLimitedQueue{
			NumNodes: func() int {
				// This needs to be updated if you have access to the memberlist instance
				// For simplicity, we'll assume a small number.
				// In a real app, pass the memberlist instance or a func to get NumMembers().
				return 10 // Placeholder
			},
			RetransmitMult: 3, // Default retransmit multiplier
		},
		receivedMessages: make(chan CustomMessage, 10), // Buffered channel
		Logger:           logger,
	}
}

// NodeMeta is used to retrieve meta-data about the current node.
func (d *Delegate) NodeMeta(limit int) []byte {
	// Example: return node name as metadata
	meta := fmt.Appendf(nil, "name:%s,pid:%d", d.nodeName, os.Getpid())
	if len(meta) > limit {
		return meta[:limit]
	}
	return meta
}

// NotifyMsg is called when a user-data message is received.
func (d *Delegate) NotifyMsg(buf []byte) {
	var msg CustomMessage
	bytes := bytes.NewBuffer(buf)
	decoder := gob.NewDecoder(bytes)
	if err := decoder.Decode(&msg); err != nil {
		d.Logger.Warnf("Received invalid message: %v", err)
		return
	}

	select {
	case d.receivedMessages <- msg:
	default:
		d.Logger.Warnf("Message queue full, dropping message from %s", msg.NodeName)

	}
}

// GetBroadcasts is called when user data messages can be broadcast.
func (d *Delegate) GetBroadcasts(overhead, limit int) [][]byte {
	// This will pull messages from the TransmitLimitedQueue
	return d.broadcasts.GetBroadcasts(overhead, limit)
}

// LocalState is used for a TCP Push/Pull.
func (d *Delegate) LocalState(join bool) []byte {
	// We can send some state during join or push/pull.
	// For this example, we'll send a simple welcome message.
	s := map[string]string{"node": d.nodeName, "status": "healthy"}
	b, _ := json.Marshal(s)
	return b
}

// MergeRemoteState is invoked after a TCP Push/Pull.
func (d *Delegate) MergeRemoteState(buf []byte, join bool) {
	var s map[string]string
	if err := json.Unmarshal(buf, &s); err == nil {
		d.Logger.Infof("Merged remote state from %s: %v (join: %t)", s["node"], s, join)
	} else {
		d.Logger.Warnf("Failed to merge remote state: %v", s["node"], err)
	}
}

// EventDelegate implements memberlist.EventDelegate
type EventDelegate struct {
	logger *NodeLogger
}

func (ed *EventDelegate) NotifyJoin(node *memberlist.Node) {
	ed.logger.Infof("Node [%s] joined", node.Address())
}

func (ed *EventDelegate) NotifyLeave(node *memberlist.Node) {
	ed.logger.Infof("Node [%s] left", node.Address())
}

func (ed *EventDelegate) NotifyUpdate(node *memberlist.Node) {
	ed.logger.Infof("Node [%s] updated", node.Address())
}

func main() {
	nodeName := flag.String("name", "", "Unique node name (required)")
	bindPort := flag.Int("port", 8000, "Port to bind to for memberlist")
	joinAddr := flag.String("join", "", "Address of an existing node to join (e.g., 127.0.0.1:7946)")
	flag.Parse()

	if *nodeName == "" {
		log.Fatal("-name is required")
	}

	logger := NewNodeLogger(*nodeName, 0)
	delegate := NewDelegate(*nodeName, logger)
	eventDelegate := &EventDelegate{logger: logger}

	// Memberlist configuration
	config := memberlist.DefaultLANConfig() // Or DefaultWANConfig, DefaultLocalConfig
	config.Name = *nodeName
	config.BindAddr = "0.0.0.0"
	config.BindPort = *bindPort
	// config.AdvertiseAddr = hostname // Set if different from BindAddr (e.g. behind NAT)
	// config.AdvertisePort = *bindPort
	config.Delegate = delegate
	config.Events = eventDelegate
	config.LogOutput = io.Discard

	// Create memberlist instance
	list, err := memberlist.Create(config)
	if err != nil {
		log.Fatalf("[%s] FATAL: Failed to create memberlist: %s", *nodeName, err.Error())
	}

	// Update NumNodes for TransmitLimitedQueue once list is created
	delegate.broadcasts.NumNodes = func() int { return list.NumMembers() }

	// Join an existing cluster if specified
	if *joinAddr != "" {
		parts := strings.Split(*joinAddr, ",")
		if _, err := list.Join(parts); err != nil {
			logger.Warnf("Failed to join cluster at %s", joinAddr)
			// Depending on your needs, you might want to exit or retry
		}
	}

	logger.Infof("Memberlist started. Name: %s, Address: %s:%d", config.BindAddr, config.BindPort)
	logger.Infof("To join this node, use: -join %s:%d", list.LocalNode().Addr.String(), list.LocalNode().Port)

	// Goroutine to process received messages
	go func() {
		for msg := range delegate.receivedMessages {
			logger.Rxf("<- Receiving message from: '%s' : '%s'", msg.NodeName, msg.Message)
		}
	}()

	// Goroutine to periodically broadcast a message
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			members := list.Members()
			targetNode := members[rand.Int()%len(members)]
			if targetNode.Name == *nodeName {
				continue // Skip self, try again later
			}
			directMsg := CustomMessage{
				NodeName: *nodeName,
				Message:  fmt.Sprintf("hello to %s!", targetNode.Name),
				SentAt:   time.Now(),
			}
			var buf bytes.Buffer
			encoder := gob.NewEncoder(&buf)
			if err := encoder.Encode(directMsg); err != nil {
				logger.Errorf("Failed to encode custom message: %v", err)
				continue
			}
			if err := list.SendReliable(targetNode, buf.Bytes()); err != nil {
				logger.Errorf("Failed to send direct TCP message to %s: %v", targetNode.Name, err)
				continue
			}
			logger.Txf("-> Sending message to %s", targetNode.Name)
		}
	}()

	// Wait for termination signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	logger.Infof("Shutting down...")
	if err := list.Leave(10 * time.Second); err != nil { // Timeout for leaving
		logger.Warnf("Error leaving cluster: %v", err)
	}
	if err := list.Shutdown(); err != nil {
		logger.Warnf("Error shutting down memberlist: %v", err)
	}
	logger.Infof("Shutdown complete.")
}
