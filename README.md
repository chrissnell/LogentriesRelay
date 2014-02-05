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
go build LogentriesRelay.go
cp LogentriesRelay <wherever you want it>
```

Usage
-----

On your syslog relay server
---------------------------
```
$ ./LogentriesRelay -apikey="your_api_key" [-consumer="host:port"] [-listen="host:port"]

-apikey="KEY"         Logentries API key

-consumer="host:port" Logentries log consumer endpoint <host:port> 
                      (Default: api.logentries.com:10000)
                      
-listen="host:port"   Host/port to listen for syslog messages <host:port>
                      (Default: 0.0.0.0:1987)
```

Logentries Token Persistence
----------------------------
LogentriesRelay talks to the Logentries API to obtain tokens for your hosts and logs.  To make these persistent after LogentriesRelay is shut down, they are stored in two ".gob" (Go Object) files that are created in the directory from which you run LogentriesRelay.   Please make sure to run LogentriesRelay from a directory where it has write and read permission.  A future revision will allow for a configurable directory to store the gob files.
