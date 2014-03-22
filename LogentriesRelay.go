package main

import (
	"database/sql"
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
	_ "github.com/go-sql-driver/mysql"
	"github.com/golang/groupcache"
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
	db                    *sql.DB
	hostTokenCache        *groupcache.Group
	logTokenCache         *groupcache.Group
	hostIDCache           *groupcache.Group
	ctx1, ctx2, ctx3      groupcache.Context
	logconsumerPtr        *string
	logentriesAPIKeyPtr   *string
	listenAddrPtr         *string
	logentities           = make(map[string]LogentriesLogEntity)
	hostentities          = make(map[string]LogentriesHostEntity)
	tokenchan             = make(chan string)
	dbConnectDone         = make(chan sql.DB)
	logentities_filename  = "logentries-logentities.gob"
	hostentities_filename = "logentries-hostentities.gob"
)

var hostTableSchema = `
CREATE TABLE IF NOT EXISTS host (
  id int NOT NULL AUTO_INCREMENT,
  hostname varchar(60) NOT NULL,
  host_key varchar(36) NOT NULL,
  PRIMARY KEY (id)
);
`

var logTableSchema = `
CREATE TABLE IF NOT EXISTS log (
  id int NOT NULL AUTO_INCREMENT,
  host_id int NOT NULL,
  logname varchar(80) NOT NULL,
  token varchar(36) NOT NULL,
  KEY host_ind (host_id),
  PRIMARY KEY (id),
  CONSTRAINT log_ibfk_1 FOREIGN KEY (host_id) REFERENCES host (id) ON DELETE CASCADE
);`

func newHandler() *handler {
	msg := make(chan syslog.Message, 100)
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
	logentrieschan := make(chan LogLine, 100)
	lh := make(chan struct{ host, log string }, 100)

	var logline LogLine

	for m := range msg {
		if m.Hostname == "" {
			m.Hostname = "NONE"
		}

		// for debugging
		// if m.Hostname == "testpiper001" {
		//	log.Printf("recv from %v: %v\n", m.Hostname, m.Content)
		// }

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

			var host_token, host_id, log_token string

			err := hostTokenCache.Get(ctx1, lht.host, groupcache.StringSink(&host_token))
			if err != nil {
				log.Fatal(err)
			}

			err = hostIDCache.Get(ctx3, lht.host, groupcache.StringSink(&host_id))
			if err != nil {
				log.Fatal(err)
			}

			if host_token == "" {
				log.Printf("Registering host entity: %v\n", lht.host)
				host_token = RegisterNewHost(lht.host)

				host_id = SaveHostTokenToDB(lht.host, host_token)
			}

			// log_token := GetLogTokenFromDB(lht.host, lht.log)
			hostandlog := lht.host + "::" + lht.log
			err = logTokenCache.Get(ctx3, hostandlog, groupcache.StringSink(&log_token))
			if err != nil {
				log.Fatal(err)
			}

			if log_token == "" {
				log.Printf("Registering new log entity [%v]: %v\n", lht.host, lht.log)
				log_token := RegisterNewLog(host_token, lht.log)

				log.Printf("Saving log token to db.  logname: %v  log_token: %v\n", lht.log, log_token)
				SaveLogTokenToDB(host_id, lht.log, log_token)

				tokenchan <- log_token
				tokenfetchdone <- true
			} else {
				tokenchan <- log_token
				tokenfetchdone <- true
			}
		}
	}
}

func GetHostTokenFromDB(h string) (token string) {

	log.Printf("Checking DB for host: %v\n", h)

	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	err = db.QueryRow("SELECT token FROM host WHERE hostname = ?", h).Scan(&token)

	if err == sql.ErrNoRows {
		log.Print("Host does not exist in DB")
		return token
	}

	return token
}

func GetHostIDFromDB(h string) (host_id string) {

	log.Printf("Checking DB for host_id for host: %v\n", h)

	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	err = db.QueryRow("SELECT id FROM host WHERE hostname = ?", h).Scan(&host_id)

	if err == sql.ErrNoRows {
		log.Print("Host does not exist in DB")
		return host_id
	}

	return host_id

}

func GetLogTokenFromDB(h, l string) (token string) {

	log.Printf("Checking DB for log: %v / %v\n", h, l)

	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	err = db.QueryRow("SELECT log.token AS token FROM log INNER JOIN host ON log.host_id=host.id WHERE log.logname = ? AND host.hostname = ?", l, h).Scan(&token)

	if err == sql.ErrNoRows {
		return token
	}

	if err != nil {
		log.Fatal(err)
	}

	return (token)
}

func SaveHostTokenToDB(hostname string, token string) (host_id string) {

	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	stmt, err := db.Prepare("INSERT INTO host (hostname, token) VALUES(?, ?)")
	if err != nil {
		log.Fatal(err)
	}

	res, err := stmt.Exec(hostname, token)
	if err != nil {
		log.Fatal(err)
	}

	host_id_int, _ := res.LastInsertId()
	host_id = string(host_id_int)
	rowCnt, _ := res.RowsAffected()
	log.Printf("INSERT INTO host sucessful.  Rows affected: %v", rowCnt)
	return host_id
}

