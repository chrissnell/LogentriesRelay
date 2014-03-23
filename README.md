About
-----
LogentriesRelay is an intelligent syslog relay for Logentries.com that handles auto-creation of log tokens via the Logentries API.  Logentries runs an excellent remote log aggregation service but it's not well-suited for some dynamic cloud-based environments out of the box because it requires you to pre-define the hosts and logs that you'll be sending to them.  You can get around this by installing their agent on all your servers but for some environments, that's not ideal.  LogentriesRelay provides an alternative: it acts as a syslog server and automatically sets up new hosts and new logs for you by making calls to the Logentries API.  Simply provide LogentriesRelay with your API key (AKA "account key"), then point syslog on your individual servers at LogentriesRelay and it will handle the rest.

Installation
------------
Install Go: http://golang.org/doc/install.

Then:
```
git clone git@github.com:chrissnell/LogentriesRelay.git
cd LogentriesRelay
go get github.com/chrissnell/syslog
go get github.com/go-sql-driver/mysql
go get github.com/golang/groupcache
go build LogentriesRelay.go
cp LogentriesRelay <wherever you want it>
```

Usage
-----

On your syslog relay server
---------------------------
```
$ ./LogentriesRelay -apikey="your_api_key" [-consumer="host:port"] [-listen="host:port"]

-apikey="KEY"              Logentries API key

-consumer="host:port"      Logentries log consumer endpoint <host:port> 
                           (Default: api.logentries.com:10000)
                      
-listen="host:port"        Host/port to listen for syslog messages <host:port>
                           (Default: 0.0.0.0:1987)
                      
-cachelisten="host:port"   Host/port to listen for groupcache requests  <host:port> (Default: 0.0.0.0:11000)

-peers=""                  Groupcache peers (for multi-server mode) <host:port[,host:port...]> (Default: none)

-dbhost="host:port"        MySQL database server address <host:port>
  
-dbname="name"             MySQL database name (Default: lerelay)

-dbuser="username"         MySQL username (Default: lerelay)

-dbpass="pass"             MySQL password

```

Logentries Token Persistence
----------------------------
LogentriesRelay talks to the Logentries API to obtain tokens for your hosts and logs.  To make these persistent after LogentriesRelay is shut down, they are stored in a MySQL database of your choosing.  Simply create a MySQL user and database and give that user CREATE, INSERT, UPDATE, and DELETE privileges and LogEntries will do the rest, including creating its own schema.

The database is fronted with [groupcache](https://github.com/golang/groupcache/), a distributed caching and cache-filling library.

Multi-Server Mode
-----------------
LogentriesRelay supports multi-server operation.  Simply run LogentriesRelay on multiple servers and point them to the same database server and use a load balancer (harware, HAproxy, etc.) to balance incoming syslog messages across the listening ports on each LogentriesRelay.   

For multi-server operation, it is recommended that you pass the ```-peers``` option to share groupcache caching between the servers to speed up operation.   See command line options description above or run ```LogentriesRelay -h``` for more details.
