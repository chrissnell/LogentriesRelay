package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chrissnell/syslog"
)

type handler struct {
	// To simplify implementation of our handler we embed helper
	// syslog.BaseHandler struct.
	*syslog.BaseHandler
}

type LogentriesHostEntity struct {
	Response  string `json:"response"`
	Host      Host   `json:"host"`
	Host_key  string `json:"host_key"`
	Worker    string `json:"worker"`
	Agent_key string `json:"agent_key"`
}

type Host struct {
	C        float64 `json:"c"`
	Name     string  `json:"name"`
	Distver  string  `json:"distver"`
	Hostname string  `json:"hostname"`
	Object   string  `json:"object"`
	Distname string  `json:"distname"`
	Key      string  `json:"key"`
}

type LogentriesLogEntity struct {
	Response string `json:"response"`
	Log_key  string `json:"log_key"`
	Log      Log    `json:"log"`
}

type Log struct {
	Token     string  `json:"token"`
	Created   float64 `json:"created"`
	Name      string  `json:"name`
	Retention float64 `json:"retention"`
	Filename  string  `json:"filename"`
	Object    string  `json:"object"`
	Type      string  `json:"type"`
	Key       string  `json:"key"`
	Follow    string  `json:"folow"`
}

type LogLine struct {
	Line  syslog.Message
	Token string
}

var (
	logconsumerPtr        *string
	logentriesAPIKeyPtr   *string
	listenAddrPtr         *string
	logentities           = make(map[string]LogentriesLogEntity)
	hostentities          = make(map[string]LogentriesHostEntity)
	tokenchan             = make(chan string)
	logentities_filename  = "logentries-logentities.gob"
	hostentities_filename = "logentries-hostentities.gob"
)

func newHandler() *handler {
	msg := make(chan syslog.Message)
	// Filter function name set to nil to disable filtering
	h := handler{syslog.NewBaseHandler(5, nil, false)}
	go h.mainLoop(msg)
	go ProcessLogMessage(msg)
	return &h
}

func (h *handler) mainLoop(msg chan syslog.Message) {
	for {
		m := h.Get()
		if m == nil {
			break
		}
		msg <- *m
	}
	fmt.Println("Exit handler")
	h.End()
}

func ProcessLogMessage(msg chan syslog.Message) {
	tokenfetchdone := make(chan bool, 1)
	logentrieschan := make(chan LogLine)
	lh := make(chan struct{ host, log string })

	var logline LogLine

	for m := range msg {
		if m.Hostname == "" {
			m.Hostname = "NONE"
		}
		go GetTokenForLog(tokenfetchdone, lh)
		lh <- struct{ host, log string }{m.Hostname, m.Tag}
		token := <-tokenchan
		<-tokenfetchdone

		logline.Token = token
		logline.Line = m

		go SendLogMessages(logentrieschan)
		logentrieschan <- logline

	}
}

func GetTokenForLog(tokenfetchdone chan bool, lh chan struct{ host, log string }) {
	select {
	case lht, msg_ok := <-lh:
		if !msg_ok {
			fmt.Println("msg channel closed")
		} else {

			var hostentity LogentriesHostEntity
			var logentity LogentriesLogEntity

			l := strings.Join([]string{lht.host, lht.log}, "::")

			hostentity = hostentities[lht.host]
			if hostentity.Host.Key == "" {
				hostentity = RegisterNewHost(lht.host)

				// Store our new host token in our map and sync it to disk
				hostentities[lht.host] = hostentity
				err := SyncHostEntitiesToDisk()
				if err != nil {
					log.Fatal(err)
				}
			}

			logentity = logentities[l]
			if logentity.Log.Token == "" {
				logentity := RegisterNewLog(hostentity, l)
				logentities[l] = logentity
				tokenchan <- logentity.Log.Token
				tokenfetchdone <- true
				err := SyncLogEntitiesToDisk()
				if err != nil {
					log.Fatal(err)
				}
			} else {
				tokenchan <- logentity.Log.Token
				tokenfetchdone <- true
			}
		}
	}
}

func DialLogEntries() (err error, conn net.Conn) {
	conn, err = net.Dial("tcp", *logconsumerPtr)
	if err != nil {
		fmt.Println("Could not connect to LogEntries log endpoint ", err.Error())
	}
	return err, conn
}