func SaveLogTokenToDB(host_id, logname, token string) {

	err := db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	stmt, err := db.Prepare("INSERT INTO log (host_id, logname, token) VALUES(?, ?, ?)")
	if err != nil {
		log.Fatalf("Error saving log token to DB: %v", err)
	}

	res, err := stmt.Exec(host_id, logname, token)
	if err != nil {
		log.Fatal(err)
	}

	rowCnt, _ := res.RowsAffected()
	log.Printf("INSERT INTO log sucessful.  Rows affected: %v", rowCnt)

}

func DialLogEntries() (conn net.Conn, err error) {
	for {
		conn, err = net.Dial("tcp", *logconsumerPtr)
		if err == nil {
			return conn, err
		} else {
			fmt.Println("Could not connect to LogEntries log endpoint...retrying")
			// Wait for 5 seconds before redialing
			time.Sleep(5000 * time.Millisecond)
		}
	}
}

func SendLogMessages(msg chan LogLine) {
	conn, err := DialLogEntries()
	if err != nil {
		fmt.Println("Could not connect to LogEntries log endpoint ", err.Error())
	}

	select {
	case logline, msg_ok := <-msg:
		if !msg_ok {
			fmt.Println("msg channel closed")
		} else {
			// For debugging...
			// if logline.Line.Hostname == "testpiper001" {
			//	log.Printf("send to LE from %v: %v\n", logline.Line.Hostname, logline.Line.Content)
			// }
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

func RegisterNewHost(h string) (host_token string) {
	var he LogentriesHostEntity

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
	return (he.Host_key)
}

func RegisterNewLog(ht, n string) (log_token string) {
	var logentity LogentriesLogEntity

	v := url.Values{}
	v.Set("request", "new_log")
	v.Set("user_key", *logentriesAPIKeyPtr)
	v.Set("host_key", ht)
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
	log_token = logentity.Log_key
	return (log_token)
}

func main() {
	var err error

	logconsumerPtr = flag.String("consumer", "api.logentries.com:10000", "Logentries log consumer endpoint <host:port> (Default: api.logentries.com:10000)")
	logentriesAPIKeyPtr = flag.String("apikey", "", "Logentries API key")
	listenAddrPtr = flag.String("listen", "0.0.0.0:1987", "Host/port to listen for syslog messages <host:port> (Default: 0.0.0.0:1987)")
	groupCachePeers := flag.String("peers", "", "groupcache peers <host:port> (Default: none)")
	groupCacheListenAddr := flag.String("cachelisten", "0.0.0.0:11000", "Interface to listen on for  <host:port> (Default: 0.0.0.0:11000)")

	flag.Parse()
	if *logentriesAPIKeyPtr == "" {
		log.Fatal("Must pass a Logentries API key. Use -h for help.")
	}

	// Set up groupcache peers
	peerSlice := make([]string, 1)

	if strings.Contains(*groupCachePeers, ",") {
		peerSlice = strings.Split(*groupCachePeers, ",")
	} else if *groupCachePeers != "" {
		peerSlice[0] = *groupCachePeers
	}

	if len(peerSlice) > 0 {
		for peer, value := range peerSlice {
			peerSlice[peer] = "http://" + value
		}
	}

	me := "http://" + *groupCacheListenAddr

	peerSlice = append(peerSlice, me)

	for peer, value := range peerSlice {
		log.Printf("Peer: %v  Value: %v\n", peer, value)
	}

	peers := groupcache.NewHTTPPool(me)
	peers.Set(peerSlice...)

	hostTokenCache = groupcache.NewGroup("HostTokenCache", 64<<10, groupcache.GetterFunc(
		func(ctx1 groupcache.Context, hostname string, dest groupcache.Sink) error {
			fmt.Printf("asking for %v from DB\n", hostname)
			result := GetHostTokenFromDB(hostname)
			dest.SetString(result)
			return nil
		}))

	logTokenCache = groupcache.NewGroup("LogTokenCache", 64<<10, groupcache.GetterFunc(
		func(ctx2 groupcache.Context, hostAndLog string, dest groupcache.Sink) error {
			s := strings.Split(hostAndLog, "::")
			hostname, logname := s[0], s[1]
			fmt.Printf("asking for %v, %v from DB\n", hostname, logname)
			result := GetLogTokenFromDB(hostname, logname)
			dest.SetString(result)
			return nil
		}))

	hostIDCache = groupcache.NewGroup("HostIDCache", 64<<10, groupcache.GetterFunc(
		func(ctx3 groupcache.Context, hostname string, dest groupcache.Sink) error {
			fmt.Printf("asking for %v from DB\n", hostname)
			result := GetHostIDFromDB(hostname)
			dest.SetString(string(result))
			return nil
		}))

	// Connect to the database
	db, err = sql.Open("mysql", "lerelay:UYsesKv(cjL7M4NUr}@tcp(f83ab18e40896fc40e90ce4e6b4a576f00544ea7.rackspaceclouddb.com:3306)/lerelay")
	if err != nil {
		log.Fatal(err)
	}

	// Create database schema if it doesn't exist already
	create, err := db.Prepare(hostTableSchema)
	if err != nil {
		log.Fatal(err)
	}

	_, err = create.Exec()
	if err != nil {
		log.Fatal(err)
	}

	create, err = db.Prepare(logTableSchema)
	if err != nil {
		log.Fatal(err)
	}

	_, err = create.Exec()
	if err != nil {
		log.Fatal(err)
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
