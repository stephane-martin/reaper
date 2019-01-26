# Changelog
All notable changes to this project will be documented in this file.

## [0.1.0] - 2019-01-25

Initial release.

Supports:

- Collect log on TCP/UDP syslog
- Collect log on stdin
- Syslog RFC3154 or RFC5424
- Parse logs formats: JSON, key/values, common, combined
- Stream access logs with websocket
- Download logs with HTTP
- Filter out unwanting log lines (predicate in Javascript)
- Can write collected logs to stdout, stderr, file
- Can write collected logs to databases: PostgreSQL/TimescaleDB, Elasticsearch
- Can write collected logs to message brokers: RabbitMQ, nsqd, STOMP enabled message broker
- Can write collected logs to a distributed log: Kafka
- Can write collected logs to a redis list
- Can forward collected logs to another reaper instance

