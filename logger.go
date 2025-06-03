package main

import (
	"fmt"
	"log"
	"os"
)

const (
	Reset    = "\033[0m"
	RedBG    = "\033[41m"
	YellowBG = "\033[43m"
	BlueBG   = "\033[44m"
	GreenBG  = "\033[42m"
	CyanBG   = "\033[46m"
)

type NodeLogger struct {
	log      *log.Logger
	nodeName string
}

func NewNodeLogger(nodeName string, flag int) *NodeLogger {
	return &NodeLogger{
		log:      log.New(os.Stderr, fmt.Sprintf("[%s] ", nodeName), flag),
		nodeName: nodeName,
	}
}

func (l *NodeLogger) Txf(format string, v ...any) {
	l.log.Printf(GreenBG+" TX "+Reset+" "+format, v...)
}

func (l *NodeLogger) Rxf(format string, v ...any) {
	l.log.Printf(CyanBG+" RX "+Reset+" "+format, v...)
}

func (l *NodeLogger) Infof(format string, v ...any) {
	l.log.Printf(BlueBG+" INFO "+Reset+" "+format, v...)
}

func (l *NodeLogger) Warnf(format string, v ...any) {
	l.log.Printf(YellowBG+" WARN "+Reset+" "+format, v...)
}

func (l *NodeLogger) Errorf(format string, v ...any) {
	l.log.Printf(RedBG+" ERROR "+Reset+" "+format, v...)
}
