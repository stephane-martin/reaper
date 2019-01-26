# reaper

`reaper` is a simple tool to collect access logs from web servers and publish the logs to an external message queue.


```
                                                                ,,,,,          ,,,,,       
                                                              ,,,,,,,,,     ,,,,,,,,,,     
                                                             ,,,,,,,,,,,,  ,,,,,,,,,,,,                            
                                                            ,,,,,,,,,,,,,,,,,,,,,,,,,,,,                           
                              ##                           ,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,                          
                           ####                           ,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,                          
                         #####                            ,,,@@@@@*,,,,,,,,,,,,,,,@@@@@,,,                         
                       ######                            ,,,,,#@@@@@&,,,,,,,,,,/@@@@@@,,,,                         
                     #######                             ,,,,,,,@@@@@@,,,,,,,,@@@@@@,,,,,,,                        
                  #########                              ,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,                        
                 ##########                              ,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,                        
               ###########                               ,,,,,,,,,,,,,,,,(/,,,,,,,,,,,,,,,,                        
              ###########                                ,,,,,,@,/@@,,@,,@@,,@,,@@*,@,,,,,,                        
             ############                                 ,,,,,,@@@@@@&@@@@@@@@@@@@@,,,,,,                         
           #############                                   ,,,,,,,@@@@@@@@@@@@@@@@,,,,,,,                          
          ##############                                      ,,,,@,@@@@@@@@@@@@,@,,,,                             
         ##############                                          ,,,,@@,@@@@,@@,,*,                                
         ##############                                           .,,,*,,@@,,/,,,                                  
        ###############                                             ,,,,,%#,,,,,                                   
       ###############                                               ,,,,,,,,,,                                    
       ###############                                               .,,,,,,,,                                     
      ################                                                                                             
      ################                                                                                             
      ################                                                                                             
      ################                                            GIVE ME YOUR LOGS                
      ################                                                                                             
       ##################                                                                                          
        ##################                                                                                         
        ###################                                                                              .////.    
         ####################                                                                        ///////////   
          #####################                                                                       ////.  *///  
           #################                                                                           *//      // 
            ########                                                                                    /        /       
```


## Features

- Collect log on TCP/UDP syslog
- Syslog RFC3154 or RFC5424
- Collect log on stdin
- Parse logs formats: JSON, key/values, common, combined
- Stream access logs with websocket
- Download logs with HTTP
- Filter out unwanted log lines (predicate in Javascript)
- Can write collected logs to stdout, stderr, file
- Can write collected logs to databases: PostgreSQL/TimescaleDB, Elasticsearch
- Can write collected logs to message brokers: RabbitMQ, nsqd, STOMP enabled message broker
- Can write collected logs to a distributed log: Kafka
- Can write collected logs to a redis list
- Can forward collected logs to another reaper instance
- Should work on any *NIX

## Project status

Alpha.
Version 0.1.0.

reaper is functional and be used in simple environments. But it lacks proper test cases and performance testing in busy
environments.

## Getting Started

### Installing

-   Binary releases

    https://github.com/stephane-martin/reaper/releases
    
    Just copy the binary in your PATH.
    

