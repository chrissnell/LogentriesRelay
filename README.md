About
===============
LogentriesRelay is an intelligent syslog relay for Logentries.com that handles auto-creation of log tokens via the Logentries API.  Logentries runs an excellent remote log aggregation service but it's not well-suited for some dynamic cloud-based environments out of the box because it requires you to pre-define the hosts and logs that you'll be sending to them.  You can get around this by installing their agent on all your servers but for many, that's not ideal.  LogentriesRelay provides an alternative: it acts as a syslog server and automatically sets up new hosts and new logs for you by making calls to the Logentries API.  Simply provide LogentriesRelay with your API key (AKA "account key"), then point syslog on your individual servers at LogentriesRelay and it will handle the rest.

Usage
=====

On your syslog relay server
---------------------------
```
$ ./LogentriesRelay -apikey="your_api_key" [-consumer="host:port"] [-listen="host:port"]

-apikey="KEY"    Logentries API key
-consumer="host:port" Logentries log consumer endpoint <host:port> 
                      (Default: api.logentries.com:10000)
-listen="host:port"   Host/port to listen for syslog messages <host:port>
                      (Default: 0.0.0.0:1987)
```