func SendLogMessages(msg chan LogLine) {
	err, conn := DialLogEntries()
	if err != nil {
		fmt.Println("Could not connect to LogEntries log endpoint ", err.Error())
	}

	select {
	case logline, msg_ok := <-msg:
		if !msg_ok {
			fmt.Println("msg channel closed")
		} else {
			t := logline.Line.Time
			line := fmt.Sprintf("%v %v %v %v\n", logline.Token, t.Format(time.RFC3339), logline.Line.Hostname, logline.Line.Content)
			_, err = conn.Write([]byte(line))
			if err != nil {
				log.Print("Send to Logentries endpoint failed.")
				break
			}
			// fmt.Printf("Sending line: %v", line)
		}
	}
}

func SyncLogEntitiesToDisk() (err error) {
	m := new(bytes.Buffer)
	enc := gob.NewEncoder(m)
	enc.Encode(logentities)
	err = ioutil.WriteFile("logentries-logentities.gob", m.Bytes(), 0600)
	return (err)
}

func SyncHostEntitiesToDisk() (err error) {
	m := new(bytes.Buffer)
	enc := gob.NewEncoder(m)
	enc.Encode(hostentities)
	err = ioutil.WriteFile("logentries-hostentities.gob", m.Bytes(), 0600)
	return (err)
}

func LoadLogEntitiesFromDisk() (err error) {
	n, err := ioutil.ReadFile("logentries-logentities.gob")
	if err != nil {
		return (err)
	}
	p := bytes.NewBuffer(n)
	dec := gob.NewDecoder(p)
	err = dec.Decode(&logentities)
	return (err)
}

func LoadHostEntitiesFromDisk() (err error) {
	n, err := ioutil.ReadFile("logentries-hostentities.gob")
	if err != nil {
		return (err)
	}
	p := bytes.NewBuffer(n)
	dec := gob.NewDecoder(p)
	err = dec.Decode(&hostentities)
	return (err)
}

func RegisterNewHost(h string) (he LogentriesHostEntity) {
	v := url.Values{}
	v.Set("request", "register")
	v.Set("user_key", *logentriesAPIKeyPtr)
	v.Set("name", h)
	v.Set("hostname", h)
	v.Set("distver", "")
	v.Set("system", "")
	v.Set("distname", "")
	res, err := http.PostForm("http://api.logentries.com/", v)
	if err != nil {
		log.Fatal(err)
	}
	body, err := ioutil.ReadAll(res.Body)
	err = json.Unmarshal(body, &he)
	return (he)
}

func RegisterNewLog(e LogentriesHostEntity, n string) (logentity LogentriesLogEntity) {
	v := url.Values{}
	v.Set("request", "new_log")
	v.Set("user_key", *logentriesAPIKeyPtr)
	v.Set("host_key", e.Host.Key)
	v.Set("name", n)
	v.Set("filename", "")
	v.Set("retention", "-1")
	v.Set("source", "token")
	res, err := http.PostForm("http://api.logentries.com/", v)
	if err != nil {
		log.Fatal(err)
	}
	body, err := ioutil.ReadAll(res.Body)
	err = json.Unmarshal(body, &logentity)
	return (logentity)
}

func main() {

	logconsumerPtr = flag.String("consumer", "api.logentries.com:10000", "Logentries log consumer endpoint <host:port> (Default: api.logentries.com:10000)")
	logentriesAPIKeyPtr = flag.String("apikey", "", "Logentries API key")
	listenAddrPtr = flag.String("listen", "0.0.0.0:1987", "Host/port to listen for syslog messages <host:port> (Default: 0.0.0.0:1987)")

	flag.Parse()

	if *logentriesAPIKeyPtr == "" {
		log.Fatal("Must pass a Logentries API key. Use -h for help.")
	}

	if _, err := os.Stat(logentities_filename); err == nil {
		err = LoadLogEntitiesFromDisk()
		if err != nil {
			log.Fatal(err)
		}
	}

	if _, err := os.Stat(hostentities_filename); err == nil {
		err = LoadHostEntitiesFromDisk()
		if err != nil {
			log.Fatal(err)
		}
	}

	// Create a server with one handler and run one listen gorutine
	s := syslog.NewServer()
	s.AddAllowedRunes("-._")
	s.AddHandler(newHandler())
	s.Listen(*listenAddrPtr)

	// Wait for terminating signal
	sc := make(chan os.Signal, 2)
	signal.Notify(sc, syscall.SIGTERM, syscall.SIGINT)
	<-sc

	// Shutdown the server
	fmt.Println("Shutdown the server...")
	s.Shutdown()
	fmt.Println("Server is down")
}