-   Compile from source

    The Go compiler and `dep` (https://golang.github.io/dep) are required
    
    `git clone https://github.com/stephane-martin/reaper` in an appropriate folder (GOPATH...)
    
    `make debug` or `make release`
    
-   Compile from source using Docker

    If you can't install Golang and dep, you can also compile reaper using Docker
    
    `git clone https://github.com/stephane-martin/reaper`
    
    `sudo make dockerbuild`
    
### Configure

Currently reaper does not use a configuration file. Arguments are passed on the command line or with
environment variables.

### Inline help

`reaper --help`

`reaper (command) --help`

### Use reaper

#### Listen for access log entries

##### TCP syslog

Start reaper with `--tcp 127.0.0.1:1514`. Here 127.0.0.1 is the listen address.

##### UDP syslog

Start reaper with `--udp 127.0.0.1:1514`.

This can be used with nginx or caddy. In nginx.conf:

```
access_log syslog:server=127.0.0.1:1514,facility=daemon,tag=nginxaccess,severity=info jrich;
```

##### Syslog protocol

By default the syslog protocol is supposed to be RFC3164. Use the global flag '--rfc5424' to switch to RFC5424.

##### stdin

Start reaper with `--stdin`.

This can be used with Apache. For example in Apache configuration:

```
CustomLog "||/path/to/reaper --format combined --stdin" combined
```

#### Configure access logs format

reaper needs to know the format in which the web server writes access logs entries. Use the `--format` flag.

##### JSON

`reaper --udp 127.0.0.1:1514 --format json`

Example nginx configuration:

```
log_format jrich escape=json
    '{'
        '"timestamp":"$time_iso8601",'
        '"method":"$request_method",'
        '"scheme":"$scheme",'
        '"host":"$host",'
        '"server":"$server_name",'
        '"uri":"$uri",'
        '"duration":$request_time,'
        '"length":$request_length,'
        '"status":$status,'
        '"sent":$bytes_sent,'
        '"agent":"$http_user_agent",'
        '"remoteaddr":"$remote_addr",'
        '"remoteuser":"$remote_user"'
    '}';

access_log syslog:server=127.0.0.1:1514,facility=daemon,tag=nginxaccess,severity=info jrich;
```

##### Key/values

`reaper --udp 127.0.0.1:1514 --format kv`

Example nginx configuration:

```
log_format rich
    'remote_addr="$remote_addr" remote_user="$remote_user" time="$time_iso8601" length=$request_length'
    ' host="$host" request="$request_uri" uri="$uri" status=$status bytes_sent=$bytes_sent agent="$http_user_agent"'
    ' duration=$request_time upstream_duration=$upstream_response_time method="$request_method" scheme="$scheme"'
    ' server="$server_name"';
```
    
##### common log format

`reaper --udp 127.0.0.1:1514 --format common`

##### combined log format

`reaper --udp 127.0.0.1:1514 --format combined`

#### Filter access logs

The `--filterout EXPR` global flag can be set to specify a filter. 

EXPR is a javascript expression that can use the log entry fields. If the EXPR is True, the entry is filtered out.
Multiple --filterout flags can be used. In that case, an entry is filtered out if any of the expressions is True.

Example:

`reaper --udp 127.0.0.1:1514 --format json --filterout 'host=="example.org"' stdout`

Log entries for requests to http://example.org will be filtered out.

Please note that filtering is not free from a performance point of view. It uses an embedded Javascript engine.

#### Forward access logs

reaper can forward access logs to various destinations. The type of the destination is selected through a command on
reaper command line, after the previous global flags.

When the destination is not reachable, log entries are buffered in the embedded nsqd instance. When the destination is
reachable again, buffered entries will be forwarded. So you do not need to start the destination before reaper.

Each destination has specific flags to configure it.

##### stdout, stderr

-   `reaper --udp 127.0.0.1 stdout`
-   `reaper --udp 127.0.0.1 stderr`

##### file

-   `reaper --udp 127.0.0.1 file --filename /tmp/access.log` => write log entries to /tmp/access.log
-   `reaper --udp 127.0.0.1 file --gzip --filename /tmp/access.log.gz` => write compressed log entries to /tmp/access.log.gz

##### RabbitMQ

Forward logs to a RabbitMQ exchange.

`reaper --udp 127.0.0.1 rabbitmq --uri "amqp://guest:guest@localhost:5672/" --exchange exname --routing-key key --type direct`

This will forward entries to a RabbitMQ broker, located at localhost:5672, using guest/guest as credentials,
to the / virtual host, in the direct exchange exname, and with "key" as a routing key.

##### STOMP

`./reaper_debug --udp 127.0.0.1:1514 stomp --login user --passcode password --host virtualhost --destination /queue/reaper --addr 192.168.1.2:61613`

##### Elasticsearch

Forward logs to an Elasticsearch server.

`reaper --udp 127.0.0.1 elasticsearch --url http://127.0.0.1:9200 --index indexname`

##### Redis

Forward logs to Redis, using a redis list (think LPOP, RPUSH). 

`reaper --udp 127.0.0.1 redis --addr 127.0.0.1:6379 --listname thelistkey --database 6 --password pass`

##### Kafka

`reaper --udp 127.0.0.1 kafka --broker 192.168.1.2:9092 --broker 192.168.1.3:9092 --broker 192.168.1.4:9092 --topic topicname`

##### PostgreSQL/TimescaleDB

First you need to create a table in PostgreSQL that is consistent with the log format.

For example:

```
+------------+--------------------------+-------------------+
| Column     | Type                     | Modifiers         | 
|------------+--------------------------+-------------------+
| timestamp  | timestamp with time zone |  not null         |
| method     | text                     |  default ''::text |
| scheme     | text                     |  default ''::text |
| host       | text                     |  default ''::text |
| server     | text                     |  default ''::text |
| uri        | text                     |  default ''::text |
| duration   | double precision         |  default 0        |
| length     | integer                  |  default 0        |
| status     | integer                  |  default 0        |
| sent       | integer                  |  default 0        |
| agent      | text                     |  default ''::text |
| remoteaddr | text                     |  default ''::text |
| remoteuser | text                     |  default ''::text |
+------------+--------------------------+-------------------+

Indexes:
    "reaper_duration_timestamp_idx" btree (duration, "timestamp" DESC)
    "reaper_host_timestamp_idx" btree (host, "timestamp" DESC)
    "reaper_length_timestamp_idx" btree (length, "timestamp" DESC)
    "reaper_method_timestamp_idx" btree (method, "timestamp" DESC)
    "reaper_remoteaddr_timestamp_idx" btree (remoteaddr, "timestamp" DESC)
    "reaper_scheme_timestamp_idx" btree (scheme, "timestamp" DESC)
    "reaper_sent_timestamp_idx" btree (sent, "timestamp" DESC)
    "reaper_server_timestamp_idx" btree (server, "timestamp" DESC)
    "reaper_timestamp_idx" btree ("timestamp" DESC)
```

Then:

```
reaper --udp 127.0.0.1:1514 pgsql \
    --uri "postgres://user:password@127.0.0.1/dbname"
    --table tablename
    --fields "timestamp,method,scheme,host,server,uri,duration,length,status,sent,agent,remoteaddr,remoteuser"    
```

##### External nsqd

`reaper --udp 127.0.0.1:1514 nsq --addr 192.168.1.2:4150 --topic topicname --json`

##### Forward to another reaper instance

On machine A 192.168.1.2 (with web server):

`reaper --udp 127.0.0.1:1514 nsq --addr 192.168.1.3:4150 --topic embedded`

On machine B 192.168.1.3:

`reaper --nsqd-address 192.168.1.3 --nsqd-tcp-port 4150 pgsql ...`

#### HTTP API

If started with `--http-address`, reaper exposes a HTTP API.

Endpoints:

-   /status => just returns 200 HTTP status code.
-   /metrics => prometheus metrics (with the embedded nsqd metrics).

-   POST /download/:clientid?wait=3000&size=1000 => creates a channel of access logs entries and download entries.

    size is the number of entries to be returned.
    wait is the number of milliseconds to wait
    
    After the first POST call, a nsq channel is created. All received entries will be copied to this channel.
    Each successive POST call with return different entries.
    
-   DELETE /download/:clientid => delete a previously created channel

#### Websocket API

If started with `--websocket-address`, reaper exposes a websocket endpoint.

-   /stream: stream received entries to the websocket client.

#### Logging

By default reaper own logs are written on stderr.

The logging level can be set with `--loglevel` [debug, info, warn, error, crit].

Alternatively reaper can use syslog with `--syslog`

## Design

reaper embeds a nsqd service (https://nsq.io). When access logs entries are received on TCP, UDP or stdin, they are
first stored in the embedded nsqd. Thus, reaper only deletes an access log entry when it has been reliably sent to the
configured destination.

Forwarding to the destination is done asynchronously to achieve good performance.

## Changelog

https://github.com/stephane-martin/reaper/blob/master/CHANGELOG.md

